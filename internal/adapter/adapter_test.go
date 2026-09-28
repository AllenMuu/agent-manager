package adapter_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

func TestAgentAdapterContractDetectsInspectsAndPlansPlacement(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	a, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter was not registered")
	}
	var _ adapter.AgentAdapter = a

	if err := os.MkdirAll(filepath.Join(project, ".codex", "skills", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	detection, err := a.Detect(adapter.DetectionRequest{Project: project, Home: home})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !detection.Configured || !detection.Available {
		t.Fatalf("Detect() = %#v, want configured and available", detection)
	}

	items, err := a.Inspect(adapter.InspectionRequest{Project: project, Kind: resource.Skill})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(items) != 1 || items[0].Identifier != "local" || items[0].Kind != resource.Skill {
		t.Fatalf("Inspect() = %#v, want local Skill", items)
	}

	managed := resource.ManagedResource{Version: "v1", ID: "demo", Kind: resource.Skill}
	if err := a.ValidateResource(managed); err != nil {
		t.Fatalf("ValidateResource() error = %v", err)
	}
	placement, err := a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: managed})
	if err != nil {
		t.Fatalf("PlanPlacement() error = %v", err)
	}
	if placement.Destination != filepath.Join(project, ".codex", "skills", "demo") {
		t.Fatalf("placement destination = %q", placement.Destination)
	}
}

func TestSupportedTargetAdaptersOwnSkillPlacements(t *testing.T) {
	project := t.TempDir()
	want := map[adapter.Target]string{
		adapter.ClaudeCode: filepath.Join(project, ".claude", "skills", "demo"),
		adapter.Codex:      filepath.Join(project, ".codex", "skills", "demo"),
		adapter.Pi:         filepath.Join(project, ".pi", "skills", "demo"),
	}
	managed := resource.ManagedResource{Version: "v1", ID: "demo", Kind: resource.Skill}
	for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi} {
		t.Run(string(target), func(t *testing.T) {
			a, ok := adapter.For(target)
			if !ok {
				t.Fatalf("adapter.For(%q) was not registered", target)
			}
			placement, err := a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: managed})
			if err != nil {
				t.Fatalf("PlanPlacement() error = %v", err)
			}
			if placement.Target != target || placement.Destination != want[target] {
				t.Fatalf("PlanPlacement() = %#v, want target %q destination %q", placement, target, want[target])
			}
		})
	}
}

func TestDirectoryAdaptersExplicitlyLackGovernanceRuntimeCapabilities(t *testing.T) {
	controls := []policy.Control{
		policy.ControlToolInterception, policy.ControlApprovalPauseResume,
		policy.ControlDurationBudget, policy.ControlCostBudget, policy.ControlToolCallBudget,
		policy.ControlSubagentLimits, policy.ControlNetworkRestriction,
		policy.ControlCredentialScope, policy.ControlRunTermination, policy.ControlRuntimeEvents,
	}
	for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi} {
		a, ok := adapter.ForAgent(target)
		if !ok {
			t.Fatalf("adapter.ForAgent(%q) was not registered", target)
		}
		capabilities := a.GovernanceCapabilities()
		for _, control := range controls {
			if supported, declared := capabilities[control]; !declared || supported {
				t.Errorf("%s capability %s = %t (declared %t), want explicit unsupported", target, control, supported, declared)
			}
		}
	}
}

func TestGovernanceEventsNormalizeIdenticallyAcrossAdapters(t *testing.T) {
	native := adapter.RuntimeEvent{Category: policy.ToolCallRequested, Actor: "agent", Tool: "read_file", ActionType: "inspect", Timestamp: time.Date(2026, 9, 27, 1, 0, 0, 0, time.FixedZone("test", 8*60*60))}
	var first policy.Event
	for i, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi} {
		got, err := adapter.NormalizeGovernanceEvent(target, native)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got.Category != first.Category || got.Actor != first.Actor || got.Tool != first.Tool || got.ActionType != first.ActionType || !got.Timestamp.Equal(first.Timestamp) {
			t.Fatalf("normalized %s event = %#v, want canonical event %#v", target, got, first)
		}
	}
}

