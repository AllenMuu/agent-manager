package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/spf13/cobra"
)

// e2eFixture builds a configured library with one eligible skill and an
// empty project, plus a CLI config pointing at them.
func e2eFixture(t *testing.T) (library, project, configPath string) {
	t.Helper()
	library = t.TempDir()
	writeSkill(t, library, "demo", "Demo", "demo skill", "tag\n")
	project = t.TempDir()
	configPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return library, project, configPath
}

func runCLI(t *testing.T, configPath string, args ...string) string {
	t.Helper()
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"--config", configPath}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("args %v: %v", args, err)
	}
	return out.String()
}

func runCLIErr(t *testing.T, configPath string, args ...string) error {
	t.Helper()
	root := cli.NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"--config", configPath}, args...))
	return root.Execute()
}

type cliEntrypoint struct {
	name string
	new  func() *cobra.Command
}

func executeEntrypoint(t *testing.T, entrypoint cliEntrypoint, configPath string, args ...string) (string, error) {
	t.Helper()
	root := entrypoint.new()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"--config", configPath}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestAgentManagerPrimaryCommandAndSkillManagerCompatibilityAlias(t *testing.T) {
	_, _, configPath := e2eFixture(t)

	primary := cli.NewAgentManagerCommand()
	if primary.Use != "agent-manager" {
		t.Fatalf("primary command = %q, want agent-manager", primary.Use)
	}
	primaryOut := &bytes.Buffer{}
	primary.SetOut(primaryOut)
	primary.SetErr(primaryOut)
	primary.SetArgs([]string{"--config", configPath, "search", "demo"})
	if err := primary.Execute(); err != nil {
		t.Fatalf("primary command: %v", err)
	}
	if !strings.Contains(primaryOut.String(), "demo") {
		t.Fatalf("primary output=%q", primaryOut.String())
	}

	legacy := cli.NewSkillManagerCommand()
	legacyOut := &bytes.Buffer{}
	legacy.SetOut(legacyOut)
	legacy.SetErr(legacyOut)
	legacy.SetArgs([]string{"--config", configPath, "search", "demo"})
	if err := legacy.Execute(); err != nil {
		t.Fatalf("legacy command: %v", err)
	}
	if !strings.Contains(legacyOut.String(), "deprecated") || !strings.Contains(legacyOut.String(), "agent-manager") {
		t.Fatalf("legacy output=%q; want migration notice", legacyOut.String())
	}
}

func TestProjectSkillActivationAndUndoParityAcrossCLIEntrypoints(t *testing.T) {
	entrypoints := []cliEntrypoint{
		{name: "agent-manager", new: cli.NewAgentManagerCommand},
		{name: "skill-manager", new: cli.NewSkillManagerCommand},
	}
	targets := []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi}
	for entrypointIndex, entrypoint := range entrypoints {
		for _, target := range targets {
			t.Run(entrypoint.name+"/"+string(target), func(t *testing.T) {
				library, project, configPath := e2eFixture(t)
				if _, err := executeEntrypoint(t, entrypoint, configPath, "add", "demo", "--project", project, "--target", string(target), "--yes"); err != nil {
					t.Fatalf("activate via %s: %v", entrypoint.name, err)
				}

				a, ok := adapter.For(target)
				if !ok {
					t.Fatalf("target %q has no adapter", target)
				}
				link := a.ProjectSkillPath(project, "demo")
				got, err := os.Readlink(link)
				if err != nil {
					t.Fatalf("read %s activation link: %v", target, err)
				}
				if !filepath.IsAbs(got) {
					t.Fatalf("%s activation target = %q, want absolute path", target, got)
				}
				if got != filepath.Join(library, "demo") {
					t.Fatalf("%s activation target = %q, want %q", target, got, filepath.Join(library, "demo"))
				}

				journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
				entry, ok, err := journal.Latest()
				if err != nil || !ok {
					t.Fatalf("%s activation journal = %#v, ok=%v, err=%v", target, entry, ok, err)
				}
				if entry.Version != "v1" || entry.ResourceKind != "skill" || entry.Operation != "activate" {
					t.Fatalf("%s activation journal metadata = %#v", target, entry)
				}
				if len(entry.Before) != 1 || entry.Before[0].Exists || len(entry.After) != 1 || entry.After[0].Path != link || !entry.After[0].Exists {
					t.Fatalf("%s activation journal snapshots = %#v", target, entry)
				}

				undoEntrypoint := entrypoints[(entrypointIndex+1)%len(entrypoints)]
				if _, err := executeEntrypoint(t, undoEntrypoint, configPath, "undo", "--project", project, "--yes"); err != nil {
					t.Fatalf("undo via %s after activation via %s: %v", undoEntrypoint.name, entrypoint.name, err)
				}
				if _, err := os.Lstat(link); !os.IsNotExist(err) {
					t.Fatalf("undo via %s left %s: %v", undoEntrypoint.name, link, err)
				}
				if _, err := os.Stat(filepath.Join(got, "SKILL.md")); err != nil {
					t.Fatalf("undo via %s changed library source %s: %v", undoEntrypoint.name, got, err)
				}
				if _, ok, err := journal.Latest(); err != nil || ok {
					t.Fatalf("%s journal after undo = ok=%v, err=%v; want empty", target, ok, err)
				}
			})
		}
	}
}

