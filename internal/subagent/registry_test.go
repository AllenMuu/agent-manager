package subagent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

func TestDiscoverReadsCanonicalDefinitionAndPreservesDetails(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "reviewer.yaml", `version: v1
id: reviewer
name: Code Reviewer
role: Reviews changes
instructions: Review the diff and report risks.
skills:
  - go-helper
compatibility:
  agents:
    - codex
requiredCapabilities:
  - filesystem-read
`)

	registry, err := subagent.NewRegistry(root, func(id string) bool { return id == "go-helper" })
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(diagnostics) != 0 || len(definitions) != 1 {
		t.Fatalf("Discover() = (%v, %v), want one definition and no diagnostics", definitions, diagnostics)
	}
	got := definitions[0]
	if got.ID != "reviewer" || got.Name != "Code Reviewer" || got.Role != "Reviews changes" {
		t.Fatalf("definition identity = %#v", got)
	}
	if got.Instructions != "Review the diff and report risks." || len(got.Skills) != 1 || got.Skills[0] != "go-helper" {
		t.Fatalf("definition content = %#v", got)
	}
	if got.Compatibility.Agents[0] != "codex" || got.RequiredCapabilities[0] != resource.CapabilityFilesystemRead {
		t.Fatalf("definition declarations = %#v", got)
	}
}

func TestDiscoverReportsMalformedDefinitionWithActionableDiagnostic(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "broken.yaml", "version: v2\nid: ../unsafe\nname: Broken\n")

	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 {
		t.Fatalf("Discover() = (%v, %v), want one diagnostic", definitions, diagnostics)
	}
	message := diagnostics[0].Error()
	if !strings.Contains(message, "broken.yaml") || !strings.Contains(message, "version") {
		t.Errorf("diagnostic = %q, want file and version guidance", message)
	}
}

func TestDiscoverReportsMissingSkillReference(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
skills:
  - absent-skill
`)

	registry, err := subagent.NewRegistry(root, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 {
		t.Fatalf("Discover() = (%v, %v), want missing-reference diagnostic", definitions, diagnostics)
	}
	message := diagnostics[0].Error()
	if !strings.Contains(message, "absent-skill") || !strings.Contains(message, "skill") {
		t.Errorf("diagnostic = %q, want missing skill guidance", message)
	}
}

func TestDiscoverReportsTrailingYAMLDocument(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "trailing.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
---
unexpected: document
`)
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Error(), "multiple") {
		t.Fatalf("Discover() = (%v, %v), want multiple-document diagnostic", definitions, diagnostics)
	}
}

func TestDiscoverRejectsDuplicateIDsWithDiagnosticsForBothFiles(t *testing.T) {
	root := t.TempDir()
	definition := `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
`
	writeDefinition(t, root, "first.yaml", definition)
	writeDefinition(t, root, "second.yaml", definition)
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 2 {
		t.Fatalf("Discover() = (%v, %v), want both duplicates rejected", definitions, diagnostics)
	}
	wantPaths := []string{
		filepath.Join(root, subagent.DefinitionsDirectory, "first.yaml"),
		filepath.Join(root, subagent.DefinitionsDirectory, "second.yaml"),
	}
	for i, diagnostic := range diagnostics {
		if diagnostic.Path != wantPaths[i] {
			t.Errorf("diagnostic order = %v, want %v", diagnostics, wantPaths)
		}
		message := diagnostic.Error()
		if !strings.Contains(message, "duplicate id \"reviewer\"") || !strings.Contains(message, ".yaml") {
			t.Errorf("diagnostic = %q, want duplicate ID and counterpart path", message)
		}
	}
}