func TestGovernanceCompletionEventsRetainRequestAuditID(t *testing.T) {
	cases := []adapter.RuntimeEvent{
		{Category: policy.ToolCallCompleted, Tool: "read_file", RequestAuditID: "evt-request", Timestamp: time.Now().UTC()},
		{Category: policy.NetworkAccessCompleted, Domain: "example.com", RequestAuditID: "evt-request", Timestamp: time.Now().UTC()},
	}
	for _, native := range cases {
		for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex, adapter.Pi} {
			got, err := adapter.NormalizeGovernanceEvent(target, native)
			if err != nil {
				t.Fatalf("NormalizeGovernanceEvent(%s, %s) error = %v", target, native.Category, err)
			}
			if got.RequestAuditID != native.RequestAuditID {
				t.Errorf("normalized %s completion request audit id = %q, want %q", target, got.RequestAuditID, native.RequestAuditID)
			}
		}
	}
}

func TestUnsupportedAgentRootsRequireRealDirectorySkillsLocations(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".unsupported", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".symlink"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, ".unsupported", "skills"), filepath.Join(project, ".symlink", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".regular"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".regular", "skills"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, ".unsupported"), filepath.Join(project, ".agent-link")); err != nil {
		t.Fatal(err)
	}

	roots, err := adapter.UnsupportedAgentRoots(project)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(project, ".unsupported")}
	if len(roots) != len(want) || roots[0] != want[0] {
		t.Fatalf("UnsupportedAgentRoots() = %v, want %v", roots, want)
	}
}

func TestPlanPlacementRefusesMissingRequiredCapabilities(t *testing.T) {
	a, _ := adapter.For(adapter.Codex)
	managed := resource.ManagedResource{
		Version:              "v1",
		ID:                   "needs-memory",
		Kind:                 resource.Skill,
		RequiredCapabilities: []resource.Capability{resource.CapabilityMemoryRead},
	}
	placement, err := a.PlanPlacement(adapter.PlacementRequest{Project: t.TempDir(), Resource: managed})
	if err == nil {
		t.Fatalf("PlanPlacement() = %#v, nil error; want missing capability refusal", placement)
	}
	if placement != (adapter.Placement{}) {
		t.Fatalf("PlanPlacement() = %#v, want no placement", placement)
	}
}

func TestPlaceFilesystemRefusesUnmanagedConflictWithoutForce(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	if err := os.WriteFile(destination, []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Journal:     journal,
		Confirm:     func(operation.Plan) bool { t.Fatal("confirmation must not be requested"); return true },
	})
	if !errors.Is(err, adapter.ErrForceRequired) {
		t.Fatalf("PlaceFilesystem() error = %v, want force-required", err)
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != "user-owned" {
		t.Fatalf("unmanaged destination changed: %q, %v", got, readErr)
	}
}

