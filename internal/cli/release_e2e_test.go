package cli_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

// TestDocumentedReleaseWorkflowEndToEnd exercises the documented control-plane
// paths in one isolated fixture. It intentionally keeps provider-owned Memory
// data outside the filesystem operation journal so an undo can never erase an
// explicit promotion.
func TestDocumentedReleaseWorkflowEndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	library := t.TempDir()
	writeSkill(t, library, "demo", "Demo", "demo skill", "release\n")
	dataRoot := t.TempDir()
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review changes
instructions: Review the diff and report risks.
`)
	project := t.TempDir()
	store := filepath.Join(t.TempDir(), "memory-store")
	if err := os.WriteFile(store, []byte("existing knowledge\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryFixtureConfig(t, library, store, "read,write,search", "user,project")
	journalPath := filepath.Join(project, ".skill-manager", "journal.json")

	// The compatibility entrypoint is a documented migration path and must
	// retain the same read-only catalog behavior while naming agent-manager.
	output, err := executeEntrypoint(t, cliEntrypoint{name: "skill-manager", new: cli.NewSkillManagerCommand}, configPath, "search", "demo")
	if err != nil || !strings.Contains(output, "demo") || !strings.Contains(output, "deprecated") || !strings.Contains(output, "agent-manager") {
		t.Fatalf("compatibility search = %q, %v; want result and migration notice", output, err)
	}

	// Inventory is read-only and must expose the supported adapter surface.
	output, err = executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "agents", "--project", project, "--json")
	if err != nil {
		t.Fatalf("agent inventory = %v; output=%s", err, output)
	}
	var inventory []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(output), &inventory); err != nil {
		t.Fatalf("decode agent inventory: %v; output=%s", err, output)
	}
	if !containsInventoryAgent(inventory, "codex") {
		t.Fatalf("agent inventory = %#v; want codex adapter", inventory)
	}

	link := filepath.Join(project, ".codex", "skills", "demo")
	output, err = executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "add", "demo", "--project", project, "--target", "codex")
	if !errors.Is(err, operation.ErrNotConfirmed) || !strings.Contains(output, "create absolute link") {
		t.Fatalf("unconfirmed Skill activation = %v; output=%q", err, output)
	}
	assertAbsent(t, link)
	assertJournalEmpty(t, journalPath)

	output, err = executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	if err != nil || !strings.Contains(output, "create absolute link") {
		t.Fatalf("confirmed Skill activation = %v; output=%q", err, output)
	}
	assertSymlinkTarget(t, link, filepath.Join(library, "demo"))
	entry, found, err := operation.New(journalPath).Latest()
	if err != nil || !found || entry.ResourceKind != string(resource.Skill) || entry.Operation != "activate" {
		t.Fatalf("Skill journal = %#v, found=%v, err=%v", entry, found, err)
	}

	output = runCLI(t, configPath, "list", "--project", project)
	if !strings.Contains(output, "codex\tdemo\tmanaged") {
		t.Fatalf("Skill inventory = %q; want managed activation", output)
	}
	output, err = executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "remove", "demo", "--project", project, "--target", "codex")
	if !errors.Is(err, operation.ErrNotConfirmed) || !strings.Contains(output, "remove managed link") {
		t.Fatalf("unconfirmed Skill removal = %v; output=%q", err, output)
	}
	assertSymlinkTarget(t, link, filepath.Join(library, "demo"))
	if _, err := executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "remove", "demo", "--project", project, "--target", "codex", "--yes"); err != nil {
		t.Fatalf("confirmed Skill removal = %v", err)
	}
	assertAbsent(t, link)
	if entry, found, err := operation.New(journalPath).Latest(); err != nil || !found || entry.Operation != "remove" {
		t.Fatalf("remove journal = %#v, found=%v, err=%v", entry, found, err)
	}
	if _, err := executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "undo", "--project", project, "--yes"); err != nil {
		t.Fatalf("undo Skill removal = %v", err)
	}
	assertSymlinkTarget(t, link, filepath.Join(library, "demo"))
	if _, err := executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "undo", "--project", project, "--yes"); err != nil {
		t.Fatalf("undo Skill activation = %v", err)
	}
	assertAbsent(t, link)
	assertJournalEmpty(t, journalPath)

	// Canonical SubAgent inspection is independent from installation. Its
	// filesystem representation is still confirmed, journaled, and undoable.
	output, err = executeSubAgentCLI(t, configPath, "subagents", "list", "--root", dataRoot, "--library", library, "--json")
	if err != nil || !strings.Contains(output, `"id":"reviewer"`) {
		t.Fatalf("SubAgent list = %v; output=%s", err, output)
	}
	output, err = executeSubAgentCLI(t, configPath, "subagents", "validate", "reviewer", "--root", dataRoot, "--library", library, "--json")
	if err != nil || !strings.Contains(output, `"valid":true`) {
		t.Fatalf("SubAgent validate = %v; output=%s", err, output)
	}
	subagentSource := filepath.Join(dataRoot, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	subagentDestination := filepath.Join(project, ".claude", "agents", "reviewer.md")
	output, err = executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code")
	if !errors.Is(err, operation.ErrNotConfirmed) || !strings.Contains(output, "place resource") {
		t.Fatalf("unconfirmed SubAgent install = %v; output=%q", err, output)
	}
	assertAbsent(t, subagentSource)
	assertAbsent(t, subagentDestination)
	assertJournalEmpty(t, journalPath)
	if _, err := executeSubAgentCLI(t, configPath, "subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"); err != nil {
		t.Fatalf("confirmed SubAgent install = %v", err)
	}
	assertSymlinkTarget(t, subagentDestination, subagentSource)
	if entry, found, err := operation.New(journalPath).Latest(); err != nil || !found || entry.ResourceKind != string(resource.SubAgent) {
		t.Fatalf("SubAgent journal = %#v, found=%v, err=%v", entry, found, err)
	}
	if _, err := executeEntrypoint(t, cliEntrypoint{name: "agent-manager", new: cli.NewAgentManagerCommand}, configPath, "undo", "--project", project, "--yes"); err != nil {
		t.Fatalf("undo SubAgent install = %v", err)
	}
	assertAbsent(t, subagentDestination)
	if _, err := os.Stat(subagentSource); err != nil {
		t.Fatalf("undo removed canonical SubAgent source: %v", err)
	}
	assertJournalEmpty(t, journalPath)

	// Memory status is read-only; promotion is the separately confirmed,
	// provider-owned mutation and intentionally has no filesystem journal entry.
	original, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	statusOutput := executeMemoryStatus(t, configPath, "--json", "--project", project)
	var status memoryStatusJSON
	if err := json.Unmarshal([]byte(statusOutput), &status); err != nil {
		t.Fatalf("decode Memory status: %v; output=%s", err, statusOutput)
	}
	if status.State != "available" || !status.Available || status.Provider != "file" || strings.Contains(statusOutput, store) {
		t.Fatalf("Memory status = %#v; output=%s", status, statusOutput)
	}
	if contents, readErr := os.ReadFile(store); readErr != nil || string(contents) != string(original) {
		t.Fatalf("Memory status changed provider store: %q, %v", contents, readErr)
	}
	output, err = executeMemoryPromote(t, configPath, "n\n", "user", "release knowledge", false)
	if err != nil || !strings.Contains(output, "Memory promotion plan") || strings.Contains(output, "release knowledge") {
		t.Fatalf("declined Memory promotion = %v; output=%q", err, output)
	}
	if contents, readErr := os.ReadFile(store); readErr != nil || string(contents) != string(original) {
		t.Fatalf("declined Memory promotion changed provider store: %q, %v", contents, readErr)
	}
	if _, err := executeMemoryPromote(t, configPath, "", "user", "release knowledge", true); err != nil {
		t.Fatalf("confirmed Memory promotion = %v", err)
	}
	if contents, readErr := os.ReadFile(store); readErr != nil || string(contents) != string(original)+"release knowledge\n" {
		t.Fatalf("confirmed Memory promotion store = %q, %v", contents, readErr)
	}
	assertJournalEmpty(t, journalPath)
	if err := runCLIErr(t, configPath, "undo", "--project", project, "--yes"); err == nil {
		t.Fatal("undo unexpectedly found a filesystem journal entry for provider-owned Memory promotion")
	}
}

func containsInventoryAgent(items []struct {
	ID string `json:"id"`
}, want string) bool {
	for _, item := range items {
		if item.ID == want {
			return true
		}
	}
	return false
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s = %v; want absent", path, err)
	}
}

func assertSymlinkTarget(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil || got != want {
		t.Fatalf("symlink %s = %q, %v; want %q", path, got, err, want)
	}
}

func assertJournalEmpty(t *testing.T, path string) {
	t.Helper()
	if _, found, err := operation.New(path).Latest(); err != nil || found {
		t.Fatalf("journal %s = found=%v, err=%v; want empty", path, found, err)
	}
}
