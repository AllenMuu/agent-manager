package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
)

func executeAgentManager(t *testing.T, args ...string) string {
	t.Helper()
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("args %v: %v; output=%s", args, err, out.String())
	}
	return out.String()
}

func TestTaskAndArtifactCommandsCreateAndInspectIntent(t *testing.T) {
	project := t.TempDir()
	out := executeAgentManager(t, "task", "init", "--project", project, "--id", "task-cli", "--summary", "CLI protocol")
	if !strings.Contains(out, "created task task-cli") {
		t.Fatalf("init output = %q", out)
	}
	out = executeAgentManager(t, "artifacts", "list", "--project", project, "task-cli")
	if !strings.Contains(out, "intent") || !strings.Contains(out, "intent.yaml") {
		t.Fatalf("list output = %q", out)
	}
	out = executeAgentManager(t, "artifacts", "render", "--project", project, ".agents/tasks/task-cli/intent.yaml")
	if !strings.Contains(out, "# intent: task-cli") || !strings.Contains(out, "CLI protocol") {
		t.Fatalf("render output = %q", out)
	}
}

func TestRolesCommandExposesCanonicalContracts(t *testing.T) {
	out := executeAgentManager(t, "roles", "--json")
	for _, want := range []string{"planner", "implementer", "reviewer", "verifier", "intent", "verification"} {
		if !strings.Contains(out, want) {
			t.Fatalf("roles output = %q; missing %q", out, want)
		}
	}
}

func TestCanonicalTaskArtifactsFlowThroughRoleContexts(t *testing.T) {
	project := t.TempDir()
	taskID := "workflow-1"
	executeAgentManager(t, "task", "init", "--project", project, "--id", taskID, "--summary", "Implement a local workflow")
	save := func(kind artifact.Kind, fields map[string]any) {
		t.Helper()
		doc := artifact.New(kind, taskID, project, time.Now())
		for name, value := range fields {
			doc.Set(name, value)
		}
		data, err := doc.YAML()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), string(kind)+".yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		executeAgentManager(t, "artifacts", "save", taskID, path, "--project", project, "--yes")
	}
	checkContext := func(roleID role.ID, wantKinds ...artifact.Kind) {
		t.Helper()
		output := executeAgentManager(t, "roles", "context", taskID, "--project", project, "--role", string(roleID), "--agent", "codex")
		var handoff struct {
			Binding role.Binding       `json:"binding"`
			Context taskcontext.Bundle `json:"context"`
		}
		if err := json.Unmarshal([]byte(output), &handoff); err != nil {
			t.Fatalf("decode role context: %v; output=%s", err, output)
		}
		if len(handoff.Context.Missing) != 0 || len(handoff.Context.Artifacts) != len(wantKinds) {
			t.Fatalf("%s context = %#v", roleID, handoff.Context)
		}
		for _, kind := range wantKinds {
			if _, ok := handoff.Context.Artifacts[kind]; !ok {
				t.Fatalf("%s missing %s", roleID, kind)
			}
		}
		if handoff.Binding.Supported || len(handoff.Binding.Missing) == 0 {
			t.Fatalf("unverified runtime permissions were claimed: %#v", handoff.Binding)
		}
		if len(handoff.Binding.Inputs) != len(wantKinds) || len(handoff.Binding.Outputs) != 1 {
			t.Fatalf("handoff lost its artifact contract: %#v", handoff.Binding)
		}
	}
	save(artifact.Spec, map[string]any{"decisions": []map[string]any{{"id": "D1", "decision": "Use local files", "rationale": "Reproducible"}}})
	checkContext(role.Planner, artifact.Intent, artifact.Spec)
	save(artifact.Plan, map[string]any{"steps": []map[string]any{{"id": "P1", "description": "Implement a file workflow", "verification": "go test ./..."}}})
	checkContext(role.Implementer, artifact.Intent, artifact.Spec, artifact.Plan)
	save(artifact.Implementation, map[string]any{"summary": "Added the workflow", "changed_files": []string{"workflow.go"}})
	checkContext(role.Verifier, artifact.Intent, artifact.Spec, artifact.Plan, artifact.Implementation)
	save(artifact.Verification, map[string]any{"status": "pass", "checks": []map[string]any{{"name": "unit-tests", "status": "pass", "command": "go test ./...", "evidence": "all packages passed"}}})
	shown := executeAgentManager(t, "artifacts", "show", taskID, "verification", "--project", project)
	if !strings.Contains(shown, "status: pass") {
		t.Fatalf("verification artifact = %q", shown)
	}
}
