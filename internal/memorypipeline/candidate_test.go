package memorypipeline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

type candidateFixtureProvider map[string]RunEvidence

func (p candidateFixtureProvider) Discover(_ context.Context, projectID string) ([]RunEvidenceRef, error) {
	refs := make([]RunEvidenceRef, 0)
	for _, evidence := range p {
		if evidence.Ref.ProjectID == projectID {
			refs = append(refs, evidence.Ref)
		}
	}
	return refs, nil
}

func (p candidateFixtureProvider) Read(_ context.Context, ref RunEvidenceRef) (RunEvidence, error) {
	evidence, ok := p[candidateFixtureKey(ref.ProjectID, ref.Runtime, ref.NativeRunID)]
	if !ok {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	if !sameReference(evidence.Ref, ref) {
		return RunEvidence{}, ErrEvidenceChanged
	}
	return evidence, nil
}

func TestSkillCandidatesRequireIndependentSuccessfulGeneralizableRunsWithoutFailure(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	baseTime := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	provider := candidateFixtureProvider{}
	add := func(project, runID, key, outcome string, generalizable bool, status string, contentOverride ...string) RunEvidence {
		content := "Validate cache keys include project identity."
		if len(contentOverride) > 0 {
			content = contentOverride[0]
		}
		evidence := RunEvidence{
			Ref:    RunEvidenceRef{ProjectID: project, Runtime: "agent-manager", NativeRunID: runID, CreatedAt: baseTime},
			Status: status, UpdatedAt: baseTime.Add(time.Minute),
			Procedures: []ProcedureEvidence{{Key: key, Content: content, Outcome: outcome, Generalizable: generalizable}},
		}
		ref, err := ReferenceFor(evidence)
		if err != nil {
			t.Fatal(err)
		}
		evidence.Ref = ref
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
		provider[candidateFixtureKey(project, evidence.Ref.Runtime, runID)] = evidence
		return evidence
	}

	const blockedProject = "project-candidates-blocked"
	add(blockedProject, "run-one", "cache-key-validation", ProcedureOutcomeVerified, true, "completed")
	add(blockedProject, "run-two", "cache-key-validation", ProcedureOutcomeSucceeded, true, "completed")
	add(blockedProject, "project-local-one", "local-migration", ProcedureOutcomeSucceeded, false, "completed")
	add(blockedProject, "project-local-two", "local-migration", ProcedureOutcomeSucceeded, false, "completed")
	add(blockedProject, "reusable-one", "project-sensitive-flow", ProcedureOutcomeSucceeded, true, "completed")
	add(blockedProject, "reusable-two", "project-sensitive-flow", ProcedureOutcomeVerified, true, "completed")
	add(blockedProject, "specific-claim", "project-sensitive-flow", ProcedureOutcomeSucceeded, false, "completed")
	add(blockedProject, "unverified-one", "unverified-procedure", "attempted", true, "completed")
	add(blockedProject, "unverified-two", "unverified-procedure", "attempted", true, "completed")
	add(blockedProject, "run-failed", "cache-key-validation", ProcedureOutcomeFailed, true, "failed", "")
	if got, err := store.DetectSkillCandidates(ctx, blockedProject, provider); err != nil || len(got) != 0 {
		t.Fatalf("project-specific or contradicted procedure became a candidate: %#v, %v", got, err)
	}

	const project = "project-candidates"
	add(project, "run-a", "cache-key-validation", ProcedureOutcomeVerified, true, "completed")
	add(project, "run-b", "cache-key-validation", ProcedureOutcomeSucceeded, true, "completed")
	candidates, err := store.DetectSkillCandidates(ctx, project, provider)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("repeated validated procedure candidates = %#v, %v", candidates, err)
	}
	candidate := candidates[0]
	generation, err := store.EvidenceGeneration(ctx, project)
	if err != nil || candidate.EvidenceGeneration != generation || generation != 2 {
		t.Fatalf("candidate evidence generation = %d, current=%d, err=%v", candidate.EvidenceGeneration, generation, err)
	}
	if candidate.Status != SkillCandidatePendingReview || candidate.ProcedureKey != "cache-key-validation" || len(candidate.EvidenceRefs) != 2 || candidate.Name != "Cache Key Validation" || candidate.ProjectSpecific || candidate.ProposedWorkflow != "Validate cache keys include project identity." {
		t.Fatalf("candidate omitted its review metadata or independent evidence: %#v", candidate)
	}
	listed, err := store.ListSkillCandidates(ctx, project)
	if err != nil || len(listed) != 1 || listed[0].ID != candidate.ID {
		t.Fatalf("persisted candidate list = %#v, %v", listed, err)
	}
	if _, err := store.ReviewSkillCandidate(ctx, project, candidate.ID, SkillCandidateApproved, false); err != memory.ErrNotConfirmed {
		t.Fatalf("candidate review without explicit confirmation = %v", err)
	}
	pending, err := store.SkillCandidate(ctx, project, candidate.ID)
	if err != nil || pending.Status != SkillCandidatePendingReview {
		t.Fatalf("unconfirmed review changed candidate state: %#v, %v", pending, err)
	}
	if _, err := store.ReviewSkillCandidate(ctx, project, candidate.ID, SkillCandidateApproved, true); err != nil {
		t.Fatal(err)
	}
	reviewed, err := store.SkillCandidate(ctx, project, candidate.ID)
	if err != nil || reviewed.Status != SkillCandidateApproved {
		t.Fatalf("reviewed candidate = %#v, %v", reviewed, err)
	}
}

