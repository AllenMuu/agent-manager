package role_test

import (
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/artifact"
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
	if !ok || len(planner.Inputs) != 2 || planner.Outputs[0] != artifact.Plan {
		t.Fatalf("planner = %#v", planner)
	}
}

func TestContractRejectsNoncanonicalArtifactKinds(t *testing.T) {
	valid, _ := role.For(role.Planner)
	for _, tc := range []struct {
		name  string
		kind  artifact.Kind
		input bool
	}{
		{name: "empty input", kind: "", input: true},
		{name: "unknown input", kind: "draft", input: true},
		{name: "empty output", kind: ""},
		{name: "unknown output", kind: "review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := valid
			if tc.input {
				contract.Inputs = []artifact.Kind{tc.kind}
			} else {
				contract.Outputs = []artifact.Kind{tc.kind}
			}
			if err := contract.Validate(); err == nil || !strings.Contains(err.Error(), "invalid ") {
				t.Fatalf("Validate() = %v, want invalid artifact error", err)
			}
		})
	}
}

func TestBindReportsRuntimeGapsWithoutSilentlyGrantingThem(t *testing.T) {
	contract, _ := role.For(role.Implementer)
	binding, err := role.Bind(adapter.Codex, contract)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Supported || len(binding.Missing) == 0 {
		t.Fatalf("binding = %#v", binding)
	}
	if binding.Capabilities.FilesystemWrite || len(binding.Warnings) == 0 {
		t.Fatalf("unverified runtime permissions were granted: %#v", binding)
	}
	if len(binding.Inputs) != len(contract.Inputs) || len(binding.Outputs) != 1 || binding.Outputs[0] != artifact.Implementation {
		t.Fatalf("binding lost the artifact contract: %#v", binding)
	}
	if _, err := role.Bind(adapter.Target("unknown"), contract); err == nil {
		t.Fatal("unknown target was accepted")
	}
}
