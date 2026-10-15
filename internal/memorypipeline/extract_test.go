package memorypipeline

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func TestExtractionIsDeterministicAndOmitsAbsentEvidence(t *testing.T) {
	evidence := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: "project-1", Runtime: "codex", NativeRunID: "run-1", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: string(run.Completed), UpdatedAt: time.Date(2026, 10, 14, 12, 1, 0, 0, time.UTC),
		PolicyID: "safe-policy", PolicyVersion: "v1", PolicyControls: []string{"tool_interception"},
		Events: []EvidenceEvent{{ID: "evt-1", Timestamp: time.Date(2026, 10, 14, 12, 0, 3, 0, time.UTC), Category: "ToolCallRequested", Decision: "DENY", Reason: "TOOL_NOT_ALLOWED"}},
	}
	ref, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	first, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("extraction changed across repeats:\n%s\n%s", firstJSON, secondJSON)
	}
	if first.Summary == "" || len(first.SummaryEvidenceIDs) != 1 || first.SummaryEvidenceIDs[0] != ref.ID {
		t.Fatalf("summary provenance = %#v", first)
	}
	seen := map[EvidenceField]int{}
	for _, item := range first.Memories {
		seen[item.Field]++
		if item.Verification != VerificationUnverified || item.State != DerivedActive || len(item.EvidenceIDs) != 1 || item.EvidenceIDs[0] != ref.ID {
			t.Fatalf("derived item trust or provenance = %#v", item)
		}
	}
	if seen[FieldDecision] != 1 || seen[FieldConstraint] != 1 {
		t.Fatalf("present evidence was not extracted: %#v", seen)
	}
	for _, absent := range []EvidenceField{FieldDiscovery, FieldKnownIssue, FieldCommand, FieldFailedApproach} {
		if seen[absent] != 0 {
			t.Fatalf("absent %s field was fabricated: %#v", absent, seen)
		}
	}
	for _, item := range first.Memories {
		if item.Type == memory.TypeSkill || item.Confidence != 1 {
			t.Fatalf("extractor changed trust or emitted a Skill: %#v", item)
		}
	}
}

func TestExtractionUsesOnlyExplicitOptionalFields(t *testing.T) {
	evidence := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: "project-2", Runtime: "test", NativeRunID: "run-2", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: string(run.Completed), SummaryText: "A bounded test summary.",
		Decisions: []string{"Use a transaction for the write."}, Discoveries: []string{"The index is project-scoped."},
		Constraints: []string{"Database writes require an explicit initialization."}, KnownIssues: []string{"Older references may be stale."},
		Commands: []string{"go test ./internal/memorypipeline"}, FailedApproaches: []string{"Replaying an unknown write duplicates external effects."},
	}
	ref, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	got, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[EvidenceField]string{}
	for _, item := range got.Memories {
		seen[item.Field] = item.Content
	}
	want := map[EvidenceField]string{
		FieldDecision: "Use a transaction for the write.", FieldDiscovery: "The index is project-scoped.",
		FieldConstraint: "Database writes require an explicit initialization.", FieldKnownIssue: "Older references may be stale.",
		FieldCommand: "go test ./internal/memorypipeline", FieldFailedApproach: "Replaying an unknown write duplicates external effects.",
	}
	for field, content := range want {
		if seen[field] != content {
			t.Errorf("%s = %q, want %q", field, seen[field], content)
		}
	}
}

func TestExtractionRejectsUnboundedAndMismatchedEvidence(t *testing.T) {
	evidence := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-1", Runtime: "codex", NativeRunID: "run-3", CreatedAt: time.Now().UTC()}, Status: string(run.Completed), Discoveries: []string{string(make([]byte, maxEvidenceFieldBytes+1))}}
	if _, err := referenceFor(evidence); err == nil {
		t.Fatal("oversized evidence field was accepted for hashing")
	}
	evidence = RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-1", Runtime: "codex", NativeRunID: "run-3", CreatedAt: time.Now().UTC()}, Status: string(run.Completed)}
	ref, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	evidence.Ref.ContentHash = "sha256:stale"
	if _, err := ExtractEvidence(evidence); err == nil {
		t.Fatal("mismatched evidence reference was accepted")
	}
}
