package run_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func testSnapshot(t *testing.T) policy.Snapshot {
	t.Helper()
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: safe\nname: Safe\ntools:\n  allow: [read_file]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func approvalSnapshot(t *testing.T) policy.Snapshot {
	t.Helper()
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: approval-policy\nname: Approval Policy\ntools:\n  allow: [github.update_file]\napproval:\n  required_for: [destructive_write]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testCapabilities() map[policy.Control]bool {
	return map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}
}

func testApprover() identity.ActorIdentity {
	return identity.ActorIdentity{ID: "reviewer", Kind: identity.Human, Subject: "reviewer@local", Roles: []string{"approver"}}
}

func approvalRequest(t *testing.T, manager *run.Manager, record run.Record, now time.Time) run.Approval {
	t.Helper()
	event := policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write"}
	decision, _, audit, err := manager.EvaluateAndRecord(record.ID, event, policy.BudgetState{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.RequireApproval {
		t.Fatalf("approval action decision = %#v, want REQUIRE_APPROVAL", decision)
	}
	return run.Approval{
		RunID: record.ID, RequestAuditID: audit.ID, Category: event.Category,
		Tool: event.Tool, ActionType: event.ActionType, ReasonCode: decision.ReasonCode,
	}
}

func TestStoreSharesRunsAcrossProjectsAndPreservesPolicySnapshot(t *testing.T) {
	store, err := run.NewStore(filepath.Join(t.TempDir(), "user-state", "runs"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	first, report, err := store.Start(testSnapshot(t), "mock", filepath.Join(t.TempDir(), "project-a"), identity.AnonymousSelection(), testCapabilities(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatal("expected capability preflight to pass")
	}
	changedPolicy := testSnapshot(t).Policy
	changedPolicy.Tools.Allow = append(changedPolicy.Tools.Allow, "search")
	changedSnapshot, err := policy.Resolve(changedPolicy, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Start(changedSnapshot, "mock", filepath.Join(t.TempDir(), "project-b"), identity.AnonymousSelection(), testCapabilities(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("shared run inventory = %#v", list)
	}
	loaded, err := store.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Policy.Hash != first.Policy.Hash || loaded.Policy.PolicyID != first.Policy.PolicyID {
		t.Fatalf("snapshot changed: %#v", loaded.Policy)
	}
	if second.Policy.Hash == first.Policy.Hash {
		t.Fatal("policy source edit did not affect the new run snapshot")
	}
	if info, err := os.Stat(filepath.Join(store.Root(), "state.json")); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("run state permissions = %o", info.Mode().Perm())
	}
}

func TestStoreStartSnapshotsExplicitIdentityAndDelegation(t *testing.T) {
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local", Roles: []string{"operator"}}
	delegation := identity.Delegation{ID: "del-1", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Hour)}
	selection := identity.Selection{Mode: identity.ModeNamed, Actor: &actor, Delegation: &delegation}
	record, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), selection, testCapabilities(), now)
	if err != nil {
		t.Fatal(err)
	}
	if record.Identity.Mode != identity.ModeNamed || record.Identity.Actor == nil || record.Identity.Actor.ID != actor.ID {
		t.Fatalf("run identity = %#v", record.Identity)
	}
	if record.Identity.Delegation == nil || record.Identity.Delegation.RunID != record.ID || !record.Identity.Delegation.HasScope("github:read", now) {
		t.Fatalf("run delegation = %#v", record.Identity.Delegation)
	}
	actor.Roles[0] = "mutated-after-start"
	delegation.Scopes[0] = "github:write"
	loaded, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity.Actor.Roles[0] != "operator" || !loaded.Identity.Delegation.HasScope("github:read", now) {
		t.Fatalf("stored identity changed after input mutation: %#v", loaded.Identity)
	}
}

func TestStoreStartRequiresExplicitIdentityMode(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if _, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.Selection{}, testCapabilities(), time.Now()); err == nil {
		t.Fatal("run started without an explicit identity mode")
	}
	runs, err := store.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("invalid identity created a run: runs=%#v err=%v", runs, err)
	}
	if _, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.Selection{Mode: identity.ModeAnonymous}, testCapabilities(), time.Now()); err != nil {
		t.Fatalf("explicit anonymous run rejected: %v", err)
	}
}

func TestV1RunStoreReadsAsLegacyWithoutRewriteAndUpgradesOnMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	store, _ := run.NewStore(root)
	started, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), testCapabilities(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(contents, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["version"] = "v1"
	runs := envelope["runs"].(map[string]any)
	legacy := runs[started.ID].(map[string]any)
	delete(legacy, "identity")
	events := envelope["events"].([]any)
	events[0].(map[string]any)["actor"] = "historical-operator"
	contents, err = json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), contents...)

	legacyStore, _ := run.NewStore(root)
	loaded, err := legacyStore.Get(started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity.Mode != identity.ModeLegacyAnonymous || loaded.Identity.Actor != nil {
		t.Fatalf("legacy record identity = %#v", loaded.Identity)
	}
	if _, err := legacyStore.List(); err != nil {
		t.Fatal(err)
	}
	afterRead, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(afterRead, before) {
		t.Fatalf("read-only inspection rewrote v1 state: err=%v", err)
	}

	manager, _ := run.NewManager(legacyStore)
	decision, _, _, err := manager.EvaluateAndRecord(started.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{}, time.Now().UTC())
	if err != nil || decision.Outcome != policy.Allow {
		t.Fatalf("legacy policy-only action = %#v, err=%v", decision, err)
	}
	var upgraded map[string]any
	upgradedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(upgradedBytes, &upgraded); err != nil {
		t.Fatal(err)
	}
	if upgraded["version"] != "v2" {
		t.Fatalf("mutated run-store envelope version = %v, want v2", upgraded["version"])
	}
	upgradedRecord, err := legacyStore.Get(started.ID)
	if err != nil || upgradedRecord.Identity.Mode != identity.ModeLegacyAnonymous {
		t.Fatalf("upgraded historical record identity=%#v err=%v", upgradedRecord.Identity, err)
	}
	upgradedEvents, err := legacyStore.Events(started.ID)
	if err != nil || len(upgradedEvents) != 2 || upgradedEvents[0].Actor != "historical-operator" {
		t.Fatalf("upgraded audit history=%#v err=%v", upgradedEvents, err)
	}
}

