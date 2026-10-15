package memorypipeline

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

type evidenceController struct{}

func (evidenceController) Capabilities() map[policy.Control]bool {
	return map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlRunTermination: true}
}
func (evidenceController) PauseForApproval(context.Context, string, string) (bool, error) {
	return true, nil
}
func (evidenceController) ResolveApproval(context.Context, string, string, bool) (bool, error) {
	return true, nil
}
func (evidenceController) Kill(context.Context, string, string) (bool, error) { return true, nil }

func evidenceSnapshot(t *testing.T) policy.Snapshot {
	t.Helper()
	loaded, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: evidence\nname: Evidence\ntools:\n  allow: [read_file]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(loaded, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReferenceIdentityIsStableAndContentAddressed(t *testing.T) {
	base := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-1", Runtime: "codex", NativeRunID: "run-1", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}, Status: string(run.Completed), UpdatedAt: time.Date(2026, 10, 14, 12, 1, 0, 0, time.UTC), Events: []EvidenceEvent{{ID: "evt-b", Timestamp: time.Date(2026, 10, 14, 12, 0, 2, 0, time.UTC), Category: "ToolCallRequested", Decision: "ALLOW"}, {ID: "evt-a", Timestamp: time.Date(2026, 10, 14, 12, 0, 1, 0, time.UTC), Category: "AgentRunStarted", Decision: "ALLOW"}}}
	first, err := referenceFor(base)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Events = []EvidenceEvent{base.Events[1], base.Events[0]}
	second, err := referenceFor(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("reordered source events changed identity: %#v != %#v", first, second)
	}
	changed := base
	changed.Events = append([]EvidenceEvent(nil), base.Events...)
	changed.Events[0].Decision = "DENY"
	third, err := referenceFor(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID || third.ContentHash == first.ContentHash {
		t.Fatal("changed evidence reused the old immutable reference")
	}
}

func TestReferencePreservesProviderNeutralSourceAndHashesNativeContent(t *testing.T) {
	evidence := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: "project-native", Runtime: "codex", NativeRunID: "rollout-1", SourceRef: "codex://sessions/rollout-1", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: "completed", SourceDigest: "sha256:" + strings.Repeat("a", 64),
	}
	first, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceRef != "codex://sessions/rollout-1" {
		t.Fatalf("native source reference was rewritten: %#v", first)
	}
	evidence.SourceDigest = "sha256:" + strings.Repeat("b", 64)
	second, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if second.ContentHash == first.ContentHash || second.ID == first.ID {
		t.Fatal("changed native source bytes reused an immutable evidence identity")
	}
}

func TestReferenceRejectsUnboundedProviderMetadata(t *testing.T) {
	base := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: "project-native", Runtime: "codex", NativeRunID: "run-1", SourceRef: "codex://sessions/run-1", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: string(run.Completed),
	}
	for _, test := range []struct {
		name string
		edit func(*RunEvidence)
	}{
		{name: "runtime", edit: func(e *RunEvidence) { e.Ref.Runtime = strings.Repeat("r", 129) }},
		{name: "native id", edit: func(e *RunEvidence) {
			e.Ref.NativeRunID = strings.Repeat("r", 256)
			e.Ref.SourceRef = "codex://sessions/" + e.Ref.NativeRunID
		}},
		{name: "source ref query", edit: func(e *RunEvidence) { e.Ref.SourceRef += "?path=/outside" }},
		{name: "event reason", edit: func(e *RunEvidence) {
			e.Events = []EvidenceEvent{{Reason: strings.Repeat("r", maxEvidenceFieldBytes+1)}}
		}},
		{name: "policy controls", edit: func(e *RunEvidence) { e.PolicyControls = []string{strings.Repeat("c", maxEvidenceFieldBytes+1)} }},
		{name: "source digest", edit: func(e *RunEvidence) { e.SourceDigest = "sha256:bad" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.edit(&input)
			if _, err := referenceFor(input); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("unbounded or unsafe metadata error = %v", err)
			}
		})
	}
}

func TestReferenceRejectsNonterminalAndIncompleteEvidence(t *testing.T) {
	base := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-1", Runtime: "codex", NativeRunID: "run-1"}, Status: string(run.Active)}
	for _, test := range []struct {
		name string
		edit func(*RunEvidence)
	}{
		{name: "active run", edit: func(e *RunEvidence) { e.Status = string(run.Active) }},
		{name: "missing project", edit: func(e *RunEvidence) { e.Ref.ProjectID = ""; e.Status = string(run.Completed) }},
		{name: "unknown status", edit: func(e *RunEvidence) { e.Status = "unknown" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.edit(&input)
			if _, err := referenceFor(input); err == nil {
				t.Fatal("invalid evidence was accepted")
			}
		})
	}
}

func TestAgentManagerProviderReadsOnlyMatchingTerminalRunsWithoutMutation(t *testing.T) {
	rootA, rootB := filepath.Join(t.TempDir(), "project-a"), filepath.Join(t.TempDir(), "project-b")
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	managerRecord, _, err := manager.Start(evidenceSnapshot(t), "codex", rootA, identity.AnonymousSelection(), time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), evidenceController{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.EvaluateAndRecord(managerRecord.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{}, time.Date(2026, 10, 14, 12, 0, 1, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	terminal, err := manager.Kill(context.Background(), managerRecord.ID, run.TerminationOperatorRequested, time.Date(2026, 10, 14, 12, 0, 2, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := manager.Start(evidenceSnapshot(t), "codex", rootB, identity.AnonymousSelection(), time.Date(2026, 10, 14, 12, 0, 3, 0, time.UTC), evidenceController{})
	if err != nil {
		t.Fatal(err)
	}
	// Active runs are deliberately omitted even when they belong to the project.
	provider, err := NewAgentManagerRunProvider(store, "project-a", rootA)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := store.Events(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := provider.Discover(context.Background(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].NativeRunID != terminal.ID || refs[0].Runtime != "codex" {
		t.Fatalf("discovered refs = %#v", refs)
	}
	if _, err := provider.Read(context.Background(), refs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Read(context.Background(), RunEvidenceRef{ProjectID: "project-b", NativeRunID: other.ID, Status: ImportCompleted}); err == nil {
		t.Fatal("provider crossed project boundary")
	}
	if _, err := provider.Discover(context.Background(), "project-b"); err == nil {
		t.Fatal("provider accepted a different project identity")
	}
	after, err := store.Get(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterEvents, err := store.Events(terminal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeEvents, afterEvents) {
		t.Fatal("read-only adapter changed the source run store")
	}
}
