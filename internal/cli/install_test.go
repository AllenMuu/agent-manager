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
