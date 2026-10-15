package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestUndoLinkDependenciesUseBatchBeforeStateAndRejectChangedExternalNodes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "external", "source")
	managed := filepath.Join(root, "library", "managed")
	writeReferenceSkill(t, source)
	writeReferenceSkill(t, managed)
	checksum, err := skillrepo.ChecksumTree(source)
	if err != nil {
		t.Fatal(err)
	}

	claude := filepath.Join(root, "project", ".claude", "skills", "foo")
	codex := filepath.Join(root, "project", ".codex", "skills", "foo")
	for _, path := range []string{claude, codex} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(managed, path); err != nil {
			t.Skipf("create simulated migrated entry: %v", err)
		}
	}
	claudeTarget, err := filepath.Rel(filepath.Dir(claude), codex)
	if err != nil {
		t.Fatal(err)
	}
	codexTarget, err := filepath.Rel(filepath.Dir(codex), source)
	if err != nil {
		t.Fatal(err)
	}
	claudeBackup := filepath.Join(root, "backups", "claude")
	codexBackup := filepath.Join(root, "backups", "codex")
	for _, backup := range []struct{ path, target string }{{claudeBackup, claudeTarget}, {codexBackup, codexTarget}} {
		if err := os.MkdirAll(filepath.Dir(backup.path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(backup.target, backup.path); err != nil {
			t.Skipf("create before-state link backup: %v", err)
		}
	}
	before := []operation.Snapshot{
		{Path: claude, Exists: true, Backup: claudeBackup},
		{Path: codex, Exists: true, Backup: codexBackup},
	}
	dependencies := []operation.LinkDependency{
		{EntryPath: claude, LinkChain: []operation.LinkDependencyHop{{Path: claude, Target: claudeTarget}, {Path: codex, Target: codexTarget}}, RealPath: source, SourceChecksum: checksum},
		{EntryPath: codex, LinkChain: []operation.LinkDependencyHop{{Path: codex, Target: codexTarget}}, RealPath: source, SourceChecksum: checksum},
	}
	if err := ValidateLinkDependenciesWithBefore(dependencies, before); err != nil {
		t.Fatalf("same-batch before-state chain was rejected: %v", err)
	}

	otherSource := filepath.Join(root, "external", "other-source")
	writeReferenceSkill(t, otherSource)
	otherTarget, err := filepath.Rel(filepath.Dir(codex), otherSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(codex); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherSource, codex); err != nil {
		t.Skipf("replace external dependency: %v", err)
	}
	if err := ValidateLinkDependenciesWithBefore(dependencies[:1], before[:1]); err == nil {
		t.Fatal("changed batch-external dependency was accepted")
	}
	if target, err := os.Readlink(codex); err != nil || target != otherSource {
		t.Fatalf("external owner changed during validation: target=%q err=%v (relative before target was %q)", target, err, otherTarget)
	}
}

func writeReferenceSkill(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: reference\ndescription: reference\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
