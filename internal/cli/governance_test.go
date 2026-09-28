package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

const cliPolicy = `version: v1
kind: agent-policy
id: cli-policy
name: CLI Policy
tools:
  allow: [read_file, github.update_file]
approval:
  required_for: [destructive_write]
`

func TestGovernanceCLIPoliciesRunsApprovalsAndEvalEvidence(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("HOME", configHome)
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	policyDir := filepath.Join(userConfig, "agent-manager", "policies")
	if err := os.MkdirAll(policyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(policyDir, "cli-policy.yaml")
	if err := os.WriteFile(policyPath, []byte(cliPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := executeCLI("policies", "validate", policyPath); err != nil || !strings.Contains(out, "valid policy cli-policy") {
		t.Fatalf("policies validate output=%q err=%v", out, err)
	}
	if out, err := executeCLI("policies", "list"); err != nil || !strings.Contains(out, "cli-policy") {
		t.Fatalf("policies list output=%q err=%v", out, err)
	}
	if out, err := executeCLI("policies", "show", "cli-policy"); err != nil || !strings.Contains(out, "required_for") {
		t.Fatalf("policies show output=%q err=%v", out, err)
	}

	configured, err := policy.Load(strings.NewReader(cliPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, err := run.NewStore(filepath.Join(userConfig, "agent-manager", "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := run.NewManager(store)
	controller := &cliRuntimeController{}
	record, _, err := manager.Start(snapshot, "mock", t.TempDir(), time.Now().UTC(), controller)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := executeCLI("runs", "list", "--json"); err != nil || !strings.Contains(out, record.ID) {
		t.Fatalf("runs list output=%q err=%v", out, err)
	}
	if out, err := executeCLI("runs", "show", record.ID, "--json"); err != nil || !strings.Contains(out, snapshot.Hash) {
		t.Fatalf("runs show output=%q err=%v", out, err)
	}
	if out, err := executeCLI("runs", "events", record.ID); err != nil || !strings.Contains(out, "AgentRunStarted") {
		t.Fatalf("runs events output=%q err=%v", out, err)
	}
	_, _, _, err = manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file", ActionType: "inspect"}, policy.BudgetState{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, _, approvalAudit, err := manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write"}, policy.BudgetState{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	approval, err := manager.RequestApproval(t.Context(), run.Approval{RunID: record.ID, RequestAuditID: approvalAudit.ID, Tool: "github.update_file", ActionType: "destructive_write", ReasonCode: policy.ReasonApprovalRequired}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := executeCLI("approvals", "list"); err != nil || !strings.Contains(out, approval.ID) {
		t.Fatalf("approvals list output=%q err=%v", out, err)
	}
	if out, err := executeCLI("approvals", "approve", approval.ID, "--reason", "human review"); err != nil || !strings.Contains(out, "no runtime action was executed or resumed") {
		t.Fatalf("approvals approve output=%q err=%v", out, err)
	}
	if _, err := executeCLI("runs", "kill", record.ID); err == nil || !strings.Contains(err.Error(), "does not support run termination") {
		t.Fatalf("runs kill err=%v", err)
	}
	current, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != run.Paused {
		t.Fatalf("unsupported kill changed run state to %s", current.Status)
	}
}

func TestGovernanceEvalCLIReadsRunAuditEvents(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("HOME", configHome)
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: eval-cli\nname: Eval CLI\ntools:\n  allow: [read_file]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, _ := run.NewStore(filepath.Join(userConfig, "agent-manager", "runs"))
	record, _, err := store.Start(snapshot, "mock", t.TempDir(), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := run.NewManager(store)
	_, _, _, err = manager.EvaluateAndRecord(record.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file", ActionType: "inspect"}, policy.BudgetState{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	suiteDir := filepath.Join(t.TempDir(), "governance-suite", "cases", "one")
	if err := os.MkdirAll(suiteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	caseYAML := "version: v3\nid: cli-run-case\ncategory: governance\ninput:\n  governance_events: events.json\ngovernance:\n  category: ToolCallRequested\n  tool: read_file\n  action_type: inspect\n  decision: ALLOW\nverifier:\n  type: rule\n"
	if err := os.WriteFile(filepath.Join(suiteDir, "case.yaml"), []byte(caseYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtureEvents, err := store.Events(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixtureData, err := json.Marshal(struct {
		Version string            `json:"version"`
		Events  []run.AuditRecord `json:"events"`
	}{Version: "v1", Events: fixtureEvents})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "events.json"), fixtureData, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := executeCLI("eval", "--governance-run-id", record.ID, "run", filepath.Dir(filepath.Dir(suiteDir)), "--json")
	if err != nil || !strings.Contains(out, snapshot.Hash) || !strings.Contains(out, `"passed":1`) {
		t.Fatalf("eval governance output=%q err=%v", out, err)
	}
}

func executeCLI(args ...string) (string, error) {
	root := cli.NewRootCommand()
	root.SetArgs(args)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	err := root.Execute()
	return out.String(), err
}

type cliRuntimeController struct{}

func (*cliRuntimeController) Capabilities() map[policy.Control]bool {
	return map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}
}
func (*cliRuntimeController) PauseForApproval(context.Context, string, string) (bool, error) {
	return true, nil
}
func (*cliRuntimeController) ResolveApproval(context.Context, string, string, bool) (bool, error) {
	return true, nil
}
func (*cliRuntimeController) Kill(context.Context, string, string) (bool, error) { return false, nil }