func TestStoreRejectsMismatchedIdentityReferencesAtStartAndLoad(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	store, _ := run.NewStore(root)
	now := time.Now().UTC()
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local"}
	wrongDelegation := identity.Delegation{ID: "del-wrong", ActorID: "other-user", Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Hour)}
	if _, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.NamedSelection(actor, wrongDelegation), testCapabilities(), now); err == nil {
		t.Fatal("run started with a delegation linked to another actor")
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("mismatched input persisted a partial run: runs=%#v err=%v", runs, err)
	}

	validDelegation := identity.Delegation{ID: "del-valid", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Hour)}
	started, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.NamedSelection(actor, validDelegation), testCapabilities(), now)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(contents, &envelope); err != nil {
		t.Fatal(err)
	}
	runMap := envelope["runs"].(map[string]any)[started.ID].(map[string]any)
	identityMap := runMap["identity"].(map[string]any)
	delegationMap := identityMap["delegation"].(map[string]any)
	delegationMap["run_id"] = "run-other"
	contents, _ = json.MarshalIndent(envelope, "", "  ")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(started.ID); err == nil {
		t.Fatal("store loaded a delegation linked to a different run")
	}
}

func TestAuditRecordsCorrelateTypedIdentityActionPolicyResultAndTrace(t *testing.T) {
	now := time.Now().UTC()
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local", Roles: []string{"operator"}}
	delegation := identity.Delegation{ID: "del-1", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Hour)}
	record, _, err := manager.Start(testSnapshot(t), "mock", t.TempDir(), identity.NamedSelection(actor, delegation), now, &mockController{capabilities: testCapabilities()})
	if err != nil {
		t.Fatal(err)
	}
	decision, _, audit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file", ActionID: "github.read", TraceID: "trace-1"}, policy.BudgetState{}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.Allow || audit.ActorID != actor.ID || audit.DelegationID != "del-1" || audit.ActionID != "github.read" || audit.TraceID != "trace-1" || audit.Result != policy.Allow {
		t.Fatalf("typed audit lineage: decision=%#v audit=%#v", decision, audit)
	}
	encoded, err := json.Marshal(audit)
	if err != nil || strings.Contains(string(encoded), "token=") {
		t.Fatalf("audit serialization exposed unsafe identity data: %s err=%v", encoded, err)
	}
}

func TestDefaultStoreUsesSharedUserConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	store, err := run.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(userConfig, "agent-manager", "runs")
	if store.Root() != want {
		t.Fatalf("default run store = %s, want %s", store.Root(), want)
	}
}