func TestLegacySkillActivationRecoveryAcrossCLIEntrypoints(t *testing.T) {
	entrypoints := []cliEntrypoint{
		{name: "agent-manager", new: cli.NewAgentManagerCommand},
		{name: "skill-manager", new: cli.NewSkillManagerCommand},
	}
	for entrypointIndex, entrypoint := range entrypoints {
		for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi} {
			t.Run(entrypoint.name+"/"+string(target), func(t *testing.T) {
				library, project, configPath := e2eFixture(t)
				legacyConfig, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := executeEntrypoint(t, entrypoint, configPath, "add", "demo", "--project", project, "--target", string(target), "--yes"); err != nil {
					t.Fatalf("legacy activation via %s: %v", entrypoint.name, err)
				}

				journalPath := filepath.Join(project, ".skill-manager", "journal.json")
				contents, err := os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				var records []map[string]any
				if err := json.Unmarshal(contents, &records); err != nil {
					t.Fatalf("decode migrated journal: %v", err)
				}
				if len(records) != 1 {
					t.Fatalf("legacy activation journal records = %d, want one", len(records))
				}
				delete(records[0], "version")
				delete(records[0], "resourceKind")
				legacyJournal, err := json.Marshal(records)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(journalPath, legacyJournal, 0o644); err != nil {
					t.Fatal(err)
				}

				a, _ := adapter.For(target)
				link := a.ProjectSkillPath(project, "demo")
				recoveryEntrypoint := entrypoints[(entrypointIndex+1)%len(entrypoints)]
				listOutput, err := executeEntrypoint(t, recoveryEntrypoint, configPath, "list", "--project", project)
				if err != nil {
					t.Fatalf("list migrated project via %s: %v", recoveryEntrypoint.name, err)
				}
				if !strings.Contains(listOutput, string(target)+"\tdemo\tmanaged") {
					t.Fatalf("list migrated project via %s = %q; want managed %s/demo", recoveryEntrypoint.name, listOutput, target)
				}
				if _, err := os.Lstat(link); err != nil {
					t.Fatalf("migrated activation link missing before undo: %v", err)
				}
				if _, err := executeEntrypoint(t, recoveryEntrypoint, configPath, "undo", "--project", project, "--yes"); err != nil {
					t.Fatalf("undo legacy activation via %s: %v", recoveryEntrypoint.name, err)
				}
				if _, err := os.Lstat(link); !os.IsNotExist(err) {
					t.Fatalf("legacy activation link survived undo: %v", err)
				}
				if _, err := os.Stat(filepath.Join(library, "demo", "SKILL.md")); err != nil {
					t.Fatalf("legacy undo removed or changed library Skill: %v", err)
				}
				_, hasJournalEntry, err := operation.New(journalPath).Latest()
				if err != nil {
					t.Fatalf("legacy journal after undo: %v", err)
				}
				if hasJournalEntry {
					t.Fatal("legacy journal retained an entry after undo")
				}
				if got, err := os.ReadFile(configPath); err != nil || string(got) != string(legacyConfig) {
					t.Fatalf("legacy config changed during migration: %q, %v", got, err)
				}
			})
		}
	}
}

