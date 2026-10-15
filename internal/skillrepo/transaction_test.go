package skillrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicImportRejectsSymlinkedBackupAncestor(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := Open(filepath.Join(root, "library"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	writeTransactionSkill(t, source, "new content")
	moved := filepath.Join(root, "backup-target")
	if err := os.Mkdir(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "backup-link")
	if err := os.Symlink(moved, link); err != nil {
		t.Skipf("create test symlink: %v", err)
	}

	_, err = repository.AddVersionsAtomic([]VersionSource{{
		Identifier: "demo", SourcePath: source,
		Metadata: VersionMetadata{Key: "v1", Version: "1"}, MakeCurrent: true,
	}}, filepath.Join(link, "transaction", "before"), nil, nil)
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("import with symlinked backup ancestor = %v, want ErrUnsafe", err)
	}
	if _, err := repository.Resolve("demo", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rejected import changed the repository: %v", err)
	}
}

func writeTransactionSkill(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: Demo\ndescription: Transaction test.\n---\n\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
