package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
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
