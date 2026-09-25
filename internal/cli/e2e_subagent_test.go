package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

func TestSubAgentInstallConflictForceAndUndoEndToEnd(t *testing.T) {
	dataRoot := t.TempDir()
	library := t.TempDir()
	configPath := writeSubAgentCLIConfig(t, library)
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review changes
instructions: Review the diff and report risks.
`)

	project := t.TempDir()
	output, err := executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes")
	if err != nil {
		t.Fatalf("successful install = %v; output=%s", err, output)
	}
	destination := filepath.Join(project, ".claude", "agents", "reviewer.md")
	source := filepath.Join(dataRoot, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	if target, readErr := os.Readlink(destination); readErr != nil || target != source {
		t.Fatalf("installed link = %q, %v; want %q", target, readErr, source)
	}
	if content, readErr := os.ReadFile(destination); readErr != nil || !strings.Contains(string(content), "Review the diff") {
		t.Fatalf("installed representation = %q, %v", content, readErr)
	}
	entry, ok, journalErr := operation.New(filepath.Join(project, ".skill-manager", "journal.json")).Latest()
	if journalErr != nil || !ok || entry.ResourceKind != string(resource.SubAgent) {
		t.Fatalf("install journal = %#v, ok=%v, err=%v", entry, ok, journalErr)
	}

	if _, err := executeSubAgentCLI(t, configPath, "undo", "--project", project, "--yes"); err != nil {
		t.Fatalf("undo successful install = %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("undo left destination = %v, want absent", err)
	}
	if content, err := os.ReadFile(source); err != nil || !strings.Contains(string(content), "Review the diff") {
		t.Fatalf("undo removed canonical rendered source = %q, %v", content, err)
	}

	conflictProject := t.TempDir()
	conflictDestination := filepath.Join(conflictProject, ".codex", "agents", "reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(conflictDestination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictDestination, []byte("user-owned definition"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err = executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", conflictProject, "--target", "codex", "--conflict", "replace")
	if !errors.Is(err, adapter.ErrForceRequired) {
		t.Fatalf("conflict without force = %v, want force-required", err)
	}
	if !strings.Contains(output, "replace conflicting path with absolute link") {
		t.Fatalf("conflict plan output = %q", output)
	}
	assertFileContent(t, conflictDestination, "user-owned definition")

	_, err = executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", conflictProject, "--target", "codex", "--conflict", "replace", "--force")
	if !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("forced conflict without confirmation = %v, want not-confirmed", err)
	}
	assertFileContent(t, conflictDestination, "user-owned definition")

	if _, err := executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", conflictProject, "--target", "codex", "--conflict", "replace", "--force", "--yes"); err != nil {
		t.Fatalf("forced confirmed conflict = %v", err)
	}
	if target, readErr := os.Readlink(conflictDestination); readErr != nil || target != filepath.Join(dataRoot, ".agent-manager", "subagents", "codex", "reviewer.toml") {
		t.Fatalf("replaced conflict link = %q, %v", target, readErr)
	}
	if _, err := executeSubAgentCLI(t, configPath, "undo", "--project", conflictProject, "--yes"); err != nil {
		t.Fatalf("undo forced replacement = %v", err)
	}
	assertFileContent(t, conflictDestination, "user-owned definition")
}

func TestSubAgentUnsupportedAndMissingSkillValidationEndToEnd(t *testing.T) {
	dataRoot := t.TempDir()
	library := t.TempDir()
	configPath := writeSubAgentCLIConfig(t, library)
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review changes
instructions: Review the diff.
`)
	writeSubAgent(t, dataRoot, "memory-reviewer.yaml", `version: v1
id: memory-reviewer
name: Memory Reviewer
role: Review with memory
instructions: Review the diff with memory context.
requiredCapabilities:
  - memory-search
`)
	writeSubAgent(t, dataRoot, "missing-skill.yaml", `version: v1
id: missing-skill
name: Missing Skill
role: Invalid fixture
instructions: This definition must not install.
skills:
  - does-not-exist
`)

	project := t.TempDir()
	if _, err := executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "pi", "--yes"); !errors.Is(err, adapter.ErrSubAgentUnsupported) {
		t.Fatalf("Pi install = %v, want unsupported result", err)
	}
	if entries, readErr := os.ReadDir(project); readErr != nil || len(entries) != 0 {
		t.Fatalf("Pi install changed project: entries=%v err=%v", entries, readErr)
	}

	if _, err := executeSubAgentCLI(t, configPath, "subagents", "install", "memory-reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "codex", "--yes"); !errors.Is(err, adapter.ErrSubAgentUnsupported) {
		t.Fatalf("unsupported capability install = %v, want unsupported result", err)
	}
	if entries, readErr := os.ReadDir(project); readErr != nil || len(entries) != 0 {
		t.Fatalf("unsupported capability install changed project: entries=%v err=%v", entries, readErr)
	}

	commandOutput, err := executeSubAgentCLI(t, configPath, "subagents", "validate", "missing-skill", "--root", dataRoot, "--library", library, "--json")
	if err != nil {
		t.Fatalf("missing Skill validation = %v; output=%s", err, commandOutput)
	}
	var report struct {
		Valid       bool `json:"valid"`
		Diagnostics []struct {
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(commandOutput), &report); err != nil {
		t.Fatalf("decode missing Skill report: %v; output=%s", err, commandOutput)
	}
	if report.Valid || len(report.Diagnostics) != 1 || !strings.Contains(report.Diagnostics[0].Message, "referenced Skill") {
		t.Fatalf("missing Skill report = %#v", report)
	}
	if _, err := executeSubAgentCLI(t, configPath, "subagents", "install", "missing-skill", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing Skill install = %v, want actionable not-found diagnostic", err)
	}
}