func TestCapabilityPreflightFailsClosed(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: needs-network\nname: Network\ntools:\n  allow: [read_file]\nnetwork:\n  allowed_domains: [example.com]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := policy.Resolve(configured, time.Now())
	_, report, err := store.Start(snapshot, "mock", t.TempDir(), identity.AnonymousSelection(), testCapabilities(), time.Now())
	if err == nil || report.Ready || len(report.Missing) != 1 || report.Missing[0] != policy.ControlNetworkRestriction {
		t.Fatalf("start = report %#v, err %v", report, err)
	}
}

func TestManagerEvaluatesAndPersistsPolicyDecision(t *testing.T) {
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	controller := &mockController{capabilities: testCapabilities()}
	record, _, err := manager.Start(testSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	decision, evaluation, audit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "open_shell"}, policy.BudgetState{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonToolNotAllowlisted {
		t.Fatalf("decision = %#v", decision)
	}
	if len(evaluation.Violations) != 0 {
		t.Fatalf("pre-event evaluation reported violations: %#v", evaluation.Violations)
	}
	if audit.Decision != decision.Outcome || audit.ReasonCode != decision.ReasonCode || audit.RunID != record.ID || audit.PolicyHash != record.Policy.Hash {
		t.Fatalf("audit = %#v", audit)
	}
	events, err := store.Events(record.ID)
	if err != nil || len(events) != 2 || events[1].ID != audit.ID {
		t.Fatalf("stored events = %#v, err=%v", events, err)
	}
}

func TestManagerEvaluatesApprovalAndWritesLinkedAudit(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{
		policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true,
	}, confirmPause: true, confirmResolve: true}
	now := time.Now().UTC()
	record, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), now, controller)
	if err != nil {
		t.Fatal(err)
	}
	action := policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write", ActionID: "github.write", TraceID: "trace-approval"}
	decision, _, decisionAudit, err := manager.EvaluateAndRecord(record.ID, action, policy.BudgetState{}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.RequireApproval || decision.ReasonCode != policy.ReasonApprovalRequired {
		t.Fatalf("decision = %#v", decision)
	}
	approval, err := manager.RequestApproval(context.Background(), run.Approval{RunID: record.ID, RequestAuditID: decisionAudit.ID, Tool: action.Tool, ActionType: action.ActionType, ReasonCode: decision.ReasonCode}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != run.ApprovalPending {
		t.Fatalf("approval = %#v", approval)
	}
	events, err := store.Events(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[1].ID != decisionAudit.ID || events[1].Decision != policy.RequireApproval || events[2].Decision != policy.RequireApproval {
		t.Fatalf("governance decision/approval audit chain = %#v", events)
	}
	if events[2].ApprovalID != approval.ID || events[2].ActionID != action.ActionID || events[2].TraceID != decisionAudit.TraceID {
		t.Fatalf("approval request audit lost the action trace: %#v", events[2])
	}
	if _, err := manager.DecideApproval(context.Background(), approval.ID, run.ApprovalApproved, "operator approved", testApprover(), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	completionDecision, evaluation, completionAudit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallCompleted, Tool: action.Tool, ActionType: action.ActionType, RequestAuditID: decisionAudit.ID}, policy.BudgetState{}, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if completionDecision.Outcome != policy.Allow || len(evaluation.Violations) != 0 || completionAudit.RequestAuditID != decisionAudit.ID || completionAudit.ApprovalID != approval.ID || completionAudit.ActionID != action.ActionID || completionAudit.TraceID != decisionAudit.TraceID {
		t.Fatalf("approved completion decision=%#v evaluation=%#v audit=%#v", completionDecision, evaluation, completionAudit)
	}
	completedEvents, err := store.Events(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(completedEvents) != 5 || completedEvents[3].ApproverID != testApprover().ID || completedEvents[3].ApprovalID != approval.ID {
		t.Fatalf("approval transition audit attribution = %#v", completedEvents)
	}
}

func TestCompletedActionUsesAndLinksOriginalPreEventDecision(t *testing.T) {
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: one-call-budget\nname: One Call Budget\ntools:\n  allow: [read_file]\nbudget:\n  max_tool_calls: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	record, _, err := manager.Start(snapshot, "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlToolCallBudget: true}})
	if err != nil {
		t.Fatal(err)
	}
	requestDecision, _, requestAudit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if requestDecision.Outcome != policy.Allow {
		t.Fatalf("request decision = %#v", requestDecision)
	}
	completionDecision, evaluation, completionAudit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallCompleted, Tool: "read_file", RequestAuditID: requestAudit.ID}, policy.BudgetState{ToolCalls: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if completionDecision.Outcome != policy.Allow || len(evaluation.Violations) != 0 {
		t.Fatalf("completion decision=%#v evaluation=%#v", completionDecision, evaluation)
	}
	if completionAudit.RequestAuditID != requestAudit.ID {
		t.Fatalf("completion audit did not retain request link: %#v", completionAudit)
	}
	if _, _, _, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallCompleted, Tool: "read_file"}, policy.BudgetState{ToolCalls: 1}, time.Now()); err == nil {
		t.Fatal("completion without a persisted request decision was accepted")
	}
	if _, _, _, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallCompleted, Tool: "read_file", RequestAuditID: "evt_nonexistent"}, policy.BudgetState{ToolCalls: 1}, time.Now()); err == nil {
		t.Fatal("completion linked to a missing request decision was accepted")
	}
}

