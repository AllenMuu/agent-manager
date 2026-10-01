package eval_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/eval"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func TestGovernanceSuiteEvaluatesLocalFixturesAndCarriesSnapshotReferences(t *testing.T) {
	suite, err := eval.LoadSuite(filepath.Join("testdata", "governance"))
	if err != nil {
		t.Fatal(err)
	}
	if !suite.GovernanceOnly || len(suite.Cases) != 4 {
		t.Fatalf("suite = %#v", suite)
	}
	result, err := eval.Run(suite, eval.Options{Now: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Passed != 4 || result.Summary.Failed != 0 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if len(result.PolicySnapshots) != 3 {
		t.Fatalf("policy snapshot references = %#v", result.PolicySnapshots)
	}
	for _, item := range result.Cases {
		if item.Status != "pass" {
			t.Fatalf("case result = %#v", item)
		}
	}
	if _, err := result.YAML(); err != nil {
		t.Fatal(err)
	}
}

func TestGovernanceEvalFailsOnReasonCodeMismatch(t *testing.T) {
	suite, err := eval.LoadSuite(filepath.Join("testdata", "governance"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range suite.Cases {
		if suite.Cases[i].ID == "governance-denied" {
			suite.Cases[i].Governance.ReasonCode = policy.ReasonToolDenied
		}
	}
	result, err := eval.Run(suite, eval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Failed != 1 {
		t.Fatalf("reason mismatch was not a failure: %#v", result.Summary)
	}
}

func TestEvalCanReadAuditEventsFromSharedLocalRunStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "user-state", "runs")
	store, err := run.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: eval-policy\nname: Eval Policy\ntools:\n  allow: [read_file]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	actor := identity.ActorIdentity{ID: "eval-actor", Kind: identity.Human, Subject: "eval@local", Roles: []string{"operator"}}
	delegation := identity.Delegation{ID: "eval-delegation", ActorID: actor.ID, Scopes: []string{"local:read"}, ExpiresAt: now.Add(time.Hour)}
	record, _, err := store.Start(snapshot, "mock", t.TempDir(), identity.NamedSelection(actor, delegation), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file", ActionType: "inspect", ActionID: "local.read", TraceID: "eval-trace"}, policy.BudgetState{}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(filepath.Join("testdata", "governance"))
	if err != nil {
		t.Fatal(err)
	}
	suite.Cases = []eval.Case{{Version: eval.GovernanceCaseVersion, ID: "shared-run-evidence", Category: "governance", Governance: &eval.GovernanceAssertion{Category: policy.ToolCallRequested, Tool: "read_file", ActionType: "inspect", ActionID: "local.read", ActorID: actor.ID, DelegationID: record.Identity.Delegation.ID, TraceID: "eval-trace", Decision: policy.Allow}}}
	result, err := eval.Run(suite, eval.Options{GovernanceRunID: record.ID, RunStoreRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Passed != 1 || len(result.PolicySnapshots) != 1 || result.PolicySnapshots[0].Hash != snapshot.Hash {
		t.Fatalf("result = %#v", result)
	}
}