func TestPlaceFilesystemRequiresConfirmationAndJournalsReplacement(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	if err := os.WriteFile(filepath.Join(source, "run.sh"), []byte("#!/bin/sh\nprintf unsafe\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	confirmed := false
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	preview, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Force:       true,
		Journal:     journal,
		Confirm: func(p operation.Plan) bool {
			confirmed = true
			return len(p.Changes) == 1
		},
	})
	if err != nil {
		t.Fatalf("PlaceFilesystem() error = %v", err)
	}
	if !confirmed || len(preview.Changes) != 1 {
		t.Fatalf("preview = %#v, confirmed = %v", preview, confirmed)
	}
	target, err := os.Readlink(destination)
	if err != nil || target != source {
		t.Fatalf("destination = %q, %v; want link to source", target, err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("UndoLatest() error = %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "user-owned" {
		t.Fatalf("undo restored %q, %v; want unmanaged content", got, err)
	}
}

func TestPlaceFilesystemRejectsConflictChangedDuringConfirmation(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "original.txt"), []byte("original unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Force:       true,
		Journal:     journal,
		Confirm: func(operation.Plan) bool {
			if err := os.RemoveAll(destination); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte("new owner"), 0o644); err != nil {
				t.Fatal(err)
			}
			return true
		},
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "new owner" {
		t.Fatalf("new owner changed: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("confirmation race journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
	backups, globErr := filepath.Glob(filepath.Join(filepath.Dir(journal.Path), ".skill-manager-journal", "*"))
	if globErr != nil || len(backups) != 1 {
		t.Fatalf("captured prior content = %#v, %v; want one recoverable snapshot", backups, globErr)
	}
	if got, readErr := os.ReadFile(filepath.Join(backups[0], "original.txt")); readErr != nil || string(got) != "original unmanaged" {
		t.Fatalf("captured prior content = %q, %v", got, readErr)
	}
}

func TestPlaceFilesystemDoesNotExecuteExecutableResourceContent(t *testing.T) {
	source := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(source, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	project, destination := testCodexPlacement(t)
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	if _, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Journal:     journal,
		Confirm:     func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("PlaceFilesystem() error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("resource executable ran; marker stat error = %v", err)
	}
}

func TestPlaceFilesystemRevalidatesSourceAfterConfirmation(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Journal:     journal,
		Confirm: func(operation.Plan) bool {
			return os.RemoveAll(source) == nil
		},
	})
	if err == nil {
		t.Fatal("PlaceFilesystem unexpectedly succeeded after source changed")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination after stale confirmation = %v, want absent", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("failed placement journal state = ok=%v err=%v, want no entry", ok, err)
	}
}

func TestPlaceFilesystemPreservesLateUnmanagedConflict(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Journal:     journal,
		BeforePublish: func() error {
			return os.WriteFile(destination, []byte("late unmanaged"), 0o644)
		},
		Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path", err)
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != "late unmanaged" {
		t.Fatalf("late unmanaged destination changed: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("late conflict journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
}

func TestPlaceFilesystemNoReplacePublicationDoesNotOverwriteLateConflict(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Journal:     journal,
		BeforeFinalPublish: func() error {
			return os.WriteFile(destination, []byte("late unmanaged"), 0o644)
		},
		Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path", err)
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != "late unmanaged" {
		t.Fatalf("late conflict changed: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("late conflict journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
}

func TestPlaceFilesystemDoesNotFollowParentSwappedBeforeFinalPublish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace bool
	}{
		{name: "create"},
		{name: "replace", replace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			project, destination := testCodexPlacement(t)
			parent := filepath.Dir(destination)
			recoverableParent := parent + "-recoverable"
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("external owner"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.replace {
				if err := os.WriteFile(destination, []byte("original owner"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
			plan := resource.PlacementPlan{Resource: resource.ManagedResource{
				Version: "v1", ID: "demo", Kind: resource.Skill,
				Provenance: resource.Provenance{Source: source},
			}}
			options := adapter.FilesystemPlacementOptions{
				Project: project, Target: adapter.Codex,
				Destination: destination,
				Journal:     journal,
				Confirm:     func(operation.Plan) bool { return true },
				BeforeFinalPublish: func() error {
					if err := os.Rename(parent, recoverableParent); err != nil {
						return err
					}
					return os.Symlink(external, parent)
				},
			}
			if tc.replace {
				options.Conflict = adapter.ConflictReplace
				options.Force = true
			}

			_, err := adapter.PlaceFilesystem(plan, options)
			if !errors.Is(err, adapter.ErrUnsafePath) {
				t.Fatalf("PlaceFilesystem() error = %v, want unsafe path", err)
			}
			if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "external owner" {
				t.Fatalf("external sentinel changed: %q, %v", got, readErr)
			}
			if _, statErr := os.Lstat(filepath.Join(external, "demo")); !os.IsNotExist(statErr) {
				t.Fatalf("external destination was published: %v", statErr)
			}
			if tc.replace {
				got, readErr := os.ReadFile(filepath.Join(recoverableParent, "demo"))
				if readErr != nil || string(got) != "original owner" {
					t.Fatalf("recoverable original = %q, %v; placement error = %v", got, readErr, err)
				}
			} else if _, statErr := os.Lstat(filepath.Join(recoverableParent, "demo")); !os.IsNotExist(statErr) {
				t.Fatalf("managed link remained in recoverable parent: %v", statErr)
			}
			if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
				t.Fatalf("parent-swap journal state = ok=%v err=%v, want no entry", ok, journalErr)
			}
		})
	}
}

func TestPlaceFilesystemPreservesReplacedContentWhenLateOwnerAppears(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "original.txt"), []byte("original unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination, Conflict: adapter.ConflictReplace, Force: true, Journal: journal,
		BeforeFinalPublish: func() error {
			return os.WriteFile(destination, []byte("late owner"), 0o644)
		},
		Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "late owner" {
		t.Fatalf("late owner changed: %q, %v", got, readErr)
	}
	recovery, globErr := filepath.Glob(destination + ".skill-manager-recovery-*")
	if globErr != nil || len(recovery) != 1 {
		t.Fatalf("recovery paths = %#v, %v; want one preserved original", recovery, globErr)
	}
	if got, readErr := os.ReadFile(filepath.Join(recovery[0], "original.txt")); readErr != nil || string(got) != "original unmanaged" {
		t.Fatalf("preserved original = %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("late replacement journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
}

func TestPlaceFilesystemWrongLinkRequiresForceAndOperationConfirmation(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	wrongTarget := filepath.Join(t.TempDir(), "other")
	if err := os.Symlink(wrongTarget, destination); err != nil {
		t.Fatal(err)
	}
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Journal:     journal,
		Confirm:     func(operation.Plan) bool { t.Fatal("confirmation must not be requested"); return true },
	})
	if !errors.Is(err, adapter.ErrForceRequired) {
		t.Fatalf("without force error = %v, want force-required", err)
	}

	_, err = adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Force:       true,
		Journal:     journal,
		Confirm:     func(operation.Plan) bool { return false },
	})
	if !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("declined replacement error = %v, want not-confirmed", err)
	}
	got, readErr := os.Readlink(destination)
	if readErr != nil || got != wrongTarget {
		t.Fatalf("wrong link after declined replacement = %q, %v", got, readErr)
	}
}

func TestAdapterPlaceUsesGuardedFilesystemPlacement(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	a, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter is unavailable")
	}
	plan := resource.PlacementPlan{
		Project: project,
		Resource: resource.ManagedResource{
			Version: "v1", ID: "demo", Kind: resource.Skill,
			Provenance: resource.Provenance{Source: source},
		},
		Destination: destination,
		Journal:     journal,
		Confirm:     func(operation.Plan) bool { return true },
	}
	if err := a.Place(plan); err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if target, err := os.Readlink(destination); err != nil || target != source {
		t.Fatalf("destination = %q, %v; want source link", target, err)
	}
}

func TestAdapterPlaceRejectsSymlinkedSkillsRootAndArbitraryDestination(t *testing.T) {
	source := t.TempDir()
	project := t.TempDir()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("external owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(project, ".codex", "skills")); err != nil {
		t.Fatal(err)
	}
	a, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter is unavailable")
	}
	managed := resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{
		Project: project, Resource: managed,
		Destination: filepath.Join(project, ".codex", "skills", "demo"),
		Journal:     journal, Confirm: func(operation.Plan) bool { return true },
	}
	if err := a.Place(plan); !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("Place() error = %v, want unsafe path for symlinked skills root", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "external owner" {
		t.Fatalf("external sentinel changed: %q, %v", got, err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("symlinked-root journal state = ok=%v err=%v, want no entry", ok, err)
	}

	project = t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal = operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan.Project = project
	plan.Destination = filepath.Join(external, "demo")
	plan.Journal = journal
	if err := a.Place(plan); !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("Place() error = %v, want unsafe path for arbitrary destination", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "external owner" {
		t.Fatalf("external sentinel changed by arbitrary destination: %q, %v", got, err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("arbitrary destination journal state = ok=%v err=%v, want no entry", ok, err)
	}
}

func TestPlaceFilesystemRejectsSymlinkedParentAndArbitraryDestination(t *testing.T) {
	source := t.TempDir()
	project := t.TempDir()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("external owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(project, ".codex", "skills")); err != nil {
		t.Fatal(err)
	}
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: filepath.Join(project, ".codex", "skills", "demo"),
		Journal:     journal, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path for symlinked parent", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "external owner" {
		t.Fatalf("external sentinel changed: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}

	project = t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal = operation.New(filepath.Join(t.TempDir(), "journal.json"))
	_, err = adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: filepath.Join(external, "demo"),
		Journal:     journal, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("PlaceFilesystem() error = %v, want unsafe path for arbitrary destination", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "external owner" {
		t.Fatalf("external sentinel changed by arbitrary destination: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("arbitrary destination journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
}

func TestPlaceFilesystemRollsBackWhenPublicationIsInterrupted(t *testing.T) {
	source := t.TempDir()
	project, destination := testCodexPlacement(t)
	if err := os.WriteFile(destination, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
	plan := resource.PlacementPlan{Resource: resource.ManagedResource{
		Version: "v1", ID: "demo", Kind: resource.Skill,
		Provenance: resource.Provenance{Source: source},
	}}
	_, err := adapter.PlaceFilesystem(plan, adapter.FilesystemPlacementOptions{
		Project: project, Target: adapter.Codex,
		Destination: destination,
		Conflict:    adapter.ConflictReplace,
		Force:       true,
		Journal:     journal,
		BeforeRemove: func() error {
			return errors.New("publication interrupted")
		},
		Confirm: func(operation.Plan) bool { return true },
	})
	if err == nil {
		t.Fatal("PlaceFilesystem() error = nil, want interruption")
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != "unmanaged" {
		t.Fatalf("interrupted placement changed unmanaged destination: %q, %v", got, readErr)
	}
}

func TestPiRejectsUnsupportedResourceKindsWithoutWriting(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".pi", "skills", "sentinel")
	candidate := filepath.Join(project, ".pi", "skills", "demo")
	parent := filepath.Dir(candidate)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, ok := adapter.For(adapter.Pi)
	if !ok {
		t.Fatal("Pi adapter was not registered")
	}
	parentBefore := snapshotDirectory(t, parent)
	assertAbsent(t, candidate)
	for _, kind := range []resource.Kind{resource.SubAgent, resource.Memory} {
		t.Run(string(kind), func(t *testing.T) {
			managed := resource.ManagedResource{Version: "v1", ID: "demo", Kind: kind}
			inspections, inspectErr := a.Inspect(adapter.InspectionRequest{Project: project, Kind: kind})
			var unsupported *adapter.UnsupportedResourceKindError
			if !errors.As(inspectErr, &unsupported) {
				t.Fatalf("Inspect() = %#v, error = %v; want UnsupportedResourceKindError", inspections, inspectErr)
			}
			assertUnchanged(t, candidate, parent, parentBefore)

			err := a.ValidateResource(managed)
			unsupported = nil
			if !errors.As(err, &unsupported) {
				t.Fatalf("ValidateResource() error = %v, want UnsupportedResourceKindError", err)
			}
			if unsupported.Target != adapter.Pi || unsupported.Kind != kind {
				t.Fatalf("unsupported error = %#v, want Pi/%s", unsupported, kind)
			}
			if !strings.Contains(err.Error(), string(kind)) {
				t.Fatalf("ValidateResource() error = %q, want resource kind", err)
			}
			assertUnchanged(t, candidate, parent, parentBefore)

			unsupported = nil
			placement, planErr := a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: managed})
			if !errors.As(planErr, &unsupported) {
				t.Fatalf("PlanPlacement() error = %v, want UnsupportedResourceKindError", planErr)
			}
			if placement != (adapter.Placement{}) {
				t.Fatalf("PlanPlacement() = %#v, want empty placement", placement)
			}
			assertUnchanged(t, candidate, parent, parentBefore)
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "unmanaged" {
				t.Fatalf("unsupported placement changed sentinel: %q, %v", got, readErr)
			}
		})
	}
}

func snapshotDirectory(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", path, err)
	}
	snapshot := make([]string, 0, len(entries))
	for _, entry := range entries {
		snapshot = append(snapshot, entry.Name()+"\\x00"+entry.Type().String())
	}
	return snapshot
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Lstat(%q) = %v, want absent", path, err)
	}
}

func assertUnchanged(t *testing.T, candidate, parent string, before []string) {
	t.Helper()
	assertAbsent(t, candidate)
	after := snapshotDirectory(t, parent)
	if len(after) != len(before) {
		t.Fatalf("parent %q entries changed: before=%v after=%v", parent, before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("parent %q entries changed: before=%v after=%v", parent, before, after)
		}
	}
}

func testCodexPlacement(t *testing.T) (string, string) {
	t.Helper()
	project := t.TempDir()
	path := filepath.Join(project, ".codex", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return project, path
}

func TestSupportedProjectAndGlobalLocations(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	cases := []struct {
		target adapter.Target
		path   string
		global string
	}{
		{adapter.ClaudeCode, filepath.Join(project, ".claude", "skills", "demo"), filepath.Join(home, ".claude", "skills", "demo")},
		{adapter.Codex, filepath.Join(project, ".codex", "skills", "demo"), filepath.Join(home, ".codex", "skills", "demo")},
		{adapter.Pi, filepath.Join(project, ".pi", "skills", "demo"), filepath.Join(home, ".pi", "agent", "skills", "demo")},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			a, ok := adapter.For(tc.target)
			if !ok {
				t.Fatalf("adapter %q was not supported", tc.target)
			}
			if got := a.ProjectSkillPath(project, "demo"); got != tc.path {
				t.Fatalf("project path = %q, want %q", got, tc.path)
			}
			if got := a.GlobalSkillPath(home, "demo"); got != tc.global {
				t.Fatalf("global path = %q, want %q", got, tc.global)
			}
			if !a.Supports(resource.Skill) {
				t.Fatalf("%s does not declare Skill support", tc.target)
			}
			if !a.HasCapability(resource.Skill, resource.CapabilityFilesystemWrite) {
				t.Fatalf("%s does not declare filesystem write capability", tc.target)
			}
		})
	}
}

func TestValidateIdentifierRejectsTraversalAndPathForms(t *testing.T) {
	for _, identifier := range []string{"", ".", "..", "a/b", "a\\b", "../demo", "demo/.."} {
		if err := adapter.ValidateIdentifier(identifier); err == nil {
			t.Fatalf("ValidateIdentifier(%q) accepted unsafe identifier", identifier)
		}
	}
	if err := adapter.ValidateIdentifier("safe-skill_2"); err != nil {
		t.Fatal(err)
	}
}

func TestDetectWithEmptyLocationsDoesNotInspectRelativeWorkingDirectory(t *testing.T) {
	working := t.TempDir()
	if err := os.MkdirAll(filepath.Join(working, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	a, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter was not registered")
	}
	detection, err := a.Detect(adapter.DetectionRequest{})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if detection.Available || detection.Configured {
		t.Fatalf("Detect() = %#v, want unavailable with empty locations", detection)
	}
}

func TestMatchProjectSkillPathUsesAdapterPlacement(t *testing.T) {
	project := t.TempDir()
	a, identifier, ok := adapter.MatchProjectSkillPath(project, filepath.Join(project, ".pi", "skills", "demo"))
	if !ok || a == nil || a.Target() != adapter.Pi || identifier != "demo" {
		t.Fatalf("MatchProjectSkillPath() = %v, %q, %v; want Pi/demo", a, identifier, ok)
	}
	if _, _, ok := adapter.MatchProjectSkillPath(project, filepath.Join(project, ".pi", "skills", "nested", "demo")); ok {
		t.Fatal("nested path accepted as a skill placement")
	}
	if _, _, ok := adapter.MatchProjectSkillPath(project, filepath.Join(project, ".unknown", "skills", "demo")); ok {
		t.Fatal("unsupported agent placement accepted")
	}
}

func TestInventoryDiscoversSupportedAndUnsupportedAgents(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(project, ".foo", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".pi", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	items, err := adapter.Inventory(home, project)
	if err != nil {
		t.Fatal(err)
	}
	var foundPi, foundFoo bool
	for _, item := range items {
		switch item.ID {
		case "pi":
			foundPi = true
			if item.Status != "supported" || item.Availability != "configured" {
				t.Fatalf("Pi inventory = %#v", item)
			}
		case "foo":
			foundFoo = true
			if item.Status != "unsupported" || item.Availability != "unsupported" || len(item.ResourceKinds) != 0 {
				t.Fatalf("unsupported inventory = %#v", item)
			}
		}
	}
	if !foundPi || !foundFoo {
		t.Fatalf("Inventory() = %#v; want Pi and foo", items)
	}
}

func TestInspectWithEmptyProjectDoesNotInspectRelativeWorkingDirectory(t *testing.T) {
	working := t.TempDir()
	if err := os.Mkdir(filepath.Join(working, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, ".codex", "skills"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	a, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter was not registered")
	}
	items, err := a.Inspect(adapter.InspectionRequest{Kind: resource.Skill})
	if err == nil || !strings.Contains(err.Error(), "project is required") {
		t.Fatalf("Inspect() = %#v, error = %v; want project-required error before filesystem read", items, err)
	}
	if items != nil {
		t.Fatalf("Inspect() = %#v, want no inspections", items)
	}
}