func TestManagerRecordsBudgetDecisionAndCompletionUsesThatDecision(t *testing.T) {
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: one-call-budget\nname: One Call Budget\ntools:\n  allow: [read_file]\nbudget:\n  max_tool_calls: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlToolCallBudget: true}}
	record, _, err := manager.Start(snapshot, "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{}, time.Now())
	if err != nil || first.Outcome != policy.Allow {
		t.Fatalf("first decision=%#v err=%v", first, err)
	}
	second, _, secondAudit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{ToolCalls: 1}, time.Now())
	if err != nil || second.Outcome != policy.Deny || second.ReasonCode != policy.ReasonToolCallBudgetExceeded {
		t.Fatalf("exhausted-budget decision=%#v err=%v", second, err)
	}
	completion, evaluation, _, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallCompleted, Tool: "read_file", RequestAuditID: secondAudit.ID}, policy.BudgetState{ToolCalls: 2}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if completion.Outcome != policy.Deny || len(evaluation.Violations) != 1 || evaluation.Violations[0] != policy.EventUnexpectedToolCall {
		t.Fatalf("completion decision=%#v evaluation=%#v", completion, evaluation)
	}
	events, err := store.Events(record.ID)
	if err != nil || len(events) != 4 || events[2].Decision != policy.Deny || events[3].RequestAuditID != secondAudit.ID {
		t.Fatalf("budget decision audit chain=%#v err=%v", events, err)
	}
}

