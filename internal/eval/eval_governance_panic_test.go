package eval

import (
	"testing"

	"github.com/AllenMuu/skill-manager/internal/policy"
)

func TestEvaluateGovernanceMissingCapabilityMismatchWithoutReadyAssertion(t *testing.T) {
	result := evaluateGovernance(Case{
		ID: "missing-only-capability-assertion",
		Governance: &GovernanceAssertion{
			MissingCapabilities: []policy.Control{policy.ControlToolCallBudget},
		},
	}, GovernanceEvidence{CapabilityReport: &policy.CapabilityReport{
		Ready:   false,
		Missing: []policy.Control{policy.ControlNetworkRestriction},
	}})
	if result.Status != "fail" {
		t.Fatalf("result status = %q, want fail", result.Status)
	}
	if len(result.Evidence) != 1 {
		t.Fatalf("evidence = %v, want one mismatch detail", result.Evidence)
	}
}
