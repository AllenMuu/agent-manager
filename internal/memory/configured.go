package memory

import (
	"context"
	"path/filepath"
)

const StructuredLocalProviderID = "structured-local"

// OpenConfiguredProvider selects an existing direct local store explicitly.
// It never converts text, creates a directory, resolves credentials or installs
// a provider. Future external implementations may use the same neutral seam.
func OpenConfiguredProvider(config ProviderConfig) (StructuredProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, ErrInvalidInput
	}
	if config.Provider != StructuredLocalProviderID || !structuredLocalSupported {
		return nil, ErrUnsupported
	}
	if config.Configuration.Kind != "file" || !filepath.IsAbs(config.Configuration.Name) {
		return nil, ErrInvalidInput
	}
	provider, err := OpenStructuredStore(config.Configuration.Name)
	return provider, SafeError(err)
}
func implementedStructured(p StructuredProvider) StructuredCapabilities {
	if p == nil {
		return StructuredCapabilities{}
	}
	declared := p.Capabilities()
	c := declared
	// Gateway add dispatches the operation-aware seam; basic Remember alone
	// does not implement its explicitly confirmed operation-ID contract.
	_, c.Remember = p.(RecordOperationWriter)
	c.Remember = c.Remember && declared.Remember
	_, c.Get = p.(RecordReader)
	c.Get = c.Get && declared.Get
	_, c.Recall = p.(RecordRecaller)
	c.Recall = c.Recall && declared.Recall
	_, c.ScoredRecall = p.(ScoredRecordRecaller)
	c.ScoredRecall = c.ScoredRecall && declared.ScoredRecall
	_, c.Update = p.(RecordUpdater)
	c.Update = c.Update && declared.Update
	c.ConditionalUpdate = c.ConditionalUpdate && c.Update
	_, c.Supersede = p.(RecordSuperseder)
	c.Supersede = c.Supersede && declared.Supersede
	c.AtomicSupersede = c.AtomicSupersede && c.Supersede
	_, c.Forget = p.(RecordForgetter)
	c.Forget = c.Forget && declared.Forget
	_, c.History = p.(RecordHistorian)
	c.History = c.History && declared.History
	_, c.ImportLegacy = p.(LegacyImporter)
	c.ImportLegacy = c.ImportLegacy && declared.ImportLegacy
	return c
}

// DiscoverConfiguredProvider keeps implemented semantics distinct from the
// requested configuration and current availability. All reasons are safe.
func DiscoverConfiguredProvider(ctx context.Context, config ProviderConfig) (ProviderStatus, error) {
	if err := config.Validate(); err != nil {
		return ProviderStatus{Reason: "invalid Memory provider configuration"}, ErrInvalidInput
	}
	switch config.Provider {
	case FileProviderID:
		status, err := DiscoverFileProvider(config)
		// The legacy text provider implements explicit append only. Its older
		// declaration API is preserved; discovery no longer advertises text search.
		status.Capabilities = []Capability{CapabilityWrite}
		return status, SafeError(err)
	case StructuredLocalProviderID:
		if !structuredLocalSupported {
			return ProviderStatus{Unsupported: true, Reason: "structured-local Memory storage is unsupported on this platform"}, ErrUnsupported
		}
		// This known local implementation retains operation discovery even when
		// its selected root is unavailable; current health remains separate.
		c := (&StructuredStore{}).Capabilities()
		status := ProviderStatus{Capabilities: []Capability{CapabilityRead, CapabilityWrite, CapabilitySearch}, Scopes: append([]Scope(nil), config.Scopes...), StructuredCapabilities: &c, Ranking: "lexical-token-coverage"}
		p, err := OpenConfiguredProvider(config)
		if err != nil {
			status.Reason = "structured Memory store unavailable; select an existing direct directory with valid canonical state"
			return status, err
		}
		health, err := p.Health(ctx)
		status.Available = health.Available && err == nil
		if !status.Available {
			status.Reason = "structured Memory store unavailable; inspect local storage"
		}
		return status, SafeError(err)
	default:
		return ProviderStatus{Unsupported: true, Reason: "configured Memory provider is not supported locally"}, ErrUnsupported
	}
}

// ProviderStatus reports the operations dispatchable through this Gateway,
// intersected with provider declarations. Remember/write means confirmed add
// via RecordOperationWriter, separately from generic RecordWriter/Remember.
// A provider cannot acquire capabilities by requesting them in configuration.
func (g *Gateway) ProviderStatus(ctx context.Context) ProviderStatus {
	status := ProviderStatus{}
	if g != nil && g.provider != nil {
		c := implementedStructured(g.provider)
		status.StructuredCapabilities = &c
		if c.Get {
			status.Capabilities = append(status.Capabilities, CapabilityRead)
		}
		if c.Remember {
			status.Capabilities = append(status.Capabilities, CapabilityWrite)
		}
		if c.Recall || c.ScoredRecall {
			status.Capabilities = append(status.Capabilities, CapabilitySearch)
		}
		status.Ranking = "lexical-token-coverage"
		if c.ScoredRecall {
			status.Ranking = "provider-normalized-score"
		}
		if health, err := g.provider.Health(ctx); err == nil {
			status.Available = health.Available
		}
		if !status.Available {
			status.Reason = "Memory provider unavailable"
		}
	}
	return status
}

func (g *Gateway) Status(ctx context.Context, config *ProviderConfig) StatusReport {
	return BuildStatus(config, g.ProviderStatus(ctx), nil, nil)
}
