package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
)

func TestSearchCommandRendersDeterministicJSON(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "zebra", "Zebra helper", "Go tooling", "go\n")
	writeSkill(t, library, "alpha", "Alpha helper", "Go tooling", "catalog\n")

	root := cli.NewRootCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"search", "tooling", "--library", library, "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "[{\"identifier\":\"alpha\",\"description\":\"Go tooling\",\"tags\":[\"catalog\"],\"compatibility\":[\"codex\"],\"version\":\"unknown\",\"versionKey\":\"legacy\",\"versions\":[{\"version\":\"unknown\",\"key\":\"legacy\",\"trustLevel\":\"unknown\",\"verificationStatus\":\"unknown\",\"tags\":[\"catalog\"],\"compatibility\":[\"codex\"]}],\"trustLevel\":\"unknown\",\"verificationStatus\":\"unknown\"},{\"identifier\":\"zebra\",\"description\":\"Go tooling\",\"tags\":[\"go\"],\"compatibility\":[\"codex\"],\"version\":\"unknown\",\"versionKey\":\"legacy\",\"versions\":[{\"version\":\"unknown\",\"key\":\"legacy\",\"trustLevel\":\"unknown\",\"verificationStatus\":\"unknown\",\"tags\":[\"go\"],\"compatibility\":[\"codex\"]}],\"trustLevel\":\"unknown\",\"verificationStatus\":\"unknown\"}]\n"
	if got := output.String(); got != want {
		t.Errorf("JSON output = %q, want %q", got, want)
	}
}

func TestSearchCommandUsesConfiguredLibrary(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "configured", "Configured helper", "searchable", "tag\n")
	configPath := filepath.Join(t.TempDir(), "skill-manager.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := cli.NewRootCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"--config", configPath, "search", "searchable"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := output.String(), "configured\tsearchable\tversion: unknown (key: legacy)\ttags: tag\tcompatibility: codex\tsource: unknown\ttype: unknown\tpublisher: unknown\tref: unknown\tchecksum: unknown\ttrust: unknown\tverification: unknown\n"; got != want {
		t.Errorf("human output = %q, want %q", got, want)
	}
}

func TestSearchCommandDoesNotMergeLegacyLibraryIntoDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSkill(t, filepath.Join(home, ".agent-manager", "skills"), "canonical", "Canonical helper", "default marker", "new\n")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "legacy", "Legacy helper", "default marker", "old\n")

	root := cli.NewRootCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"search", "default marker"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := output.String(), "canonical\tdefault marker\tversion: unknown (key: legacy)\ttags: new\tcompatibility: codex\tsource: unknown\ttype: unknown\tpublisher: unknown\tref: unknown\tchecksum: unknown\ttrust: unknown\tverification: unknown\n"; got != want {
		t.Fatalf("human output = %q, want only canonical library result %q", got, want)
	}
}

func TestSearchCommandPropagatesHumanOutputWriterFailure(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "catalog", "Catalog helper", "searchable", "tag\n")

	root := cli.NewRootCommand()
	root.SetOut(failingWriter{err: errors.New("write failure")})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"search", "searchable", "--library", library})

	err := root.Execute()
	if err == nil || err.Error() != "write failure" {
		t.Fatalf("Execute() error = %v, want write failure", err)
	}
}

func writeSkill(t *testing.T, library, identifier, name, description, tag string) {
	t.Helper()
	directory := filepath.Join(library, identifier)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	markdown := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := "tags:\n  - " + tag + "compatibility:\n  - codex\n"
	if err := os.WriteFile(filepath.Join(directory, ".skill-manager.yaml"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}