func TestAgentsInventoryReportsDeclaredResourceCapabilitiesAsJSON(t *testing.T) {
	root := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"agents", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("agents inventory: %v", err)
	}
	for _, want := range []string{`"id":"claude-code"`, `"id":"codex"`, `"id":"pi"`, `"resourceKinds":["skill"]`, `"filesystem-write"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("inventory output=%q; missing %s", out.String(), want)
		}
	}
}

func TestAgentsInventoryReportsLocalAdapterAvailability(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"agents", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("agents inventory: %v", err)
	}

	var items []struct {
		ID           string `json:"id"`
		Availability string `json:"availability"`
	}
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}
	availability := make(map[string]string, len(items))
	for _, item := range items {
		availability[item.ID] = item.Availability
	}
	for id, want := range map[string]string{"claude-code": "configured", "codex": "detected", "pi": "unavailable"} {
		if got := availability[id]; got != want {
			t.Errorf("%s availability = %q, want %q; inventory=%s", id, got, want, out.String())
		}
	}
}

func TestAgentsInventoryDistinguishesUnsupportedProjectAgent(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".foo", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"agents", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("agents inventory: %v", err)
	}

	var items []struct {
		ID     string   `json:"id"`
		Status string   `json:"status"`
		Kinds  []string `json:"resourceKinds"`
	}
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("decode inventory: %v; output=%s", err, out.String())
	}
	for _, item := range items {
		if item.ID == "foo" {
			if item.Status != "unsupported" || len(item.Kinds) != 0 {
				t.Fatalf("unsupported item = %#v, want unsupported with no resource kinds", item)
			}
			return
		}
	}
	t.Fatalf("inventory=%s; missing unsupported foo agent", out.String())
}

func TestAgentsInventorySkipsUnsupportedSymlinkSkillsLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(external, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(project, ".foo", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".bar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".bar", "skills"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"agents", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("agents inventory: %v", err)
	}
	for _, item := range mustDecodeAgentInventory(t, out.Bytes()) {
		if item.ID == "foo" || item.ID == "bar" {
			t.Fatalf("unsupported location was inventoried: %#v; inventory=%s", item, out.String())
		}
	}
}

func TestAgentsInventoryUsesRecognizedProjectLocationForAvailability(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"agents", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("agents inventory: %v", err)
	}
	for _, item := range mustDecodeAgentInventory(t, out.Bytes()) {
		if item.ID == "codex" {
			if item.Availability != "configured" {
				t.Fatalf("codex availability = %q, want configured; inventory=%s", item.Availability, out.String())
			}
			return
		}
	}
	t.Fatalf("inventory=%s; missing codex agent", out.String())
}

func mustDecodeAgentInventory(t *testing.T, contents []byte) []struct {
	ID           string `json:"id"`
	Availability string `json:"availability"`
} {
	t.Helper()
	var items []struct {
		ID           string `json:"id"`
		Availability string `json:"availability"`
	}
	if err := json.Unmarshal(contents, &items); err != nil {
		t.Fatalf("decode inventory: %v; output=%s", err, contents)
	}
	return items
}

func TestEndToEndInitInstallsOperatorSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(home, dir, "skills", "skill-manager-operator", "SKILL.md")); err != nil {
			t.Fatalf("operator skill missing for %s: %v", dir, err)
		}
	}
}

func TestEndToEndListRemoveAndUndo(t *testing.T) {
	_, project, configPath := e2eFixture(t)
	out := runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	if !strings.Contains(out, "create absolute link") {
		t.Fatalf("add output=%q", out)
	}
	link := filepath.Join(project, ".codex", "skills", "demo")

	out = runCLI(t, configPath, "list", "--project", project)
	if !strings.Contains(out, "codex\tdemo\tmanaged") {
		t.Fatalf("list output=%q", out)
	}

	runCLI(t, configPath, "undo", "--project", project, "--yes")
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("undo left the link: %v", err)
	}

	runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	runCLI(t, configPath, "remove", "demo", "--project", project, "--target", "codex", "--yes")
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("remove left the link: %v", err)
	}
}

func TestListInventoryJSONLabelsUnsupportedLocations(t *testing.T) {
	library, project, configPath := e2eFixture(t)
	if err := os.MkdirAll(filepath.Join(project, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".foo", "skills", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".foo", "skills", "local", "SKILL.md"), []byte("---\nname: local\ndescription: local\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "missing"), filepath.Join(project, ".claude", "skills", "missing")); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	if !strings.Contains(out, "create absolute link") {
		t.Fatalf("add output=%q", out)
	}

	root := cli.NewAgentManagerCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"--config", configPath, "list", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list inventory: %v", err)
	}
	var items []struct {
		Target     string `json:"target"`
		Identifier string `json:"identifier"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &items); err != nil {
		t.Fatalf("decode list inventory: %v; output=%s", err, output.String())
	}
	want := map[string]string{"codex/demo": "managed", "claude-code/missing": "orphaned", "foo/local": "unsupported"}
	for _, item := range items {
		delete(want, item.Target+"/"+item.Identifier)
		if item.Target == "foo" && item.Status != "unsupported" {
			t.Fatalf("unsupported item = %#v", item)
		}
	}
	if len(want) != 0 {
		t.Fatalf("list inventory=%s; missing %#v", output.String(), want)
	}
}

