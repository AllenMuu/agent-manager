package skillrepo_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestVersionedRepositoryRetainsAndResolvesVersions(t *testing.T) {
	root := t.TempDir()
	repo, err := skillrepo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	firstSource := writeSkill(t, t.TempDir(), "First version", "tags:\n  - go\ncompatibility:\n  - codex\nsupportedLanguages:\n  - go\nsupportedFrameworks:\n  - cobra\nsupportedAgents:\n  - codex\nprovenance: imported\n")
	first, err := repo.AddVersion("demo", firstSource, skillrepo.VersionMetadata{
		Version:      "1.0.0",
		Source:       "https://example.test/demo",
		SourceType:   "git",
		Publisher:    "Example",
		ImmutableRef: "abc123",
		TrustLevel:   "verified",
	}, true)
	if err != nil {
		t.Fatalf("AddVersion(1.0.0): %v", err)
	}

	secondSource := writeSkill(t, t.TempDir(), "Second version", "")
	second, err := repo.AddVersion("demo", secondSource, skillrepo.VersionMetadata{Version: "2.0.0"}, true)
	if err != nil {
		t.Fatalf("AddVersion(2.0.0): %v", err)
	}
	if first.Path == second.Path {
		t.Fatalf("version paths are identical: %q", first.Path)
	}
	if got, want := filepath.Base(filepath.Dir(filepath.Dir(second.Path))), "demo"; got != want {
		t.Fatalf("second path=%q, want under skill directory %q", second.Path, want)
	}

	current, err := repo.Resolve("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "2.0.0" || current.Path != second.Path {
		t.Fatalf("current = %#v, want second version %q at %q", current, "2.0.0", second.Path)
	}
	resolvedFirst, err := repo.Resolve("demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if resolvedFirst.Path != first.Path {
		t.Fatalf("Resolve(1.0.0) path=%q, want %q", resolvedFirst.Path, first.Path)
	}
	versions, err := repo.Versions("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("Versions() returned %d entries, want 2: %#v", len(versions), versions)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(root, "demo", "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata skillrepo.Metadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SchemaVersion != skillrepo.MetadataSchemaVersion || metadata.Identifier != "demo" {
		t.Fatalf("metadata identity = %#v", metadata)
	}
	if metadata.CurrentVersion != second.Key {
		t.Fatalf("current version key=%q, want %q", metadata.CurrentVersion, second.Key)
	}
	if got, want := metadata.Versions[0].Compatibility, []string{"codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("imported compatibility=%v, want %v", got, want)
	}
	if got, want := metadata.Versions[0].Tags, []string{"go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("imported tags=%v, want %v", got, want)
	}
	if metadata.Versions[0].LegacyProvenance != "imported" {
		t.Fatalf("legacy companion provenance=%q, want imported", metadata.Versions[0].LegacyProvenance)
	}
	if !reflect.DeepEqual(metadata.Versions[0].SupportedLanguages, []string{"go"}) || !reflect.DeepEqual(metadata.Versions[0].SupportedFrameworks, []string{"cobra"}) || !reflect.DeepEqual(metadata.Versions[0].SupportedAgents, []string{"codex"}) {
		t.Fatalf("supported metadata was not converted: %#v", metadata.Versions[0])
	}
	if metadata.Versions[0].Checksum == "" {
		t.Fatal("version checksum is empty")
	}
	if _, err := os.Stat(filepath.Join(first.Path, "SKILL.md")); err != nil {
		t.Fatalf("first version content was not retained: %v", err)
	}
}

