package skillimport_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/skillimport"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestPreviewIsReadOnlyAndImportCopiesOnlySelectedSkills(t *testing.T) {
	legacy := t.TempDir()
	sourceA := writeSkill(t, legacy, "alpha", "Alpha")
	sourceB := writeSkill(t, legacy, "beta", "Beta")
	projectLink := filepath.Join(t.TempDir(), "legacy-link")
	if err := os.Symlink(sourceA, projectLink); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Join(t.TempDir(), "canonical")
	service, err := skillimport.New(repositoryRoot, legacy)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := service.Preview(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 2 || preview.Entries[0].Identifier != "alpha" || preview.Entries[1].Identifier != "beta" {
		t.Fatalf("Preview() entries = %#v, want both identifiers in stable order", preview.Entries)
	}
	if len(preview.Entries[0].MetadataGaps) == 0 || preview.Entries[0].VersionKey == "" || preview.Entries[0].Conflict {
		t.Fatalf("Preview() omitted version, metadata gaps, or conflict state: %#v", preview.Entries[0])
	}
	if _, err := os.Lstat(repositoryRoot); !os.IsNotExist(err) {
		t.Fatalf("preview created destination root: stat error = %v", err)
	}
	if got, err := os.Readlink(projectLink); err != nil || got != sourceA {
		t.Fatalf("preview changed existing project link: %q, %v", got, err)
	}
	if _, err := service.Import(preview); !errors.Is(err, skillimport.ErrInvalidSelection) {
		t.Fatalf("Import(unselected all-preview) error = %v, want explicit selection error", err)
	}

	selectedPreview, err := service.Preview([]string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(selectedPreview)
	if err != nil {
		t.Fatalf("Import(alpha) error = %v", err)
	}
	if len(imported) != 1 || imported[0].Identifier != "alpha" || imported[0].Version != "unknown" {
		t.Fatalf("Import(alpha) = %#v, want one legacy version marked unknown", imported)
	}
	metadata := imported[0].Metadata
	if metadata.Source != sourceA || metadata.SourceType != "local-import" || metadata.TrustLevel != "unknown" || metadata.VerificationStatus != "unknown" {
		t.Fatalf("import provenance = %#v", metadata)
	}
	if _, err := os.Stat(filepath.Join(imported[0].Path, "SKILL.md")); err != nil {
		t.Fatalf("imported content missing: %v", err)
	}
	if _, err := os.Stat(sourceA); err != nil {
		t.Fatalf("legacy source was removed: %v", err)
	}
	if _, err := os.Stat(sourceB); err != nil {
		t.Fatalf("unselected legacy source was removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repositoryRoot, "beta")); !os.IsNotExist(err) {
		t.Fatalf("unselected Skill was imported: stat error = %v", err)
	}
	if got, err := os.Readlink(projectLink); err != nil || got != sourceA {
		t.Fatalf("import rewrote existing project link: %q, %v", got, err)
	}
	resolved, err := skillrepo.Open(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if entry, err := resolved.Resolve("alpha", imported[0].Key); err != nil || entry.Path != imported[0].Path {
		t.Fatalf("resolve imported version = %#v, %v", entry, err)
	}
}

func TestImportRefusesIdentifierConflictWithoutChangingEitherSide(t *testing.T) {
	legacy := t.TempDir()
	source := writeSkill(t, legacy, "duplicate", "Legacy")
	repositoryRoot := t.TempDir()
	destination := filepath.Join(repositoryRoot, "duplicate")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(destination, "keep.txt")
	if err := os.WriteFile(marker, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := skillimport.New(repositoryRoot, legacy)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{"duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 1 || !preview.Entries[0].Conflict {
		t.Fatalf("Preview() = %#v, want destination conflict", preview)
	}
	if _, err := service.Import(preview); !errors.Is(err, skillimport.ErrConflict) {
		t.Fatalf("Import(conflict) error = %v, want conflict", err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "existing" {
		t.Fatalf("conflicting destination changed: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatalf("legacy source changed: %v", err)
	}
}

func TestImportRejectsStalePreviewAndUnsafeTree(t *testing.T) {
	legacy := t.TempDir()
	source := writeSkill(t, legacy, "demo", "before")
	service, err := skillimport.New(filepath.Join(t.TempDir(), "repository"), legacy)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "notes.md"), []byte("changed after preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Import(preview); !errors.Is(err, skillimport.ErrStalePreview) {
		t.Fatalf("Import(stale preview) error = %v, want stale-preview error", err)
	}

	unsafeRoot := t.TempDir()
	unsafeSkill := writeSkill(t, unsafeRoot, "unsafe", "symlink")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(unsafeSkill, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	unsafeService, err := skillimport.New(filepath.Join(t.TempDir(), "repository"), unsafeRoot)
	if err != nil {
		t.Fatal(err)
	}
	unsafePreview, err := unsafeService.Preview([]string{"unsafe"})
	if err != nil {
		t.Fatal(err)
	}
	if unsafePreview.Entries[0].Eligible {
		t.Fatalf("unsafe Skill was marked eligible: %#v", unsafePreview.Entries[0])
	}
	if _, err := unsafeService.Import(unsafePreview); !errors.Is(err, skillimport.ErrInvalidSelection) {
		t.Fatalf("Import(unsafe tree) error = %v, want ineligible error", err)
	}
	if got := unsafePreview.Entries[0].MetadataGaps; !reflect.DeepEqual(got, []string{"declared version", "publisher", "immutable revision", "trust verification", "legacy companion metadata"}) {
		t.Fatalf("metadata gaps = %v", got)
	}
}

func TestNewRejectsLegacyAndRepositoryPathsThatAliasThroughSymlinks(t *testing.T) {
	actual := t.TempDir()
	alias := filepath.Join(t.TempDir(), "library-link")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := skillimport.New(alias, actual); err == nil {
		t.Fatal("New() accepted legacy and repository roots that resolve to the same directory")
	}
}

func writeSkill(t *testing.T, parent, identifier, description string) string {
	t.Helper()
	path := filepath.Join(parent, identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + identifier + "\ndescription: " + description + "\n---\n" + description + "\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
