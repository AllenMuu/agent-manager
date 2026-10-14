package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestInstallActivatesMultipleSelectedSkillsInOneOperation(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "one", "One", "first", "tag\n")
	writeSkill(t, library, "two", "Two", "second", "tag\n")
	project := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "install", "one", "two", "--project", project, "--target", "codex", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		link := filepath.Join(project, ".codex", "skills", id)
		if got, err := os.Readlink(link); err != nil || got != filepath.Join(library, id) {
			t.Fatalf("%s link = %q, %v", id, got, err)
		}
		if !strings.Contains(out.String(), link) {
			t.Fatalf("preview omitted %s: %q", link, out.String())
		}
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	entry, ok, err := journal.Latest()
	if err != nil || !ok || len(entry.After) != 2 {
		t.Fatalf("journal = %#v, ok=%v, err=%v", entry, ok, err)
	}
}

func TestInstallWithoutYesOnlyDisplaysPlan(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "demo", "Demo", "demo", "tag\n")
	project := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "install", "demo", "--project", project, "--target", "codex"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("install error = %v, want confirmation error", err)
	}
	if !strings.Contains(out.String(), filepath.Join(project, ".codex", "skills", "demo")) {
		t.Fatalf("preview missing destination: %q", out.String())
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed install changed project: %v", err)
	}
}

func TestInstallConflictDisplaysPlanAndPreservesUnmanagedContent(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "demo", "Demo", "demo", "tag\n")
	project := t.TempDir()
	destination := filepath.Join(project, ".codex", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("user data"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "install", "demo", "--project", project, "--target", "codex", "--yes"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(out.String(), "refuse conflicting destination") {
		t.Fatalf("conflict plan missing: %q", out.String())
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "user data" {
		t.Fatalf("unmanaged destination = %q, %v", contents, err)
	}
}

func TestInstallRequiresTargets(t *testing.T) {
	root := cli.NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"install", "demo"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("install error = %v, want target validation", err)
	}
}

func TestGlobalInstallRejectsExplicitProjectPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	command := cli.NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"install", "demo", "--global", "--project", project, "--target", "codex"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("global install error = %v, want explicit scope conflict", err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("invalid scope request changed project: %v", err)
	}
}

