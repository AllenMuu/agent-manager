// Package memoryprovider selects delivery providers without networking the domain core.
package memoryprovider

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
)

const configLimit = 64 * 1024

// Open performs explicit configuration selection; it never starts or installs services.
func Open(c memory.ProviderConfig) (memory.StructuredProvider, error) {
	if c.Provider != "mem0" {
		return memory.OpenConfiguredProvider(c)
	}
	if c.Validate() != nil {
		return nil, memory.ErrInvalidInput
	}
	data, err := configuration(c.Configuration)
	if err != nil {
		return nil, err
	}
	var input struct {
		Endpoint            string                  `json:"endpoint"`
		Contract            string                  `json:"contract"`
		AllowNetwork        bool                    `json:"allowNetwork"`
		TimeoutMilliseconds int64                   `json:"timeoutMilliseconds"`
		SecretReference     *memory.ConfigReference `json:"secretReference"`
	}
	if memory.ValidateCanonicalJSON(data) != nil {
		return nil, memory.ErrInvalidInput
	}
	if json.Unmarshal(data, &input) != nil {
		return nil, memory.ErrInvalidInput
	}
	// Unknown fields might be raw credentials; reject rather than silently retain.
	var fields map[string]json.RawMessage
	json.Unmarshal(data, &fields)
	for k := range fields {
		switch k {
		case "endpoint", "contract", "allowNetwork", "timeoutMilliseconds", "secretReference":
		default:
			return nil, memory.ErrInvalidInput
		}
	}
	if input.TimeoutMilliseconds < 0 || input.TimeoutMilliseconds > 60000 {
		return nil, memory.ErrInvalidInput
	}
	if input.SecretReference != nil && input.SecretReference.Kind != "env" {
		return nil, memory.ErrUnsupported
	}
	return mem0.New(mem0.Config{Endpoint: input.Endpoint, Contract: input.Contract, AllowNetwork: input.AllowNetwork, Timeout: time.Duration(input.TimeoutMilliseconds) * time.Millisecond, SecretReference: input.SecretReference}, environmentSecrets{})
}
func configuration(ref memory.ConfigReference) ([]byte, error) {
	switch ref.Kind {
	case "env":
		data := []byte(os.Getenv(ref.Name))
		if len(data) == 0 || len(data) > configLimit {
			return nil, memory.ErrInvalidInput
		}
		return data, nil
	case "file":
		before, err := os.Lstat(ref.Name)
		if err != nil || !before.Mode().IsRegular() {
			return nil, memory.ErrInvalidInput
		}
		f, err := os.Open(ref.Name)
		if err != nil {
			return nil, memory.ErrInvalidInput
		}
		defer f.Close()
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) {
			return nil, memory.ErrInvalidInput
		}
		data, err := io.ReadAll(io.LimitReader(f, configLimit+1))
		if err != nil || len(data) > configLimit {
			return nil, memory.ErrInvalidInput
		}
		return data, nil
	default:
		return nil, memory.ErrUnsupported
	}
}

type environmentSecrets struct{}

func (environmentSecrets) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	if ctx.Err() != nil {
		return "", memory.ErrCanceled
	}
	if ref.Kind != "env" {
		return "", memory.ErrUnsupported
	}
	value := os.Getenv(ref.Name)
	if value == "" {
		return "", memory.ErrAuthentication
	}
	return value, nil
}

// Discover reports Gateway dispatch separately from the weaker provider API.
func Discover(ctx context.Context, c memory.ProviderConfig) (memory.ProviderStatus, error) {
	if c.Provider != "mem0" {
		return memory.DiscoverConfiguredProvider(ctx, c)
	}
	p, err := Open(c)
	if err != nil {
		return memory.ProviderStatus{Unsupported: err == memory.ErrUnsupported, Reason: memory.SafeError(err).Error()}, err
	}
	// This selected adapter provides basic mutations but no confirmed Gateway writes.
	caps := p.Capabilities()
	caps.Remember = false
	caps.BasicReplace = false
	caps.BasicRemove = false
	status := memory.ProviderStatus{StructuredCapabilities: &caps, Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch}, Scopes: append([]memory.Scope(nil), c.Scopes...), Ranking: "provider-normalized-score"}
	health, err := p.Health(ctx)
	status.Available = health.Available && err == nil
	if err != nil {
		status.Reason = memory.SafeError(err).Error()
		return status, memory.SafeError(err)
	}
	return status, nil
}
