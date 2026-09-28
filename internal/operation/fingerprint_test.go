package operation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintPathTracksTreeContentsWithoutFollowingLinks(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(project, "owner.txt")
	if err := os.WriteFile(content, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := FingerprintPath(project)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FingerprintPath(project)
	if err != nil || second != first {
		t.Fatalf("unchanged fingerprint = %q, %v; want %q", second, err, first)
	}
	if err := os.WriteFile(content, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := FingerprintPath(project)
	if err != nil || third == first {
		t.Fatalf("changed content fingerprint = %q, %v; want a different digest", third, err)
	}
	if err := os.Remove(content); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(project, "owner-link")); err != nil {
		t.Fatal(err)
	}
	fourth, err := FingerprintPath(project)
	if err != nil || fourth == third {
		t.Fatalf("changed symlink fingerprint = %q, %v; want a different digest", fourth, err)
	}
}