func TestResolveRejectsStoredVersionContentThatNoLongerMatchesChecksum(t *testing.T) {
	repository, err := skillrepo.Open(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	source := writeSkill(t, t.TempDir(), "original", "")
	entry, err := repository.AddVersion("demo", source, skillrepo.VersionMetadata{Version: "1.0.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Path, "SKILL.md"), []byte("---\nname: Demo\ndescription: changed\n---\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve("demo", entry.Key); !errors.Is(err, skillrepo.ErrUnsafe) {
		t.Fatalf("Resolve(modified version) error = %v, want checksum mismatch", err)
	}
}

func TestDiscoverFlatLegacyLibraryWithoutInventingVersion(t *testing.T) {
	root := t.TempDir()
	legacyPath := writeSkill(t, root, "Legacy", "tags:\n  - local\n")
	repo, err := skillrepo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, diagnostics, err := repo.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%#v, want none", diagnostics)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%#v, want one legacy skill", entries)
	}
	if entries[0].Identifier != "demo" || entries[0].Path != legacyPath {
		t.Fatalf("entry identity/path=%#v", entries[0])
	}
	if entries[0].Version != "unknown" || entries[0].Key != "legacy" {
		t.Fatalf("legacy version=%#v, want explicit unknown version and legacy key", entries[0])
	}
}

func TestOwnsPathRecognizesMissingLegacyDirectoryForDanglingLinks(t *testing.T) {
	root := t.TempDir()
	repo, err := skillrepo.Open(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(repo.Root(), "old-skill")
	owned, err := repo.OwnsPath("old-skill", missing)
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("OwnsPath returned false for a missing canonical legacy path")
	}
}

func TestResolveMissingLegacySkillPreservesNotExistCause(t *testing.T) {
	root := t.TempDir()
	repo, err := skillrepo.Open(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Resolve("missing", ""); !errors.Is(err, skillrepo.ErrNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve missing entry = %v, want ErrNotFound and os.ErrNotExist", err)
	}
}

func TestResolveRejectsUnsafeIdentifiersAndVersionKeys(t *testing.T) {
	repo, err := skillrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		identifier string
		version    string
	}{
		{identifier: "../outside"},
		{identifier: "demo", version: "../../outside"},
	} {
		if _, err := repo.Resolve(tc.identifier, tc.version); err == nil {
			t.Errorf("Resolve(%q, %q) succeeded; want path validation error", tc.identifier, tc.version)
		}
	}
}

func TestDiscoverRejectsMetadataVersionPathTraversal(t *testing.T) {
	root := t.TempDir()
	versionedRoot := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(versionedRoot, "versions", "v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionedRoot, "metadata.json"), []byte(`{"schemaVersion":1,"id":"demo","currentVersion":"../outside","versions":[{"key":"../outside","version":"1.0.0"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := skillrepo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, diagnostics, err := repo.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(diagnostics) != 1 {
		t.Fatalf("entries=%#v diagnostics=%#v; want invalid repository entry diagnosed", entries, diagnostics)
	}
}

func TestRepositoryRejectsSymlinkRootsAndReviewedChecksumMismatch(t *testing.T) {
	actualRoot := t.TempDir()
	skillPath := writeSkill(t, actualRoot, "Safe Skill", "")
	linkedRoot := filepath.Join(t.TempDir(), "repository-link")
	if err := os.Symlink(actualRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	repository, err := skillrepo.Open(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve("safe", ""); !errors.Is(err, skillrepo.ErrUnsafe) {
		t.Fatalf("Resolve through symlink root error = %v, want unsafe path", err)
	}
	if _, _, err := repository.Discover(); !errors.Is(err, skillrepo.ErrUnsafe) {
		t.Fatalf("Discover through symlink root error = %v, want unsafe path", err)
	}

	cleanRepository, err := skillrepo.Open(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = cleanRepository.AddVersion("safe", skillPath, skillrepo.VersionMetadata{Checksum: "reviewed-old-checksum"}, true)
	if !errors.Is(err, skillrepo.ErrUnsafe) {
		t.Fatalf("AddVersion(checksum mismatch) error = %v, want source-changed rejection", err)
	}
	if _, err := os.Lstat(cleanRepository.Root()); !os.IsNotExist(err) {
		t.Fatalf("checksum mismatch created repository data: %v", err)
	}
}

func writeSkill(t *testing.T, parent, title, companion string) string {
	t.Helper()
	directory := filepath.Join(parent, "demo")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: Demo\ndescription: " + title + "\n---\n" + title + "\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if companion != "" {
		if err := os.WriteFile(filepath.Join(directory, ".skill-manager.yaml"), []byte(companion), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}