func TestSubAgentInstallRollsBackCanonicalSourceWhenPublicationFailsEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-denied publication fixture is Unix-specific")
	}
	dataRoot := t.TempDir()
	library := t.TempDir()
	configPath := writeSubAgentCLIConfig(t, library)
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review changes
instructions: Review the diff.
`)
	project := t.TempDir()
	destinationParent := filepath.Join(project, ".claude", "agents")
	if err := os.MkdirAll(destinationParent, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(destinationParent, "reviewer.md")
	source := filepath.Join(dataRoot, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	journalPath := filepath.Join(project, ".skill-manager", "journal.json")

	// The plan is rendered before publication. Make the already-validated
	// destination parent inaccessible at that boundary. Validation checks
	// parent identity/type, while publication must still fail safely when the
	// anchored open cannot acquire the directory.
	writer := &rollbackPlanWriter{parent: destinationParent}
	command := cli.NewAgentManagerCommand()
	command.SetOut(writer)
	command.SetErr(writer)
	command.SetArgs([]string{"--config", configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"})
	err := command.Execute()
	_ = os.Chmod(destinationParent, 0o755)
	if err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	if writer.chmodErr != nil {
		t.Fatalf("plan fixture chmod failed: %v", writer.chmodErr)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("failed publication left destination: %v", statErr)
	}
	if _, statErr := os.Lstat(source); !os.IsNotExist(statErr) {
		t.Fatalf("failed publication left canonical source: %v", statErr)
	}
	if _, found, journalErr := operation.New(journalPath).Latest(); journalErr != nil || found {
		t.Fatalf("failed publication journal = found=%v err=%v; want no incomplete entry", found, journalErr)
	}
}

func writeSubAgentCLIConfig(t *testing.T, library string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeSubAgentCLI(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()
	command := cli.NewAgentManagerCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(append([]string{"--config", configPath}, args...))
	err := command.Execute()
	return output.String(), err
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != want {
		t.Fatalf("%s = %q, %v; want %q", path, content, err, want)
	}
}

type rollbackPlanWriter struct {
	bytes.Buffer
	parent   string
	trigger  bool
	chmodErr error
}

func (w *rollbackPlanWriter) Write(content []byte) (int, error) {
	if !w.trigger && strings.Contains(string(content), "write managed SubAgent source") {
		w.trigger = true
		w.chmodErr = os.Chmod(w.parent, 0)
		if w.chmodErr != nil {
			return 0, w.chmodErr
		}
	}
	return w.Buffer.Write(content)
}