func TestDiscoverRejectsSymlinkedDefinitionsDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeDefinition(t, outside, "outside.yaml", `version: v1
id: outside
name: Outside
role: Review
instructions: Review changes.
`)
	if err := os.Symlink(filepath.Join(outside, subagent.DefinitionsDirectory), filepath.Join(root, subagent.DefinitionsDirectory)); err != nil {
		t.Fatal(err)
	}
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err == nil {
		t.Fatal("Discover() error = nil, want symlinked definitions directory rejection")
	}
	if len(definitions) != 0 || len(diagnostics) != 0 {
		t.Fatalf("Discover() = (%v, %v, %v), want no results", definitions, diagnostics, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Errorf("Discover() error = %q, want symlink guidance", err)
	}
}

func TestDiscoverReportsSymlinkedDefinitionWithoutReadingTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeDefinition(t, outside, "outside.yaml", `version: v1
id: outside
name: Outside
role: Review
instructions: Review changes.
`)
	link := filepath.Join(root, subagent.DefinitionsDirectory, "linked.yaml")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, subagent.DefinitionsDirectory, "outside.yaml"), link); err != nil {
		t.Fatal(err)
	}
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 {
		t.Fatalf("Discover() = (%v, %v), want one symlink diagnostic", definitions, diagnostics)
	}
	if !strings.Contains(strings.ToLower(diagnostics[0].Error()), "symlink") {
		t.Errorf("diagnostic = %q, want symlink guidance", diagnostics[0].Error())
	}
}

func TestDiscoverReportsDirectoryDefinitionAsNonRegular(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, subagent.DefinitionsDirectory, "nested.yaml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 || !strings.Contains(strings.ToLower(diagnostics[0].Error()), "regular") {
		t.Fatalf("Discover() = (%v, %v), want non-regular directory diagnostic", definitions, diagnostics)
	}
}

func TestDiscoverReportsDuplicateYAMLKeysAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "duplicate.yaml", `version: v1
id: duplicate
id: second
name: Duplicate
role: Review
instructions: Review changes.
`)
	writeDefinition(t, root, "unknown.yaml", `version: v1
id: unknown
name: Unknown
role: Review
instructions: Review changes.
unexpected: true
`)
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 2 {
		t.Fatalf("Discover() = (%v, %v), want duplicate-key and unknown-field diagnostics", definitions, diagnostics)
	}
	joined := diagnostics[0].Error() + "\n" + diagnostics[1].Error()
	if !strings.Contains(joined, "duplicate") || !strings.Contains(joined, "field") {
		t.Errorf("diagnostics = %q, want duplicate-key and unknown-field guidance", joined)
	}
}

func TestDiscoverReportsOversizedDefinitionWithoutUnboundedRead(t *testing.T) {
	root := t.TempDir()
	contents := "version: v1\nid: oversized\nname: Oversized\nrole: Review\ninstructions: " + strings.Repeat("x", (1<<20)+1) + "\n"
	writeDefinition(t, root, "oversized.yaml", contents)
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 || !strings.Contains(strings.ToLower(diagnostics[0].Error()), "maximum size") {
		t.Fatalf("Discover() = (%v, %v), want bounded-size diagnostic", definitions, diagnostics)
	}
}

func TestDiscoverReturnsDefinitionsInDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"zeta", "alpha", "middle"} {
		writeDefinition(t, root, id+".yaml", "version: v1\nid: "+id+"\nname: "+id+"\nrole: Review\ninstructions: Review changes.\n")
	}
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("Discover() = (%v, %v, %v)", definitions, diagnostics, err)
	}
	want := []string{"alpha", "middle", "zeta"}
	for i, definition := range definitions {
		if definition.ID != want[i] {
			t.Fatalf("definition order = %v, want %v", definitions, want)
		}
	}
}

func TestNewRegistryRejectsRelativeDataRoot(t *testing.T) {
	if _, err := subagent.NewRegistry("relative", nil); err == nil {
		t.Fatal("NewRegistry(relative) error = nil")
	}
}

func writeDefinition(t *testing.T, root, name, contents string) {
	t.Helper()
	dir := filepath.Join(root, subagent.DefinitionsDirectory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