func TestOperatorProcedureClaimsEnableCandidateDetectionAndInvalidateOnFailure(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const project = "project-operator-claims"
	createdAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	provider := candidateFixtureProvider{}
	addRun := func(runID string) RunEvidence {
		t.Helper()
		evidence := RunEvidence{Ref: RunEvidenceRef{ProjectID: project, Runtime: "fixture", NativeRunID: runID, CreatedAt: createdAt}, Status: "completed", UpdatedAt: createdAt.Add(time.Minute)}
		ref, err := ReferenceFor(evidence)
		if err != nil {
			t.Fatal(err)
		}
		evidence.Ref = ref
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
		provider[candidateFixtureKey(project, ref.Runtime, runID)] = evidence
		return evidence
	}
	procedure := ProcedureEvidence{Key: "shared-cache-validation", Content: "Validate cache keys include project identity.", Outcome: ProcedureOutcomeVerified, Generalizable: true}
	first := addRun("operator-claim-run-a")
	if _, err := store.RecordProcedureClaim(ctx, project, first.Ref.ID, procedure, false); err != memory.ErrNotConfirmed {
		t.Fatalf("unconfirmed procedure claim error = %v", err)
	}
	firstClaim, err := store.RecordProcedureClaim(ctx, project, first.Ref.ID, procedure, true)
	if err != nil || firstClaim.Origin != "operator" {
		t.Fatalf("record operator procedure claim = %#v, %v", firstClaim, err)
	}
	second := addRun("operator-claim-run-b")
	if _, err := store.RecordProcedureClaim(ctx, project, second.Ref.ID, procedure, true); err != nil {
		t.Fatal(err)
	}
	generation, err := store.EvidenceGeneration(ctx, project)
	if err != nil || generation != 4 {
		t.Fatalf("candidate input generation = %d, %v; want two refs and two claims", generation, err)
	}
	repeated, err := store.RecordProcedureClaim(ctx, project, first.Ref.ID, procedure, true)
	if err != nil || repeated.ID != firstClaim.ID {
		t.Fatalf("duplicate claim was not idempotent: %#v, %v", repeated, err)
	}
	if after, err := store.EvidenceGeneration(ctx, project); err != nil || after != generation {
		t.Fatalf("idempotent claim advanced generation to %d, %v", after, err)
	}
	candidates, err := store.DetectSkillCandidates(ctx, project, provider)
	if err != nil || len(candidates) != 1 || len(candidates[0].EvidenceRefs) != 2 || candidates[0].Status != SkillCandidatePendingReview {
		t.Fatalf("operator claims did not produce a reviewable candidate: %#v, %v", candidates, err)
	}
	candidate := candidates[0]
	if _, err := store.ReviewSkillCandidate(ctx, project, candidate.ID, SkillCandidateApproved, true); err != nil {
		t.Fatal(err)
	}
	failed := addRun("operator-claim-run-failed")
	if _, err := store.RecordProcedureClaim(ctx, project, failed.Ref.ID, ProcedureEvidence{Key: procedure.Key, Content: procedure.Content, Outcome: ProcedureOutcomeFailed, Generalizable: true}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewSkillCandidate(ctx, project, candidate.ID, SkillCandidateRejected, true); err == nil {
		t.Fatal("stale candidate accepted a new review decision")
	}
	updated, err := store.DetectSkillCandidates(ctx, project, provider)
	if err != nil || len(updated) != 1 || updated[0].Status != SkillCandidateInvalidated || len(updated[0].EvidenceRefs) != 3 {
		t.Fatalf("failed operator claim did not invalidate prior candidate: %#v, %v", updated, err)
	}
}

func TestSkillCandidateReadsEveryImportedReferenceAndRejectsStaleEvidence(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	evidence := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-candidate-stale", Runtime: "runtime", NativeRunID: "run-one", CreatedAt: createdAt}, Status: "completed", UpdatedAt: createdAt.Add(time.Minute), Procedures: []ProcedureEvidence{{Key: "safe-recovery", Content: "Recover safely.", Outcome: ProcedureOutcomeVerified, Generalizable: true}}}
	ref, err := ReferenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	changed := evidence
	changed.Procedures = []ProcedureEvidence{{Key: "safe-recovery", Content: "Recover safely with validation.", Outcome: ProcedureOutcomeVerified, Generalizable: true}}
	changedRef, err := ReferenceFor(changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.Ref = changedRef
	provider := candidateFixtureProvider{candidateFixtureKey(changed.Ref.ProjectID, changed.Ref.Runtime, changed.Ref.NativeRunID): changed}
	if got, err := store.DetectSkillCandidates(ctx, evidence.Ref.ProjectID, provider); err != ErrEvidenceChanged || len(got) != 0 {
		t.Fatalf("stale imported evidence was accepted: %#v, %v", got, err)
	}
}

func TestSkillCandidateDoesNotCountDuplicateProceduresWithinOneRunTwice(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	evidence := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-candidate-one", Runtime: "runtime", NativeRunID: "same-run", CreatedAt: createdAt}, Status: "completed", UpdatedAt: createdAt.Add(time.Minute), Procedures: []ProcedureEvidence{
		{Key: "safe-recovery", Content: "Recover safely.", Outcome: ProcedureOutcomeSucceeded, Generalizable: true},
		{Key: "safe-recovery", Content: "Recover safely.", Outcome: ProcedureOutcomeVerified, Generalizable: true},
	}}
	ref, err := ReferenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	provider := candidateFixtureProvider{candidateFixtureKey(ref.ProjectID, ref.Runtime, ref.NativeRunID): evidence}
	if got, err := store.DetectSkillCandidates(ctx, ref.ProjectID, provider); err != nil || len(got) != 0 {
		t.Fatalf("duplicate procedures in one native run counted as independent: %#v, %v", got, err)
	}
}

