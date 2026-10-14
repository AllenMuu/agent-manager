package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/skillimport"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestSkillImportCLIRequiresExplicitSelectionAndConfirmation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacy := filepath.Join(home, ".agents", "skills")
	writeSkill(t, legacy, "demo", "Legacy helper", "import marker", "legacy\n")
	source := filepath.Join(legacy, "demo")
	projectLink := filepath.Join(t.TempDir(), "legacy-demo")
	if err := os.Symlink(source, projectLink); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(home, ".agent-manager", "skills")

	previewCommand := cli.NewRootCommand()
	previewOutput := &bytes.Buffer{}
	previewCommand.SetOut(previewOutput)
	previewCommand.SetErr(previewOutput)
	previewCommand.SetArgs([]string{"skill", "import", "demo"})
	if err := previewCommand.Execute(); !errors.Is(err, lifecycle.ErrNotConfirmed) {
		t.Fatalf("preview Execute() error = %v, want confirmation required", err)
	}
	for _, want := range []string{"demo", destination, "version: unknown", "Preview only"} {
		if !strings.Contains(previewOutput.String(), want) {
			t.Errorf("preview output %q omitted %q", previewOutput.String(), want)
		}
	}
	confirmationToken := confirmationTokenFromOutput(t, previewOutput.String())
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed import created destination: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: demo\ndescription: changed\n---\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleCommand := cli.NewRootCommand()
	staleOutput := &bytes.Buffer{}
	staleCommand.SetOut(staleOutput)
	staleCommand.SetErr(staleOutput)
	staleCommand.SetArgs([]string{"skill", "import", "demo", "--confirm-token", confirmationToken, "--yes"})
	if err := staleCommand.Execute(); !errors.Is(err, skillimport.ErrStalePreview) {
		t.Fatalf("stale confirmation error = %v, want stale-preview error", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("stale confirmation imported changed source: %v", err)
	}

	refreshCommand := cli.NewRootCommand()
	refreshOutput := &bytes.Buffer{}
	refreshCommand.SetOut(refreshOutput)
	refreshCommand.SetErr(refreshOutput)
	refreshCommand.SetArgs([]string{"skill", "import", "demo"})
	if err := refreshCommand.Execute(); !errors.Is(err, lifecycle.ErrNotConfirmed) {
		t.Fatalf("refreshed preview error = %v, want confirmation required", err)
	}
	confirmationToken = confirmationTokenFromOutput(t, refreshOutput.String())

	applyCommand := cli.NewRootCommand()
	applyOutput := &bytes.Buffer{}
	applyCommand.SetOut(applyOutput)
	applyCommand.SetErr(applyOutput)
	applyCommand.SetArgs([]string{"skill", "import", "demo", "--confirm-token", confirmationToken, "--yes"})
	if err := applyCommand.Execute(); err != nil {
		t.Fatalf("confirmed import error = %v", err)
	}
	if !strings.Contains(applyOutput.String(), "Imported 1 selected Skill") {
		t.Fatalf("import output = %q", applyOutput.String())
	}
	repository, err := skillrepo.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := repository.Resolve("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "unknown" || entry.Metadata.SourceType != "local-import" {
		t.Fatalf("imported entry = %#v", entry)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("legacy source was removed: %v", err)
	}
	if got, err := os.Readlink(projectLink); err != nil || got != source {
		t.Fatalf("legacy project link changed: %q, %v", got, err)
	}
}

func confirmationTokenFromOutput(t *testing.T, output string) string {
	t.Helper()
	previewParts := strings.SplitN(output, "Confirmation token: ", 2)
	if len(previewParts) != 2 {
		t.Fatalf("preview output %q omitted confirmation token", output)
	}
	tokens := strings.Fields(previewParts[1])
	if len(tokens) == 0 {
		t.Fatalf("preview output %q contained an empty confirmation token", output)
	}
	return tokens[0]
}

