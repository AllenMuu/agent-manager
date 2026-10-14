package skillrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAddVersionsBatchRollsBackEarlierPublishedIdentities(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	first := writeBatchSkill(t, t.TempDir(), "first")
	restricted := filepath.Join(first, "read-only")
	if err := os.MkdirAll(restricted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restricted, "nested.txt"), []byte("read only"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(restricted, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(restricted, 0o755) }()
	second := writeBatchSkill(t, t.TempDir(), "second")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("simulated later batch failure")
	_, err = repository.addVersionsBatch([]VersionSource{
		{Identifier: "first", SourcePath: first, MakeCurrent: true},
		{Identifier: "second", SourcePath: second, MakeCurrent: true},
	}, func(index int, _ Entry) error {
		if index == 0 {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) {
		t.Fatalf("addVersionsBatch() error = %v, want injected failure", err)
	}
	for _, identifier := range []string{"first", "second"} {
		if _, err := os.Lstat(filepath.Join(root, identifier)); !os.IsNotExist(err) {
			t.Errorf("batch left destination %q after rollback: %v", identifier, err)
		}
	}
	if _, err := os.Stat(filepath.Join(first, "SKILL.md")); err != nil {
		t.Fatalf("batch changed first source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "SKILL.md")); err != nil {
		t.Fatalf("batch changed second source: %v", err)
	}
}

func TestAddVersionsBatchPreservesReplacementDuringRollback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	first := writeBatchSkill(t, t.TempDir(), "first")
	second := writeBatchSkill(t, t.TempDir(), "second")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("simulated later batch failure")
	_, err = repository.addVersionsBatch([]VersionSource{
		{Identifier: "first", SourcePath: first, MakeCurrent: true},
		{Identifier: "second", SourcePath: second, MakeCurrent: true},
	}, func(index int, _ Entry) error {
		if index != 0 {
			return nil
		}
		identity := filepath.Join(root, "first")
		if err := os.Rename(identity, identity+".saved"); err != nil {
			return err
		}
		if err := os.Mkdir(identity, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(identity, "new-owner.txt"), []byte("keep me"), 0o644); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("addVersionsBatch() error = %v, want injected failure", err)
	}
	marker := filepath.Join(root, "first", "new-owner.txt")
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep me" {
		t.Fatalf("rollback changed replacement owner marker: contents=%q err=%v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(root, "first.saved", "metadata.json")); err != nil {
		t.Fatalf("original batch identity was not left at its moved location: %v", err)
	}
}

func writeBatchSkill(t *testing.T, parent, identifier string) string {
	t.Helper()
	path := filepath.Join(parent, identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: "+identifier+"\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
