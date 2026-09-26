package role_test

import (
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/role"
)

func TestBuiltinsDeclareStableArtifactContracts(t *testing.T) {
	contracts := role.Builtins()
	if len(contracts) != 4 {
		t.Fatalf("contracts = %d, want 4", len(contracts))
	}
	for _, contract := range contracts {
		if err := contract.Validate(); err != nil {
			t.Fatalf("%s: %v", contract.ID, err)
		}
	}
	planner, ok := role.For(role.Planner)
	if !ok || len(planner.Inputs) != 2 || planner.Outputs[0] != "plan" {
		t.Fatalf("planner = %#v", planner)
	}
}

func TestBindReportsRuntimeGapsWithoutSilentlyGrantingThem(t *testing.T) {
	contract, _ := role.For(role.Implementer)
	binding, err := role.Bind(adapter.Codex, contract)
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Supported || len(binding.Missing) != 0 {
		t.Fatalf("binding = %#v", binding)
	}
	if len(binding.Warnings) == 0 {
		t.Fatal("shell capability warning missing")
	}
	if _, err := role.Bind(adapter.Target("unknown"), contract); err == nil {
		t.Fatal("unknown target was accepted")
	}
}