func TestSkillVersionsAndSearchExposeAddressableMetadataWithoutOldContent(t *testing.T) {
	library := t.TempDir()
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	oldSource := writeVersion(t, t.TempDir(), "demo", "1.0.0", "old-only phrase")
	old, err := repository.AddVersion("demo", oldSource, skillrepo.VersionMetadata{Version: "1.0.0", SourceType: "git", Publisher: "Example", ImmutableRef: "abc123"}, true)
	if err != nil {
		t.Fatal(err)
	}
	newSource := writeVersion(t, t.TempDir(), "demo", "2.0.0", "current phrase")
	current, err := repository.AddVersion("demo", newSource, skillrepo.VersionMetadata{Version: "2.0.0", SourceType: "git", Publisher: "Example", ImmutableRef: "def456"}, true)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "agent-manager.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	versionsCommand := cli.NewRootCommand()
	versionsOutput := &bytes.Buffer{}
	versionsCommand.SetOut(versionsOutput)
	versionsCommand.SetErr(versionsOutput)
	versionsCommand.SetArgs([]string{"--config", configPath, "skill", "versions", "demo", "--json"})
	if err := versionsCommand.Execute(); err != nil {
		t.Fatalf("versions Execute() error = %v", err)
	}
	var versionReport struct {
		Identifier        string `json:"identifier"`
		CurrentVersionKey string `json:"currentVersionKey"`
		Versions          []struct {
			Key          string `json:"key"`
			Version      string `json:"version"`
			Publisher    string `json:"publisher"`
			ImmutableRef string `json:"immutableRef"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(versionsOutput.Bytes(), &versionReport); err != nil {
		t.Fatalf("decode versions output: %v; output=%s", err, versionsOutput.String())
	}
	if versionReport.Identifier != "demo" || versionReport.CurrentVersionKey != current.Key || len(versionReport.Versions) != 2 {
		t.Fatalf("versions report = %#v", versionReport)
	}
	if versionReport.Versions[0].Key != old.Key || versionReport.Versions[0].Publisher != "Example" || versionReport.Versions[0].ImmutableRef != "abc123" {
		t.Fatalf("old version provenance missing from report: %#v", versionReport.Versions[0])
	}

	searchCommand := cli.NewRootCommand()
	searchOutput := &bytes.Buffer{}
	searchCommand.SetOut(searchOutput)
	searchCommand.SetErr(searchOutput)
	searchCommand.SetArgs([]string{"search", "current phrase", "--library", library, "--json"})
	if err := searchCommand.Execute(); err != nil {
		t.Fatalf("search Execute() error = %v", err)
	}
	var catalogReport []struct {
		Identifier string `json:"identifier"`
		Version    string `json:"version"`
		VersionKey string `json:"versionKey"`
		Versions   []struct {
			Key     string `json:"key"`
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(searchOutput.Bytes(), &catalogReport); err != nil {
		t.Fatalf("decode search output: %v; output=%s", err, searchOutput.String())
	}
	if len(catalogReport) != 1 || catalogReport[0].Version != "2.0.0" || catalogReport[0].VersionKey != current.Key || len(catalogReport[0].Versions) != 2 {
		t.Fatalf("search catalog = %#v", catalogReport)
	}
	oldContentSearch := cli.NewRootCommand()
	oldContentOutput := &bytes.Buffer{}
	oldContentSearch.SetOut(oldContentOutput)
	oldContentSearch.SetErr(&bytes.Buffer{})
	oldContentSearch.SetArgs([]string{"search", "old-only phrase", "--library", library, "--json"})
	if err := oldContentSearch.Execute(); err != nil {
		t.Fatalf("old content search error = %v", err)
	}
	if got := strings.TrimSpace(oldContentOutput.String()); got != "[]" {
		t.Fatalf("search exposed unselected version content: %s", got)
	}
	if _, err := os.Stat(filepath.Join(old.Path, "SKILL.md")); err != nil {
		t.Fatalf("addressable old version was not retained: %v", err)
	}
}

func TestAddCLIActivatesRequestedRepositoryVersion(t *testing.T) {
	library := t.TempDir()
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	oldSource := writeVersion(t, t.TempDir(), "demo", "1.0.0", "historical")
	old, err := repository.AddVersion("demo", oldSource, skillrepo.VersionMetadata{Version: "1.0.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	newSource := writeVersion(t, t.TempDir(), "demo", "2.0.0", "current")
	if _, err := repository.AddVersion("demo", newSource, skillrepo.VersionMetadata{Version: "2.0.0"}, true); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "agent-manager.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--config", configPath, "add", "demo", "--project", project, "--target", "codex", "--version", "demo=1.0.0", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatalf("add selected version error = %v; output=%s", err, output.String())
	}
	link := filepath.Join(project, ".codex", "skills", "demo")
	if got, err := os.Readlink(link); err != nil || got != old.Path {
		t.Fatalf("activation link = %q, %v, want %q", got, err, old.Path)
	}
	remove := cli.NewRootCommand()
	remove.SetOut(&bytes.Buffer{})
	remove.SetErr(&bytes.Buffer{})
	remove.SetArgs([]string{"--config", configPath, "remove", "demo", "--project", project, "--target", "codex", "--yes"})
	if err := remove.Execute(); err != nil {
		t.Fatalf("remove selected historical version error = %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("managed activation link was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old.Path, "SKILL.md")); err != nil {
		t.Fatalf("removal deleted selected repository version: %v", err)
	}
}

func writeVersion(t *testing.T, parent, identifier, version, body string) string {
	t.Helper()
	path := filepath.Join(parent, identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + identifier + "\ndescription: current phrase\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "version.txt"), []byte(version), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