func TestListInventorySkipsUnsupportedRegularSkillsLocation(t *testing.T) {
	_, project, configPath := e2eFixture(t)
	if err := os.Mkdir(filepath.Join(project, ".foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".foo", "skills"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := cli.NewAgentManagerCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"--config", configPath, "list", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list inventory: %v", err)
	}
	var items []struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(output.Bytes(), &items); err != nil {
		t.Fatalf("decode list inventory: %v; output=%s", err, output.String())
	}
	for _, item := range items {
		if item.Target == "foo" {
			t.Fatalf("regular unsupported skills location was inventoried: %#v", item)
		}
	}
}

func TestEndToEndAdoptAndFork(t *testing.T) {
	library, project, configPath := e2eFixture(t)
	adoptable := filepath.Join(project, ".codex", "skills", "adopted")
	if err := os.MkdirAll(adoptable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adoptable, "SKILL.md"), []byte("---\nname: adopted\ndescription: adopted\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, configPath, "adopt", "adopted", "--project", project, "--target", "codex", "--yes")
	if _, err := os.Stat(filepath.Join(library, "adopted", "SKILL.md")); err != nil {
		t.Fatalf("adopted skill missing from library: %v", err)
	}
	if got, err := os.Readlink(adoptable); err != nil || got != filepath.Join(library, "adopted") {
		t.Fatalf("adopted path = %q, %v; want library link", got, err)
	}

	runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	forked := filepath.Join(project, ".codex", "skills", "demo")
	runCLI(t, configPath, "fork", "demo", "--project", project, "--target", "codex", "--yes")
	info, err := os.Lstat(forked)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("fork left a link instead of an independent directory")
	}
	if _, err := os.Stat(filepath.Join(forked, "SKILL.md")); err != nil {
		t.Fatalf("forked skill content missing: %v", err)
	}
}

func TestEndToEndDoctorReportsOrphanedLink(t *testing.T) {
	library, project, configPath := e2eFixture(t)
	runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")
	link := filepath.Join(project, ".codex", "skills", "demo")
	// Break the link destination to make the managed link orphaned.
	if err := os.RemoveAll(filepath.Join(library, "demo")); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, configPath, "doctor", "--project", project)
	if !strings.Contains(out, link) {
		t.Fatalf("doctor output=%q; want the orphaned link path", out)
	}
}

func TestEndToEndReconcileRelinksMovedLibrary(t *testing.T) {
	root := t.TempDir()
	oldLibrary := filepath.Join(root, "old-library")
	newLibrary := filepath.Join(root, "new-library")
	if err := os.MkdirAll(filepath.Join(oldLibrary, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldLibrary, "demo", "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	link := filepath.Join(project, ".codex", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(oldLibrary, "demo"), link); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+oldLibrary+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, configPath, "add", "demo", "--project", project, "--target", "codex", "--yes")

	// Move the library and repoint the config at its new location.
	if err := os.Rename(oldLibrary, newLibrary); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("library: "+newLibrary+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, configPath, "reconcile", "--project", project, "--yes")
	if got, err := os.Readlink(link); err != nil || got != filepath.Join(newLibrary, "demo") {
		t.Fatalf("reconciled link = %q, %v; want new library link", got, err)
	}
}

func TestEndToEndUndoEmptyJournalFails(t *testing.T) {
	_, project, configPath := e2eFixture(t)
	if err := runCLIErr(t, configPath, "undo", "--project", project, "--yes"); err == nil {
		t.Fatal("undo on empty journal succeeded")
	}
}