func TestGlobalInstallSelectsVersionAndSupportsSafeRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	library := filepath.Join(t.TempDir(), "skills")
	repo, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	firstSourceRoot := t.TempDir()
	writeSkill(t, firstSourceRoot, "demo", "Demo", "first", "tag\n")
	firstSource := filepath.Join(firstSourceRoot, "demo")
	first, err := repo.AddVersion("demo", firstSource, skillrepo.VersionMetadata{Version: "1.0.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	secondSourceRoot := t.TempDir()
	writeSkill(t, secondSourceRoot, "demo", "Demo", "second", "tag\n")
	secondSource := filepath.Join(secondSourceRoot, "demo")
	if _, err := repo.AddVersion("demo", secondSource, skillrepo.VersionMetadata{Version: "2.0.0"}, true); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	install := cli.NewRootCommand()
	installOut := &bytes.Buffer{}
	install.SetOut(installOut)
	install.SetErr(installOut)
	install.SetArgs([]string{"--config", configPath, "install", "demo", "--global", "--target", "claude-code", "--target", "codex", "--target", "pi", "--version", "demo=1.0.0", "--yes"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}
	destinations := []string{
		filepath.Join(home, ".claude", "skills", "demo"),
		filepath.Join(home, ".codex", "skills", "demo"),
		filepath.Join(home, ".pi", "agent", "skills", "demo"),
	}
	for _, destination := range destinations {
		if got, err := os.Readlink(destination); err != nil || got != first.Path {
			t.Fatalf("global Skill link %s = %q, %v; want selected version %q", destination, got, err, first.Path)
		}
		if !strings.Contains(installOut.String(), destination) {
			t.Fatalf("global preview omitted its destination %s: %q", destination, installOut.String())
		}
	}

	list := cli.NewRootCommand()
	listOut := &bytes.Buffer{}
	list.SetOut(listOut)
	list.SetErr(listOut)
	list.SetArgs([]string{"--config", configPath, "list", "--global", "--json"})
	if err := list.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), `"status":"managed"`) {
		t.Fatalf("global list did not report managed links: %q", listOut.String())
	}

	remove := cli.NewRootCommand()
	remove.SetOut(&bytes.Buffer{})
	remove.SetErr(&bytes.Buffer{})
	remove.SetArgs([]string{"--config", configPath, "remove", "demo", "--global", "--target", "codex", "--yes"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(destinations[1]); !os.IsNotExist(err) {
		t.Fatalf("removed global Codex link remains: %v", err)
	}
	for _, destination := range []string{destinations[0], destinations[2]} {
		if got, err := os.Readlink(destination); err != nil || got != first.Path {
			t.Fatalf("other global link changed during removal: %s = %q, %v", destination, got, err)
		}
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("removal deleted retained repository version: %v", err)
	}

	undo := cli.NewRootCommand()
	undo.SetOut(&bytes.Buffer{})
	undo.SetErr(&bytes.Buffer{})
	undo.SetArgs([]string{"--config", configPath, "undo", "--global", "--yes"})
	if err := undo.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(destinations[1]); err != nil || got != first.Path {
		t.Fatalf("global link after undo = %q, %v; want selected version %q", got, err, first.Path)
	}
}

func TestUndoGlobalDoesNotUndoProjectOperationRootedAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	library := t.TempDir()
	writeSkill(t, library, "demo", "Demo", "test skill", "test\n")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectInstall := cli.NewRootCommand()
	projectInstall.SetOut(&bytes.Buffer{})
	projectInstall.SetErr(&bytes.Buffer{})
	projectInstall.SetArgs([]string{"--config", configPath, "install", "demo", "--project", home, "--target", "codex", "--yes"})
	if err := projectInstall.Execute(); err != nil {
		t.Fatalf("project install rooted at home: %v", err)
	}
	projectLink := filepath.Join(home, ".codex", "skills", "demo")

	globalInstall := cli.NewRootCommand()
	globalInstall.SetOut(&bytes.Buffer{})
	globalInstall.SetErr(&bytes.Buffer{})
	globalInstall.SetArgs([]string{"--config", configPath, "install", "demo", "--global", "--target", "pi", "--yes"})
	if err := globalInstall.Execute(); err != nil {
		t.Fatalf("global install: %v", err)
	}
	globalLink := filepath.Join(home, ".pi", "agent", "skills", "demo")

	undo := cli.NewRootCommand()
	undo.SetOut(&bytes.Buffer{})
	undo.SetErr(&bytes.Buffer{})
	undo.SetArgs([]string{"--config", configPath, "undo", "--global", "--yes"})
	if err := undo.Execute(); err != nil {
		t.Fatalf("global undo: %v", err)
	}
	if _, err := os.Lstat(globalLink); !os.IsNotExist(err) {
		t.Fatalf("global undo left global link in place: %v", err)
	}
	if got, err := os.Readlink(projectLink); err != nil || got != filepath.Join(library, "demo") {
		t.Fatalf("global undo changed the home-rooted project link: %q, %v", got, err)
	}
	if _, ok, err := operation.ProjectJournal(home).Latest(); err != nil || !ok {
		t.Fatalf("project journal after global undo = (ok=%v, err=%v), want its entry retained", ok, err)
	}
}

func TestRecommendationIdentifierInstallsOnlyAfterExplicitYes(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "go-helper", "Go helper", "helps with go", "go\n")
	writeSkill(t, library, "other-helper", "Other helper", "unrelated guide", "catalog\n")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module install-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	recommend := cli.NewRootCommand()
	recommendation := &bytes.Buffer{}
	recommend.SetOut(recommendation)
	recommend.SetErr(recommendation)
	recommend.SetArgs([]string{"--config", configPath, "recommend", "--project", project, "--json"})
	if err := recommend.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Scopes []struct {
			Recommendations []struct {
				Identifier string `json:"identifier"`
			} `json:"recommendations"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(recommendation.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 1 || len(result.Scopes[0].Recommendations) == 0 {
		t.Fatalf("recommendations = %s", recommendation)
	}
	selectedID := result.Scopes[0].Recommendations[0].Identifier
	if selectedID != "go-helper" {
		t.Fatalf("first recommendation = %q", selectedID)
	}

	install := func(yes bool) error {
		root := cli.NewRootCommand()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		args := []string{"--config", configPath, "install", selectedID, "--project", project, "--target", "codex"}
		if yes {
			args = append(args, "--yes")
		}
		root.SetArgs(args)
		return root.Execute()
	}
	if err := install(false); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("unconfirmed install error = %v", err)
	}
	selectedPath := filepath.Join(project, ".codex", "skills", selectedID)
	if _, err := os.Lstat(selectedPath); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed recommendation install changed project: %v", err)
	}
	if err := install(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(selectedPath); err != nil {
		t.Fatalf("selected recommendation was not installed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex", "skills", "other-helper")); !os.IsNotExist(err) {
		t.Fatalf("install added an unselected Skill: %v", err)
	}
}
