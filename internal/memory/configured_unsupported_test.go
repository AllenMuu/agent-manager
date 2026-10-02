//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package memory_test

import (
	"context"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"testing"
)

func TestStructuredConfiguredDiscoveryReportsUnsupportedPlatform(t *testing.T) {
	configuration := memory.ProviderConfig{Version: "v1", ID: "local", Provider: memory.StructuredLocalProviderID, Configuration: memory.ConfigReference{Kind: "file", Name: t.TempDir()}, Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}
	status, err := memory.DiscoverConfiguredProvider(context.Background(), configuration)
	report := memory.BuildStatus(&configuration, status, nil, nil)
	if !errors.Is(err, memory.ErrUnsupported) || !status.Unsupported || status.Available || report.State != memory.StateUnsupported || len(report.Capabilities) != 0 || len(report.UnsupportedCapabilities) != 2 {
		t.Fatalf("unsupported platform discovery: %+v %v", report, err)
	}
}
