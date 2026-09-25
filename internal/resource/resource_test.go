package resource_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/resource"
)

func TestValidateAcceptsVersionedResourceContract(t *testing.T) {
	r := resource.ManagedResource{
		Version: "v1",
		ID:      "java-reviewer",
		Kind:    resource.SubAgent,
		Provenance: resource.Provenance{
			Source: "local",
		},
		Compatibility: resource.Compatibility{Agents: []string{"codex"}},
		RequiredCapabilities: []resource.Capability{
			resource.CapabilityFilesystemWrite,
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidResourceContract(t *testing.T) {
	tests := []resource.ManagedResource{
		{Version: "v1", ID: "../unsafe", Kind: resource.Skill},
		{Version: "v2", ID: "valid", Kind: resource.Skill},
		{Version: "v1", ID: "valid", Kind: "unknown"},
		{Version: "v1", ID: "valid", Kind: resource.Memory, RequiredCapabilities: []resource.Capability{"unknown"}},
	}
	for _, r := range tests {
		t.Run(r.ID+string(r.Kind), func(t *testing.T) {
			if err := r.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			} else if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("Validate() returned an empty error")
			}
		})
	}
}

func TestSkillHandlerAcceptsOnlySkillResources(t *testing.T) {
	handler := resource.NewSkillHandler()
	var _ resource.ResourceHandler = handler
	if handler.Kind() != resource.Skill {
		t.Fatalf("Kind() = %q, want skill", handler.Kind())
	}
	if err := handler.Validate(resource.ManagedResource{Version: "v1", ID: "demo", Kind: resource.Skill}); err != nil {
		t.Fatalf("Validate(Skill) error = %v", err)
	}
	if err := handler.Validate(resource.ManagedResource{Version: "v1", ID: "reviewer", Kind: resource.SubAgent}); err == nil {
		t.Fatal("Validate(SubAgent) error = nil")
	}
}

func TestSkillHandlerOwnsCatalogDiscoveryAndManagedResourceConversion(t *testing.T) {
	library := t.TempDir()
	skillPath := filepath.Join(library, "catalog-tool")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("---\nname: Catalog Tool\ndescription: Catalog work\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, ".skill-manager.yaml"), []byte("tags: [go, catalog]\ncompatibility: [codex]\nprovenance: adopted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := resource.NewSkillHandler()
	skills, diagnostics, err := handler.Discover(library)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(diagnostics) != 0 || len(skills) != 1 {
		t.Fatalf("Discover() = (%#v, %#v), want one Skill and no diagnostics", skills, diagnostics)
	}
	if skills[0].Identifier != "catalog-tool" || skills[0].SourcePath != skillPath || skills[0].Body != "Body\n" {
		t.Fatalf("Discover() Skill = %#v", skills[0])
	}

	managed, err := handler.ManagedResource(skills[0])
	if err != nil {
		t.Fatalf("ManagedResource() error = %v", err)
	}
	if managed.Kind != resource.Skill || managed.ID != "catalog-tool" || managed.Provenance.Source != skillPath {
		t.Fatalf("ManagedResource() = %#v", managed)
	}
	if len(managed.Compatibility.Agents) != 1 || managed.Compatibility.Agents[0] != "codex" {
		t.Fatalf("ManagedResource() compatibility = %#v", managed.Compatibility)
	}
	libraryManaged, err := handler.LibraryResource(library, "catalog-tool")
	if err != nil {
		t.Fatalf("LibraryResource() error = %v", err)
	}
	if libraryManaged.ID != managed.ID || libraryManaged.Provenance.Source != managed.Provenance.Source {
		t.Fatalf("LibraryResource() = %#v, want catalog identity/source from %#v", libraryManaged, managed)
	}
	owned, err := handler.ManagesSource(library, "catalog-tool", skillPath)
	if err != nil || !owned {
		t.Fatalf("ManagesSource(catalog path) = %v, %v, want true", owned, err)
	}
	owned, err = handler.ManagesSource(library, "catalog-tool", filepath.Join(library, "other"))
	if err != nil || owned {
		t.Fatalf("ManagesSource(other path) = %v, %v, want false", owned, err)
	}

	resources, genericDiagnostics, err := handler.Catalog(library)
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if len(genericDiagnostics) != 0 || len(resources) != 1 || !reflect.DeepEqual(resources[0], managed) {
		t.Fatalf("Catalog() = (%#v, %#v), want transformed managed Skill", resources, genericDiagnostics)
	}
	inspected, err := handler.Inspect(skillPath)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !reflect.DeepEqual(inspected, managed) {
		t.Fatalf("Inspect() = %#v, want %#v", inspected, managed)
	}
}

func TestSkillHandlerInspectRejectsNonDirectoryAndSymlinkCatalogEntries(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	if err := os.MkdirAll(valid, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(valid, "SKILL.md"), []byte("---\nname: Valid\ndescription: Valid skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(valid, link); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "plain")
	if err := os.WriteFile(plain, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := resource.NewSkillHandler()
	for _, path := range []string{link, plain} {
		if _, err := handler.Inspect(path); err == nil {
			t.Fatalf("Inspect(%q) error = nil, want ineligible Skill", path)
		}
	}
}

func TestSkillHandlerPlansActivationAtAdapterPlacement(t *testing.T) {
	library := t.TempDir()
	skillPath := filepath.Join(library, "demo")
	writeSkill(t, skillPath)
	placement := filepath.Join(t.TempDir(), "runtime-owned", "demo")
	handler := resource.NewSkillHandler()
	managed, err := handler.ManagedResource(resource.SkillCatalogEntry{
		Identifier:    "demo",
		SourcePath:    skillPath,
		Compatibility: []string{"codex"},
	})
	if err != nil {
		t.Fatal(err)
	}

	planned, err := handler.PlanLifecycle(resource.SkillLifecycleRequest{
		Action:        resource.LifecycleActivate,
		LibraryPath:   library,
		Resource:      managed,
		PlacementPath: placement,
		Target:        "claude-code",
	})
	if err != nil {
		t.Fatalf("PlanLifecycle() error = %v", err)
	}
	plan, ok := planned.(resource.SkillLifecyclePlan)
	if !ok {
		t.Fatalf("PlanLifecycle() type = %T, want SkillLifecyclePlan", planned)
	}
	if plan.Resource.ID != "demo" || plan.SourcePath != skillPath {
		t.Fatalf("PlanLifecycle() = %#v", plan)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != placement || plan.Changes[0].Action != "create absolute link" || plan.Changes[0].Detail != skillPath {
		t.Fatalf("PlanLifecycle() changes = %#v", plan.Changes)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "claude-code") {
		t.Fatalf("PlanLifecycle() warnings = %#v", plan.Warnings)
	}
}

func TestSkillHandlerPlansManagedRemovalAndFork(t *testing.T) {
	library := t.TempDir()
	source := filepath.Join(library, "demo")
	writeSkill(t, source)
	placement := filepath.Join(t.TempDir(), "agent", "demo")
	if err := os.MkdirAll(filepath.Dir(placement), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, placement); err != nil {
		t.Fatal(err)
	}
	handler := resource.NewSkillHandler()

	for _, test := range []struct {
		action     resource.LifecycleAction
		wantAction string
	}{
		{resource.LifecycleRemove, "remove managed link"},
		{resource.LifecycleFork, "replace managed link with independent copy"},
	} {
		t.Run(string(test.action), func(t *testing.T) {
			planned, err := handler.PlanLifecycle(resource.SkillLifecycleRequest{
				Action:        test.action,
				LibraryPath:   library,
				Identifier:    "demo",
				PlacementPath: placement,
			})
			if err != nil {
				t.Fatalf("PlanLifecycle() error = %v", err)
			}
			plan := planned.(resource.SkillLifecyclePlan)
			if plan.SourcePath != source || len(plan.Changes) != 1 || plan.Changes[0].Action != test.wantAction {
				t.Fatalf("PlanLifecycle() = %#v", plan)
			}
		})
	}
}

func TestSkillHandlerPlansAdoptionAndReconciliation(t *testing.T) {
	library := t.TempDir()
	handler := resource.NewSkillHandler()
	projectSkill := filepath.Join(t.TempDir(), "agent", "demo")
	writeSkill(t, projectSkill)

	planned, err := handler.PlanLifecycle(resource.SkillLifecycleRequest{
		Action:        resource.LifecycleAdopt,
		LibraryPath:   library,
		Identifier:    "demo",
		PlacementPath: projectSkill,
	})
	if err != nil {
		t.Fatalf("PlanLifecycle(adopt) error = %v", err)
	}
	adopt := planned.(resource.SkillLifecyclePlan)
	if adopt.SourcePath != projectSkill || adopt.Resource.Provenance.Source != filepath.Join(library, "demo") || len(adopt.Changes) != 2 {
		t.Fatalf("PlanLifecycle(adopt) = %#v", adopt)
	}

	writeSkill(t, filepath.Join(library, "demo"))
	orphan := filepath.Join(t.TempDir(), "agent", "demo")
	old := filepath.Join(t.TempDir(), "old-library", "demo")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, orphan); err != nil {
		t.Fatal(err)
	}
	planned, err = handler.PlanLifecycle(resource.SkillLifecycleRequest{
		Action:        resource.LifecycleReconcile,
		LibraryPath:   library,
		Identifier:    "demo",
		PlacementPath: orphan,
		JournalSource: old,
		JournalOwned:  true,
	})
	if err != nil {
		t.Fatalf("PlanLifecycle(reconcile) error = %v", err)
	}
	reconcile := planned.(resource.SkillLifecyclePlan)
	if !reconcile.Applicable || reconcile.CurrentSource != old || reconcile.SourcePath != filepath.Join(library, "demo") || len(reconcile.Changes) != 1 {
		t.Fatalf("PlanLifecycle(reconcile) = %#v", reconcile)
	}
}

func writeSkill(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingCapabilitiesPreservesRequiredOrder(t *testing.T) {
	missing := resource.MissingCapabilities(
		[]resource.Capability{resource.CapabilityFilesystemRead, resource.CapabilityFilesystemWrite, resource.CapabilityMemorySearch},
		[]resource.Capability{resource.CapabilityFilesystemRead},
	)
	want := []resource.Capability{resource.CapabilityFilesystemWrite, resource.CapabilityMemorySearch}
	if len(missing) != len(want) {
		t.Fatalf("MissingCapabilities() = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("MissingCapabilities() = %v, want %v", missing, want)
		}
	}
}
