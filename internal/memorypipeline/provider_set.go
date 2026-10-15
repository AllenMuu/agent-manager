package memorypipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CompositeRunEvidenceProvider merges explicitly configured source adapters
// and routes each immutable reference back to exactly one matching provider.
type CompositeRunEvidenceProvider struct {
	providers []RunEvidenceProvider
}

type evidenceReferenceMatcher interface {
	MatchesEvidenceRef(RunEvidenceRef) bool
}

func NewCompositeRunEvidenceProvider(providers ...RunEvidenceProvider) (*CompositeRunEvidenceProvider, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: at least one evidence provider is required", ErrInvalidEvidence)
	}
	copyOfProviders := append([]RunEvidenceProvider(nil), providers...)
	for _, provider := range copyOfProviders {
		if provider == nil {
			return nil, fmt.Errorf("%w: evidence provider is nil", ErrInvalidEvidence)
		}
	}
	return &CompositeRunEvidenceProvider{providers: copyOfProviders}, nil
}

func (p *CompositeRunEvidenceProvider) Discover(ctx context.Context, projectID string) ([]RunEvidenceRef, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	refsByID := make(map[string]RunEvidenceRef)
	for _, provider := range p.providers {
		refs, err := provider.Discover(ctx, projectID)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if prior, ok := refsByID[ref.ID]; ok {
				if !sameReference(prior, ref) {
					return nil, fmt.Errorf("%w: evidence providers returned conflicting source metadata", ErrConflict)
				}
				continue
			}
			refsByID[ref.ID] = ref
		}
	}
	refs := make([]RunEvidenceRef, 0, len(refsByID))
	for _, ref := range refsByID {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].CreatedAt.Equal(refs[j].CreatedAt) {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].CreatedAt.Before(refs[j].CreatedAt)
	})
	return refs, nil
}

func (p *CompositeRunEvidenceProvider) Read(ctx context.Context, ref RunEvidenceRef) (RunEvidence, error) {
	if err := contextErr(ctx); err != nil {
		return RunEvidence{}, err
	}
	var matched RunEvidenceProvider
	for _, provider := range p.providers {
		matcher, ok := provider.(evidenceReferenceMatcher)
		if !ok || !matcher.MatchesEvidenceRef(ref) {
			continue
		}
		if matched != nil {
			return RunEvidence{}, fmt.Errorf("%w: multiple providers match the evidence reference", ErrConflict)
		}
		matched = provider
	}
	if matched == nil {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	return matched.Read(ctx, ref)
}

func (p *AgentManagerRunProvider) MatchesEvidenceRef(ref RunEvidenceRef) bool {
	return strings.HasPrefix(ref.SourceRef, "agent-manager://runs/")
}

func (p *NativeSessionProvider) MatchesEvidenceRef(ref RunEvidenceRef) bool {
	return nativeSourceReference(ref.SourceRef, p.runtime, ref.NativeRunID)
}