func TestApprovalRoundTripsAndTerminalTransitionsAreRejected(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	now := time.Now().UTC()
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}, confirmPause: true}
	runRecord, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), now, controller)
	if err != nil {
		t.Fatal(err)
	}
	approval := approvalRequest(t, manager, runRecord, now)
	approval.ID = "approval-test"
	request, err := manager.RequestApproval(context.Background(), approval, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetApproval(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != run.ApprovalPending {
		t.Fatalf("approval = %#v", loaded)
	}
	if _, err := store.DecideApproval(request.ID, run.ApprovalRejected, "missing decider", identity.ActorIdentity{}, now.Add(time.Second)); err == nil {
		t.Fatal("approval transition without an explicit actor succeeded")
	}
	decided, err := store.DecideApproval(request.ID, run.ApprovalRejected, "operator rejected", testApprover(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != run.ApprovalRejected || decided.DecidedBy == nil || decided.DecidedBy.ID != testApprover().ID {
		t.Fatalf("approval = %#v", decided)
	}
	if _, err := store.DecideApproval(request.ID, run.ApprovalApproved, "", testApprover(), now.Add(2*time.Second)); err == nil {
		t.Fatal("terminal approval transition was accepted")
	}
	events, err := store.Events(runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v", events)
	}
	decisionAuditFound := false
	for _, event := range events {
		if event.ApprovalID == request.ID && event.ApproverID == testApprover().ID && event.Decision == policy.Deny {
			decisionAuditFound = true
		}
	}
	if !decisionAuditFound {
		t.Fatalf("approval audit did not retain explicit approver: %#v", events)
	}
	again, err := store.Events(runRecord.ID)
	if err != nil || !reflect.DeepEqual(events, again) {
		t.Fatalf("audit listing changed: first=%#v second=%#v err=%v", events, again, err)
	}
	for _, event := range events {
		if event.PolicyHash != runRecord.Policy.Hash || event.PolicyID != runRecord.Policy.PolicyID {
			t.Fatalf("audit missing snapshot identity: %#v", event)
		}
	}
}

func TestApprovalCanExpireOnlyWhilePending(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}, confirmPause: true}
	record, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	request, err := manager.RequestApproval(context.Background(), approvalRequest(t, manager, record, time.Now()), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := store.DecideApproval(request.ID, run.ApprovalExpired, "deadline", testApprover(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if expired.Status != run.ApprovalExpired {
		t.Fatalf("approval=%#v", expired)
	}
	if expired.DecidedBy == nil || expired.DecidedBy.ID != testApprover().ID {
		t.Fatalf("expiry lost explicit deciding actor: %#v", expired)
	}
	if _, err := store.DecideApproval(request.ID, run.ApprovalRejected, "late decision", testApprover(), time.Now()); err == nil {
		t.Fatal("expired approval accepted another terminal transition")
	}
}

func TestUnconfirmedKillDoesNotTerminateRun(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlRunTermination: true}}
	record, _, err := manager.Start(testSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	controller.confirmKill = false
	if _, err := manager.Kill(context.Background(), record.ID, run.TerminationOperatorRequested, time.Now()); err == nil {
		t.Fatal("unconfirmed kill succeeded")
	}
	if controller.killedRunID != record.ID || controller.killReason != run.TerminationOperatorRequested {
		t.Fatalf("kill request = %s / %s", controller.killedRunID, controller.killReason)
	}
	current, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != run.Active {
		t.Fatalf("run status = %q", current.Status)
	}
	controller.confirmKill = true
	terminated, err := manager.Kill(context.Background(), record.ID, run.TerminationOperatorRequested, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if terminated.Status != run.Terminated || terminated.TerminationReason != run.TerminationOperatorRequested {
		t.Fatalf("terminated run = %#v", terminated)
	}
	events, _ := store.Events(record.ID)
	if events[len(events)-1].TerminationReason != run.TerminationOperatorRequested || events[len(events)-1].PolicyHash != record.Policy.Hash {
		t.Fatalf("termination evidence = %#v", events[len(events)-1])
	}
}

func TestApprovalRequiresConfirmedPauseAndRecordsTerminalDecision(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}, confirmPause: true, confirmResolve: true}
	now := time.Now().UTC()
	initiator := identity.ActorIdentity{ID: "initiator", Kind: identity.Human, Subject: "initiator@local", Roles: []string{"developer"}}
	delegation := identity.Delegation{ID: "initiator-delegation", ActorID: initiator.ID, Scopes: []string{"github:write"}, ExpiresAt: now.Add(time.Hour)}
	record, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.NamedSelection(initiator, delegation), now, controller)
	if err != nil {
		t.Fatal(err)
	}
	request, err := manager.RequestApproval(context.Background(), approvalRequest(t, manager, record, now), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !controller.paused {
		t.Fatal("controller did not pause the runtime")
	}
	paused, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != run.Paused {
		t.Fatalf("run status = %s", paused.Status)
	}
	decided, err := manager.DecideApproval(context.Background(), request.ID, run.ApprovalApproved, "operator approved", testApprover(), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != run.ApprovalApproved {
		t.Fatalf("approval = %#v", decided)
	}
	if decided.DecidedBy == nil || decided.DecidedBy.ID != testApprover().ID {
		t.Fatalf("approval did not persist explicit approver: %#v", decided)
	}
	resumed, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != run.Active {
		t.Fatalf("run status = %s", resumed.Status)
	}
	events, err := store.Events(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Decision != policy.RequireApproval || events[3].Decision != policy.Allow {
		t.Fatalf("approval audit = %#v", events)
	}
	if events[3].ApprovalID != request.ID || events[3].ApproverID != testApprover().ID || events[3].ActorID != initiator.ID {
		t.Fatalf("approval decision audit omitted approver attribution: %#v", events[3])
	}
}

func TestApprovalRequestMustMatchPolicyAndRuntimeCapabilities(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}}
	record, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	invalidRequest := approvalRequest(t, manager, record, time.Now())
	invalidRequest.ReasonCode = policy.ReasonToolDenied
	_, err = manager.RequestApproval(context.Background(), invalidRequest, time.Now().Add(time.Second))
	if err == nil {
		t.Fatal("mismatched reason code produced an approval")
	}
	if controller.paused {
		t.Fatal("runtime paused for a request that did not match a policy approval")
	}
}

func TestBudgetDeniedRequestCannotCreateApproval(t *testing.T) {
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: approval-budget\nname: Approval Budget\ntools:\n  allow: [github.update_file]\napproval:\n  required_for: [destructive_write]\nbudget:\n  max_tool_calls: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{
		policy.ControlToolInterception:    true,
		policy.ControlRuntimeEvents:       true,
		policy.ControlApprovalPauseResume: true,
		policy.ControlToolCallBudget:      true,
	}, confirmPause: true}
	record, _, err := manager.Start(snapshot, "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	decision, _, audit, err := manager.EvaluateAndRecord(record.ID, policy.Event{
		Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write",
	}, policy.BudgetState{ToolCalls: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.Deny {
		t.Fatalf("exhausted-budget decision = %#v, want DENY", decision)
	}
	_, err = manager.RequestApproval(context.Background(), run.Approval{
		RunID: record.ID, RequestAuditID: audit.ID, Category: policy.ToolCallRequested,
		Tool: "github.update_file", ActionType: "destructive_write", ReasonCode: decision.ReasonCode,
	}, time.Now())
	if err == nil {
		t.Fatal("budget-denied request produced an approval")
	}
	if controller.pauseCalls != 0 {
		t.Fatalf("runtime paused for a denied request (%d calls)", controller.pauseCalls)
	}
	approvals, err := store.ListApprovals(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 0 {
		t.Fatalf("approvals = %#v, want none", approvals)
	}
}

func TestUnsupportedApprovalCapabilityBlocksRunBeforeAction(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}}
	if _, report, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller); err == nil || report.Ready || controller.pauseCalls != 0 {
		t.Fatalf("unsupported approval runtime start report=%#v err=%v pause calls=%d", report, err, controller.pauseCalls)
	}
}