func TestContradictoryEvidenceInvalidatesPreviouslyApprovedCandidate(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	project := "project-candidate-contradiction"
	createdAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	provider := candidateFixtureProvider{}
	add := func(runID, status, outcome string) {
		t.Helper()
		evidence := RunEvidence{
			Ref:    RunEvidenceRef{ProjectID: project, Runtime: "fixture", NativeRunID: runID, CreatedAt: createdAt},
			Status: status, UpdatedAt: createdAt.Add(time.Minute),
			Procedures: []ProcedureEvidence{{Key: "safe-recovery", Content: "Recover safely.", Outcome: outcome, Generalizable: true}},
		}
		ref, err := ReferenceFor(evidence)
		if err != nil {
			t.Fatal(err)
		}
		evidence.Ref = ref
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
		provider[candidateFixtureKey(project, ref.Runtime, runID)] = evidence
	}
	add("run-one", "completed", ProcedureOutcomeVerified)
	add("run-two", "completed", ProcedureOutcomeSucceeded)
	candidates, err := store.DetectSkillCandidates(ctx, project, provider)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("initial candidate detection = %#v, %v", candidates, err)
	}
	candidate := candidates[0]
	if _, err := store.ReviewSkillCandidate(ctx, project, candidate.ID, SkillCandidateApproved, true); err != nil {
		t.Fatal(err)
	}

	add("run-failed", "failed", ProcedureOutcomeFailed)
	generation, err := store.EvidenceGeneration(ctx, project)
	if err != nil || generation != 3 || candidate.EvidenceGeneration == generation {
		t.Fatalf("new evidence did not make the reviewed candidate stale: candidate=%d current=%d err=%v", candidate.EvidenceGeneration, generation, err)
	}
	updated, err := store.DetectSkillCandidates(ctx, project, provider)
	if err != nil || len(updated) != 1 || updated[0].ID != candidate.ID || updated[0].Status != SkillCandidateInvalidated {
		t.Fatalf("contradictory evidence did not invalidate approved candidate: %#v, %v", updated, err)
	}
	if len(updated[0].EvidenceRefs) != 3 || updated[0].EvidenceGeneration != generation {
		t.Fatalf("invalidated candidate omitted contradictory evidence: %#v", updated[0].EvidenceRefs)
	}
	stored, err := store.SkillCandidate(ctx, project, candidate.ID)
	if err != nil || stored.Status != SkillCandidateInvalidated {
		t.Fatalf("stored candidate state = %#v, %v", stored, err)
	}
}

func candidateFixtureKey(projectID, runtime, runID string) string {
	return projectID + "\x00" + runtime + "\x00" + runID
}
