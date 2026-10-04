package adapter_test

import (
	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"testing"
)

func TestDirectoryDeclarationCannotEnforceMandatoryControls(t *testing.T) {
	for _, target := range []adapter.Target{adapter.Codex, adapter.ClaudeCode, adapter.Pi} {
		declaration, err := adapter.EnforcementDeclaration(target)
		if err != nil {
			t.Fatal(err)
		}
		r := enforcement.Request{Version: "v1", Policy: policy.AgentPolicy{Version: "v1", Kind: "agent-policy", ID: "offline", Name: "Offline"}, Required: []enforcement.Dimension{enforcement.Credential}, Provider: declaration}
		result := enforcement.Preflight(r)
		if result.Accepted || result.Controls[enforcement.Credential].Support != "unsupported" || result.Governance.Ready {
			t.Fatalf("directory %s claims protection: %+v", target, result)
		}
	}
	if _, err := adapter.EnforcementDeclaration(adapter.Target("unknown")); err == nil {
		t.Fatal("unknown target accepted")
	}
}
