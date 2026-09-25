package memory_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"gopkg.in/yaml.v3"
)

func TestProviderConfigValidatesVersionReferenceScopesAndCapabilities(t *testing.T) {
	cfg := memory.ProviderConfig{
		Version: "v1", ID: "local-memory", Provider: "graphiti",
		Configuration: memory.ConfigReference{Kind: "env", Name: "GRAPHITI_URL"},
		Scopes:        []memory.Scope{memory.ScopeUser, memory.ScopeProject},
		Capabilities:  []memory.Capability{memory.CapabilityRead, memory.CapabilityWrite, memory.CapabilitySearch},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProviderConfigRejectsInlineSecrets(t *testing.T) {
	cfg := memory.ProviderConfig{
		Version: "v1", ID: "provider", Provider: "graphiti",
		Configuration: memory.ConfigReference{Kind: "inline", Name: "token=super-secret"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want inline secret rejection")
	}
}

func TestProviderConfigJSONAndEvidenceRedactSensitiveValues(t *testing.T) {
	cfg := memory.ProviderConfig{
		Version: "v1", ID: "provider", Provider: "graphiti",
		Configuration: memory.ConfigReference{Kind: "env", Name: "GRAPHITI_TOKEN"},
		Scopes:        []memory.Scope{memory.ScopeProject},
		Capabilities:  []memory.Capability{memory.CapabilityRead},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "GRAPHITI_TOKEN") || strings.Contains(out, "super-secret") {
		t.Fatalf("JSON leaked sensitive reference: %s", out)
	}
	if !strings.Contains(out, "redacted") {
		t.Fatalf("JSON = %s, want redacted marker", out)
	}
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if yamlOut := string(yamlBytes); strings.Contains(yamlOut, "GRAPHITI_TOKEN") || !strings.Contains(yamlOut, "[redacted]") {
		t.Fatalf("YAML leaked or omitted redaction: %s", yamlOut)
	}
	evidence := cfg.JournalEvidence()
	if strings.Contains(evidence, "GRAPHITI_TOKEN") || strings.Contains(evidence, "super-secret") {
		t.Fatalf("journal evidence leaked sensitive value: %s", evidence)
	}
}

func TestAgentAccessReportsIndependentCapabilitiesAndScopes(t *testing.T) {
	access := memory.AgentAccess{Agent: "codex", Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}
	if !access.Supports(memory.CapabilitySearch, memory.ScopeProject) {
		t.Fatal("Supports(search, project) = false, want true")
	}
	if access.Supports(memory.CapabilityWrite, memory.ScopeProject) {
		t.Fatal("Supports(write, project) = true, want false")
	}
	if access.Supports(memory.CapabilityRead, memory.ScopeUser) {
		t.Fatal("Supports(read, user) = true, want false")
	}
}

type localTestProvider struct {
	status       memory.ProviderStatus
	statusCalls  int
	promoteCalls int
}

func (p *localTestProvider) Status() memory.ProviderStatus {
	p.statusCalls++
	return p.status
}

func (p *localTestProvider) Promote(memory.Scope, string) error {
	p.promoteCalls++
	return nil
}

func TestProviderStatusMapsOnlyCapabilitiesTheAgentSupports(t *testing.T) {
	provider := &localTestProvider{status: memory.ProviderStatus{
		Available:    true,
		Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilityWrite, memory.CapabilitySearch},
		Scopes:       []memory.Scope{memory.ScopeUser, memory.ScopeProject},
	}}
	agent := memory.AgentAccess{
		Agent:        "codex",
		Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
		Scopes:       []memory.Scope{memory.ScopeProject},
	}

	got := memory.MapAgentAccess(provider.Status(), agent)
	if got.Agent != agent.Agent {
		t.Fatalf("mapped agent = %q, want %q", got.Agent, agent.Agent)
	}
	if got.Supports(memory.CapabilityRead, memory.ScopeProject) == false ||
		got.Supports(memory.CapabilitySearch, memory.ScopeProject) == false {
		t.Fatalf("mapped access = %#v, want read/search project access", got)
	}
	if got.Supports(memory.CapabilityWrite, memory.ScopeProject) {
		t.Fatal("mapped access unexpectedly granted unsupported write capability")
	}
	if got.Supports(memory.CapabilityRead, memory.ScopeUser) {
		t.Fatal("mapped access unexpectedly granted unsupported user scope")
	}
}

func TestBuildStatusReportsProviderCapabilitiesUnsupportedByAgent(t *testing.T) {
	config := &memory.ProviderConfig{Provider: memory.FileProviderID}
	report := memory.BuildStatus(config, memory.ProviderStatus{
		Available:    true,
		Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
		Scopes:       []memory.Scope{memory.ScopeUser, memory.ScopeProject},
	}, []memory.AgentAccess{{
		Agent:        "codex",
		Capabilities: []memory.Capability{memory.CapabilityRead},
		Scopes:       []memory.Scope{memory.ScopeUser},
	}}, nil)
	if len(report.Agents) != 1 {
		t.Fatalf("agents = %#v, want one agent", report.Agents)
	}
	agent := report.Agents[0]
	if len(agent.Capabilities) != 1 || agent.Capabilities[0] != memory.CapabilityRead {
		t.Fatalf("capabilities = %#v, want read intersection", agent.Capabilities)
	}
	if len(agent.UnsupportedCapabilities) != 1 || agent.UnsupportedCapabilities[0] != memory.CapabilitySearch {
		t.Fatalf("unsupported capabilities = %#v, want search gap", agent.UnsupportedCapabilities)
	}
	if len(agent.UnsupportedScopes) != 1 || agent.UnsupportedScopes[0] != memory.ScopeProject {
		t.Fatalf("unsupported scopes = %#v, want project gap", agent.UnsupportedScopes)
	}
}

func TestBuildStatusSplitsUnavailableAndUnsupportedProviderGaps(t *testing.T) {
	config := &memory.ProviderConfig{
		Provider:     memory.FileProviderID,
		Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
		Scopes:       []memory.Scope{memory.ScopeUser, memory.ScopeProject},
	}
	report := memory.BuildStatus(config, memory.ProviderStatus{
		Reason: "provider is unavailable",
	}, []memory.AgentAccess{{
		Agent:        "codex",
		Capabilities: []memory.Capability{memory.CapabilityRead},
		Scopes:       []memory.Scope{memory.ScopeUser},
	}}, nil)
	agent := report.Agents[0]
	if len(agent.UnsupportedCapabilities) != 1 || agent.UnsupportedCapabilities[0] != memory.CapabilitySearch {
		t.Fatalf("unsupported capabilities = %#v, want search only", agent.UnsupportedCapabilities)
	}
	if len(agent.UnavailableCapabilities) != 1 || agent.UnavailableCapabilities[0] != memory.CapabilityRead {
		t.Fatalf("unavailable capabilities = %#v, want read only", agent.UnavailableCapabilities)
	}
	if len(agent.UnsupportedScopes) != 1 || agent.UnsupportedScopes[0] != memory.ScopeProject {
		t.Fatalf("unsupported scopes = %#v, want project only", agent.UnsupportedScopes)
	}
	if len(agent.UnavailableScopes) != 1 || agent.UnavailableScopes[0] != memory.ScopeUser {
		t.Fatalf("unavailable scopes = %#v, want user only", agent.UnavailableScopes)
	}
}

func TestUnavailableProviderMapsToNoAgentAccess(t *testing.T) {
	got := memory.MapAgentAccess(memory.ProviderStatus{Available: false, Capabilities: []memory.Capability{memory.CapabilityRead}, Scopes: []memory.Scope{memory.ScopeProject}}, memory.AgentAccess{
		Agent: "codex", Capabilities: []memory.Capability{memory.CapabilityRead}, Scopes: []memory.Scope{memory.ScopeProject},
	})
	if len(got.Capabilities) != 0 || len(got.Scopes) != 0 {
		t.Fatalf("unavailable provider mapped access = %#v, want no capabilities or scopes", got)
	}
}

func TestOrdinaryProviderInspectionDoesNotWrite(t *testing.T) {
	provider := &localTestProvider{status: memory.ProviderStatus{
		Available:    true,
		Capabilities: []memory.Capability{memory.CapabilityRead},
		Scopes:       []memory.Scope{memory.ScopeProject},
	}}
	cfg := memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: "test-provider",
		Configuration: memory.ConfigReference{Kind: "env", Name: "TEST_PROVIDER_CONFIG"},
		Scopes:        []memory.Scope{memory.ScopeProject},
		Capabilities:  []memory.Capability{memory.CapabilityRead},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	_ = cfg.Redacted()
	_ = memory.MapAgentAccess(provider.Status(), memory.AgentAccess{
		Agent: "codex", Capabilities: []memory.Capability{memory.CapabilityRead}, Scopes: []memory.Scope{memory.ScopeProject},
	})
	if provider.promoteCalls != 0 {
		t.Fatalf("ordinary inspection promoted %d values, want 0", provider.promoteCalls)
	}
	if provider.statusCalls != 1 {
		t.Fatalf("provider status calls = %d, want 1", provider.statusCalls)
	}
}

func TestFakeProviderStatusMatrixMapsAdapterAccessWithoutPromotion(t *testing.T) {
	config := &memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: "/tmp/provider-store"},
		Capabilities:  []memory.Capability{memory.CapabilityRead, memory.CapabilityWrite, memory.CapabilitySearch},
		Scopes:        []memory.Scope{memory.ScopeUser, memory.ScopeProject},
	}
	integration := memory.AgentAccess{
		Agent:        "codex",
		Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
		Scopes:       []memory.Scope{memory.ScopeProject},
	}
	cases := []struct {
		name        string
		status      memory.ProviderStatus
		wantState   string
		wantAccess  []memory.Capability
		wantMissing []memory.Capability
	}{
		{
			name:        "available",
			status:      memory.ProviderStatus{Available: true, Capabilities: config.Capabilities, Scopes: config.Scopes},
			wantState:   memory.StateAvailable,
			wantAccess:  []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
			wantMissing: []memory.Capability{memory.CapabilityWrite},
		},
		{
			name:        "unavailable",
			status:      memory.ProviderStatus{Reason: "provider is unavailable", Capabilities: config.Capabilities, Scopes: config.Scopes},
			wantState:   memory.StateUnavailable,
			wantAccess:  nil,
			wantMissing: []memory.Capability{memory.CapabilityWrite},
		},
		{
			name:        "unsupported",
			status:      memory.ProviderStatus{Unsupported: true, Reason: "provider is not supported locally"},
			wantState:   memory.StateUnsupported,
			wantAccess:  nil,
			wantMissing: config.Capabilities,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &localTestProvider{status: tc.status}
			report := memory.BuildStatus(config, provider.Status(), []memory.AgentAccess{integration}, nil)
			if report.State != tc.wantState {
				t.Fatalf("provider state = %q, want %q", report.State, tc.wantState)
			}
			agent := findAgentStatus(report, integration.Agent)
			if agent == nil {
				t.Fatalf("agent %q missing from %#v", integration.Agent, report.Agents)
			}
			if !sameCapabilities(agent.Capabilities, tc.wantAccess) {
				t.Fatalf("mapped capabilities = %#v, want %#v", agent.Capabilities, tc.wantAccess)
			}
			if tc.wantState == memory.StateUnavailable && len(agent.UnavailableCapabilities) != 2 {
				t.Fatalf("unavailable capabilities = %#v, want read/search intersection", agent.UnavailableCapabilities)
			}
			if !sameCapabilities(agent.UnsupportedCapabilities, tc.wantMissing) && tc.wantState != memory.StateUnavailable {
				t.Fatalf("unsupported capabilities = %#v, want %#v", agent.UnsupportedCapabilities, tc.wantMissing)
			}
			mapped := memory.MapAgentAccess(provider.Status(), integration)
			if !sameCapabilities(mapped.Capabilities, tc.wantAccess) {
				t.Fatalf("adapter mapping = %#v, want %#v", mapped.Capabilities, tc.wantAccess)
			}
			if provider.promoteCalls != 0 {
				t.Fatalf("status and mapping promoted %d values, want 0", provider.promoteCalls)
			}
		})
	}
}

func findAgentStatus(report memory.StatusReport, id string) *memory.AgentStatus {
	for i := range report.Agents {
		if report.Agents[i].Agent == id {
			return &report.Agents[i]
		}
	}
	return nil
}

func sameCapabilities(got, want []memory.Capability) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