func TestApprovalResolutionCanRetryAfterRuntimeDoesNotConfirm(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	manager, _ := run.NewManager(store)
	controller := &mockController{capabilities: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}, confirmPause: true}
	record, _, err := manager.Start(approvalSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), time.Now(), controller)
	if err != nil {
		t.Fatal(err)
	}
	request, err := manager.RequestApproval(context.Background(), approvalRequest(t, manager, record, time.Now()), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.DecideApproval(context.Background(), request.ID, run.ApprovalApproved, "approved", testApprover(), time.Now()); err == nil {
		t.Fatal("unconfirmed runtime approval resolution succeeded")
	}
	paused, _ := store.Get(record.ID)
	if paused.Status != run.Paused {
		t.Fatalf("run status after failed resolution = %s", paused.Status)
	}
	controller.confirmResolve = true
	if _, err := manager.DecideApproval(context.Background(), request.ID, run.ApprovalApproved, "approved", testApprover(), time.Now()); err != nil {
		t.Fatal(err)
	}
	resumed, _ := store.Get(record.ID)
	if resumed.Status != run.Active {
		t.Fatalf("run status after retry = %s", resumed.Status)
	}
}

func TestAuditOmitsUntrustedActorText(t *testing.T) {
	store, _ := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	record, _, err := store.Start(testSnapshot(t), "mock", t.TempDir(), identity.AnonymousSelection(), testCapabilities(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := run.NewManager(store)
	_, _, _, err = manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.CredentialAccessRequested, Actor: "Bearer secret-token", CredentialScope: "production:token"}, policy.BudgetState{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.Root(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "Bearer") || strings.Contains(string(data), "credential_scope") {
		t.Fatalf("sensitive actor text persisted: %s", data)
	}
}

func TestStoreRejectsSymlinkedStateAndTrailingDocuments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"sensitive":"outside"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "state.json")); err != nil {
		t.Fatal(err)
	}
	store, _ := run.NewStore(root)
	if _, err := store.List(); err == nil {
		t.Fatal("store followed symlinked state file")
	}

	if err := os.Remove(filepath.Join(root, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"version":"v1","runs":{},"approvals":{},"events":[]} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("store accepted trailing JSON document")
	}
}

type mockController struct {
	capabilities   map[policy.Control]bool
	confirmKill    bool
	confirmPause   bool
	confirmResolve bool
	pauseCalls     int
	paused         bool
	killedRunID    string
	killReason     string
}

func (m *mockController) Capabilities() map[policy.Control]bool { return m.capabilities }

func (m *mockController) PauseForApproval(context.Context, string, string) (bool, error) {
	m.pauseCalls++
	if !m.confirmPause {
		return false, nil
	}
	m.paused = true
	return true, nil
}
func (m *mockController) ResolveApproval(context.Context, string, string, bool) (bool, error) {
	if !m.confirmResolve {
		return false, nil
	}
	m.paused = false
	return true, nil
}
func (m *mockController) Kill(_ context.Context, runID, reason string) (bool, error) {
	m.killedRunID, m.killReason = runID, reason
	return m.confirmKill, nil
}
