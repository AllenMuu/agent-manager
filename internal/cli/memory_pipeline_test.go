package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memorypipeline"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

type cliMemoryPipelineEvidenceFixture []memorypipeline.RunEvidence

func (p cliMemoryPipelineEvidenceFixture) Discover(_ context.Context, projectID string) ([]memorypipeline.RunEvidenceRef, error) {
	refs := make([]memorypipeline.RunEvidenceRef, 0)
	for _, item := range p {
		if item.Ref.ProjectID == projectID {
			refs = append(refs, item.Ref)
		}
	}
	return refs, nil
}

func (p cliMemoryPipelineEvidenceFixture) Read(_ context.Context, ref memorypipeline.RunEvidenceRef) (memorypipeline.RunEvidence, error) {
	for _, item := range p {
		if item.Ref.ID == ref.ID {
			return item, nil
		}
	}
	return memorypipeline.RunEvidence{}, memorypipeline.ErrEvidenceNotFound
}

func TestMemoryPipelineRequiresExplicitInitializationAndDoesNotImportOnReads(t *testing.T) {
	configPath, _, project := structuredCLIConfig(t)
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes"); err != nil {
		t.Fatal(err)
	}
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(resolvedProject, ".agent-manager", "memory.db")
	for _, args := range [][]string{
		{"memory", "status", "--json"},
		{"memory", "search", "pipeline", "--project", project},
		{"roles", "context", "memory-uninitialized", "--project", project, "--role", "planner", "--agent", "codex"},
	} {
		if _, err := executeStructuredMemory(t, configPath, "", args...); err != nil {
			t.Fatalf("read command %v failed: %v", args, err)
		}
		if _, err := os.Stat(database); !os.IsNotExist(err) {
			t.Fatalf("read command %v initialized the derived store: %v", args, err)
		}
	}
	out, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "init", "--project", project)
	if err != nil {
		t.Fatal(err)
	}
	var initialized struct {
		ProjectID string `json:"projectId"`
		Path      string `json:"path"`
		FTS5      bool   `json:"fts5"`
	}
	if err := json.Unmarshal([]byte(out), &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.Path != database || !initialized.FTS5 || initialized.ProjectID == "" {
		t.Fatalf("pipeline init output = %s", out)
	}
	store, err := memorypipeline.OpenProjectStore(project)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	refs, err := store.ListEvidence(t.Context(), initialized.ProjectID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("init imported source runs: %#v, %v", refs, err)
	}
}

func TestRoleContextIncludesPipelineSummaryWithoutOpeningDerivedOrRawEvidence(t *testing.T) {
	configPath, _, project := structuredCLIConfig(t)
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes"); err != nil {
		t.Fatal(err)
	}
	registry, err := memory.OpenProjectRegistry(configPath + ".projects.json")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := registry.Lookup(project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := memorypipeline.InitializeProject(project)
	if err != nil {
		t.Fatal(err)
	}
	evidence := memorypipeline.RunEvidence{
		Ref:    memorypipeline.RunEvidenceRef{ProjectID: identity.ID, Runtime: "fixture", NativeRunID: "role-context-run", CreatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
		Status: "completed", SummaryText: "A compact, source-backed project discovery.", Discoveries: []string{"The project uses a fixed cache key namespace."},
	}
	ref, err := memorypipeline.ReferenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := store.ImportEvidence(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	extraction, err := memorypipeline.ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(t.Context(), extraction); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(project, ".agent-manager", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE procedure_claims"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configPath, "", "roles", "context", "memory-initialized", "--project", project, "--role", "planner", "--agent", "pi")
	if err != nil {
		t.Fatal(out, err)
	}
	var handoff struct {
		Context struct {
			MemorySummary *memorypipeline.ProjectSummary `json:"memorySummary"`
			MemoryDerived []memorypipeline.SearchResult  `json:"memoryDerived"`
			MemoryRecords []memory.Record                `json:"memoryRecords"`
		} `json:"context"`
	}
	if err := json.Unmarshal([]byte(out), &handoff); err != nil {
		t.Fatal(err)
	}
	if handoff.Context.MemorySummary == nil || handoff.Context.MemorySummary.TrustLevel != "untrusted" || !strings.Contains(handoff.Context.MemorySummary.Content, "compact, source-backed") || len(handoff.Context.MemoryDerived) != 0 || len(handoff.Context.MemoryRecords) != 0 {
		t.Fatalf("initial role context violated progressive disclosure: %s", out)
	}
	assertMemoryPipelineSchemaVersion(t, dbPath, 2)
	out, err = executeStructuredMemory(t, configPath, "", "roles", "context", "memory-initialized", "--project", project, "--role", "planner", "--agent", "pi", "--disable-memory-pipeline")
	if err != nil {
		t.Fatal(out, err)
	}
	handoff = struct {
		Context struct {
			MemorySummary *memorypipeline.ProjectSummary `json:"memorySummary"`
			MemoryDerived []memorypipeline.SearchResult  `json:"memoryDerived"`
			MemoryRecords []memory.Record                `json:"memoryRecords"`
		} `json:"context"`
	}{}
	if err := json.Unmarshal([]byte(out), &handoff); err != nil {
		t.Fatal(err)
	}
	if handoff.Context.MemorySummary != nil || len(handoff.Context.MemoryDerived) != 0 {
		t.Fatalf("disabled pipeline still injected derived context: %s", out)
	}
	assertMemoryPipelineSchemaVersion(t, dbPath, 2)
	store, err = memorypipeline.OpenProjectStore(project)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	refs, err := store.ListEvidence(t.Context(), identity.ID)
	if err != nil || len(refs) != 1 || refs[0].ID != ref.ID {
		t.Fatalf("role context changed evidence imports: %#v, %v", refs, err)
	}
}

func assertMemoryPipelineSchemaVersion(t *testing.T, path string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("PRAGMA user_version").Scan(&got); err != nil || got != want {
		t.Fatalf("memory pipeline schema version = %d, %v, want %d", got, err, want)
	}
}

func TestMemoryPipelineImportsAndReopensExplicitCodexAndPiEvidence(t *testing.T) {
	configPath, _, project := structuredCLIConfig(t)
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "init", "--project", project); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(t.TempDir(), "sessions")
	piDir := filepath.Join(t.TempDir(), "sessions")
	created := "2026-10-15T12:00:00Z"
	writeCLINativeFixture(t, filepath.Join(codexDir, "2026", "10", "15", "rollout-cli-codex.jsonl"),
		`{"type":"session_meta","timestamp":"`+created+`","payload":{"session_id":"cli-codex","timestamp":"2026-10-15T12-00-00","cwd":"`+project+`"}}`,
		`{"type":"response_item","timestamp":"2026-10-15T12:01:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"private prompt secret"}]}}`,
		`{"type":"event_msg","timestamp":"2026-10-15T12:02:00Z","payload":{"type":"task_complete"}}`,
	)
	writeCLINativeFixture(t, filepath.Join(piDir, "--project--", "cli-pi.jsonl"),
		`{"type":"session","version":3,"id":"cli-pi","timestamp":"`+created+`","cwd":"`+project+`"}`,
		`{"type":"message","id":"user0001","parentId":null,"timestamp":"2026-10-15T12:01:00Z","message":{"role":"user","content":[{"type":"text","text":"private Pi prompt secret"}]}}`,
		`{"type":"message","id":"agent001","parentId":"user0001","timestamp":"2026-10-15T12:02:00Z","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"private answer secret"}]}}`,
	)
	args := []string{"--project", project, "--codex-sessions-dir", codexDir, "--pi-sessions-dir", piDir}
	out, err := executeStructuredMemory(t, configPath, "", append([]string{"memory", "pipeline", "import"}, args...)...)
	var refs []memorypipeline.RunEvidenceRef
	if err != nil || json.Unmarshal([]byte(out), &refs) != nil || len(refs) != 2 {
		t.Fatalf("native evidence import: %s %v", out, err)
	}
	for _, ref := range refs {
		if _, err := executeStructuredMemory(t, configPath, "", append([]string{"memory", "pipeline", "extract", ref.ID}, args...)...); err != nil {
			t.Fatalf("extract %s: %v", ref.Runtime, err)
		}
		out, err = executeStructuredMemory(t, configPath, "", append([]string{"memory", "pipeline", "evidence", ref.ID}, args...)...)
		if err != nil || strings.Contains(out, "private prompt secret") || strings.Contains(out, "private answer secret") {
			t.Fatalf("evidence read copied transcript content: %s %v", out, err)
		}
	}
	for _, ref := range refs {
		claimArgs := []string{"memory", "pipeline", "candidates", "claim", ref.ID, "--key", "validated-cache-workflow", "--workflow", "Validate cache keys include project identity.", "--outcome", "verified", "--generalizable=true", "--yes"}
		out, err := executeStructuredMemory(t, configPath, "", append(claimArgs, args...)...)
		if err != nil || !strings.Contains(out, `"origin":"operator"`) {
			t.Fatalf("explicit procedure claim for %s: %s %v", ref.Runtime, out, err)
		}
	}
	out, err = executeStructuredMemory(t, configPath, "", append([]string{"memory", "pipeline", "candidates", "detect"}, args...)...)
	var candidates []memorypipeline.SkillCandidate
	if err != nil || json.Unmarshal([]byte(out), &candidates) != nil || len(candidates) != 1 || len(candidates[0].EvidenceRefs) != 2 || candidates[0].Status != memorypipeline.SkillCandidatePendingReview {
		t.Fatalf("candidate detection from explicit Codex/Pi procedure claims: %s %v", out, err)
	}
	piPath := filepath.Join(piDir, "--project--", "cli-pi.jsonl")
	contents, err := os.ReadFile(piPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, []byte(`{"type":"custom","id":"changed"}`+"\n")...)
	if err := os.WriteFile(piPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	var piRef memorypipeline.RunEvidenceRef
	for _, ref := range refs {
		if ref.Runtime == "pi" {
			piRef = ref
		}
	}
	if _, err := executeStructuredMemory(t, configPath, "", append([]string{"memory", "pipeline", "evidence", piRef.ID}, args...)...); !errors.Is(err, memorypipeline.ErrEvidenceChanged) {
		t.Fatalf("changed source was not rejected: %v", err)
	}
}

func writeCLINativeFixture(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryPipelineProposalCreateCommandAddsReviewableProposal(t *testing.T) {
	configPath, _, project := structuredCLIConfig(t)
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes"); err != nil {
		t.Fatal(err)
	}
	registry, err := memory.OpenProjectRegistry(configPath + ".projects.json")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := registry.Lookup(project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := memorypipeline.InitializeProject(project)
	if err != nil {
		t.Fatal(err)
	}
	evidence := memorypipeline.RunEvidence{
		Ref:    memorypipeline.RunEvidenceRef{ProjectID: identity.ID, Runtime: "fixture", NativeRunID: "proposal-cli-run", CreatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
		Status: "completed", Discoveries: []string{"An explicitly selected fact for review."},
	}
	ref, err := memorypipeline.ReferenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := store.ImportEvidence(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	extraction, err := memorypipeline.ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(t.Context(), extraction); err != nil {
		t.Fatal(err)
	}
	derived, err := store.Search(t.Context(), identity.ID, "explicitly selected fact", 10)
	if err != nil || len(derived) != 1 {
		t.Fatalf("derived selection = %#v, %v", derived, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "proposal", "create", derived[0].ID, "--project", project)
	if err != nil || !strings.Contains(out, string(memorypipeline.ProposalPending)) {
		t.Fatalf("proposal create: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "pipeline", "proposal", "list", "--project", project)
	var proposals []memorypipeline.Proposal
	if err != nil || json.Unmarshal([]byte(out), &proposals) != nil || len(proposals) != 1 || proposals[0].DerivedID != derived[0].ID {
		t.Fatalf("proposal listing = %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "pipeline", "proposal", "accept", proposals[0].ID, "--project", project, "--yes")
	if err != nil || !strings.Contains(out, string(memorypipeline.ProposalAccepted)) {
		t.Fatalf("proposal acceptance: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "search", "explicitly selected fact", "--project", project)
	var canonical []memory.Record
	wantEvidence := evidence.Ref.SourceRef + "#" + evidence.Ref.ContentHash
	if err != nil || json.Unmarshal([]byte(out), &canonical) != nil || len(canonical) != 1 || canonical[0].Content != derived[0].Content || len(canonical[0].Evidence) != 1 || canonical[0].Evidence[0] != wantEvidence {
		t.Fatalf("accepted proposal did not preserve canonical provenance: %s %v", out, err)
	}
	if _, err := executeStructuredMemory(t, configPath, "", "task", "init", "--project", project, "--id", "cross-runtime-memory", "--summary", "An explicitly selected fact for review"); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"codex", "pi"} {
		out, err = executeStructuredMemory(t, configPath, "", "roles", "context", "cross-runtime-memory", "--project", project, "--role", "planner", "--agent", agent)
		if err != nil {
			t.Fatalf("%s canonical context: %s %v", agent, out, err)
		}
		var handoff struct {
			Context struct {
				MemoryRecords []memory.Record `json:"memoryRecords"`
			} `json:"context"`
		}
		if err := json.Unmarshal([]byte(out), &handoff); err != nil || len(handoff.Context.MemoryRecords) != 1 || handoff.Context.MemoryRecords[0].ID != canonical[0].ID {
			t.Fatalf("%s did not observe the same canonical record: %s %v", agent, out, err)
		}
	}
}

func TestMemoryPipelineCandidateCLIRequiresReviewAndDoesNotTouchSkillOrCanonicalStores(t *testing.T) {
	configPath, canonicalRoot, project := structuredCLIConfig(t)
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes"); err != nil {
		t.Fatal(err)
	}
	registry, err := memory.OpenProjectRegistry(configPath + ".projects.json")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := registry.Lookup(project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := memorypipeline.InitializeProject(project)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	baseTime := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	evidence := make([]memorypipeline.RunEvidence, 0, 2)
	for _, runID := range []string{"candidate-run-a", "candidate-run-b"} {
		item := memorypipeline.RunEvidence{Ref: memorypipeline.RunEvidenceRef{ProjectID: identity.ID, Runtime: "fixture", NativeRunID: runID, CreatedAt: baseTime}, Status: "completed", UpdatedAt: baseTime.Add(time.Minute), Procedures: []memorypipeline.ProcedureEvidence{{Key: "cache-key-validation", Content: "Validate cache keys include project identity.", Outcome: memorypipeline.ProcedureOutcomeVerified, Generalizable: true}}}
		ref, err := memorypipeline.ReferenceFor(item)
		if err != nil {
			t.Fatal(err)
		}
		item.Ref = ref
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
		evidence = append(evidence, item)
	}
	candidates, err := store.DetectSkillCandidates(ctx, identity.ID, cliMemoryPipelineEvidenceFixture(evidence))
	if err != nil || len(candidates) != 1 {
		t.Fatalf("prepare candidate: %#v %v", candidates, err)
	}
	candidateID := candidates[0].ID
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "candidates", "list", "--project", project)
	if err != nil {
		t.Fatal(err)
	}
	var listed []memorypipeline.SkillCandidate
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed) != 1 || listed[0].ID != candidateID || listed[0].Name == "" || listed[0].ProposedWorkflow == "" || listed[0].ProjectSpecific || len(listed[0].EvidenceRefs) != 2 {
		t.Fatalf("candidate listing omitted review metadata: %s %v", out, err)
	}
	skillLibrary := filepath.Join(t.TempDir(), "skills")
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "candidates", "convert", candidateID, "--identifier", "cache-key-validation", "--library", skillLibrary, "--version", "0.1.0", "--yes", "--project", project); err == nil {
		t.Fatal("unreviewed Skill candidate was converted")
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "pipeline", "candidates", "review", candidateID, "--decision", "approve", "--project", project, "--yes")
	if err != nil || !strings.Contains(out, string(memorypipeline.SkillCandidateApproved)) {
		t.Fatalf("candidate review: %s %v", out, err)
	}
	store, err = memorypipeline.OpenProjectStore(project)
	if err != nil {
		t.Fatal(err)
	}
	lateEvidence := memorypipeline.RunEvidence{Ref: memorypipeline.RunEvidenceRef{ProjectID: identity.ID, Runtime: "fixture", NativeRunID: "candidate-run-c", CreatedAt: baseTime}, Status: "completed", UpdatedAt: baseTime.Add(time.Minute), Procedures: []memorypipeline.ProcedureEvidence{{Key: "cache-key-validation", Content: "Validate cache keys include project identity.", Outcome: memorypipeline.ProcedureOutcomeSucceeded, Generalizable: true}}}
	lateRef, err := memorypipeline.ReferenceFor(lateEvidence)
	if err != nil {
		t.Fatal(err)
	}
	lateEvidence.Ref = lateRef
	if _, err := store.ImportEvidence(ctx, lateRef); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := executeStructuredMemory(t, configPath, "", "memory", "pipeline", "candidates", "convert", candidateID, "--identifier", "cache-key-validation", "--library", skillLibrary, "--version", "0.1.0", "--yes", "--project", project); err == nil || !strings.Contains(err.Error(), "run candidate detection again") {
		t.Fatalf("candidate with newly imported evidence was converted without re-detection: %v", err)
	}
	evidence = append(evidence, lateEvidence)
	store, err = memorypipeline.OpenProjectStore(project)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.DetectSkillCandidates(ctx, identity.ID, cliMemoryPipelineEvidenceFixture(evidence))
	if err != nil || len(refreshed) != 1 || refreshed[0].ID != candidateID || refreshed[0].Status != memorypipeline.SkillCandidateApproved {
		t.Fatalf("refresh candidate evidence after import: %#v %v", refreshed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "pipeline", "candidates", "convert", candidateID, "--identifier", "cache-key-validation", "--library", skillLibrary, "--version", "0.1.0", "--yes", "--project", project)
	if err != nil || !strings.Contains(out, "cache-key-validation") {
		t.Fatalf("candidate conversion: %s %v", out, err)
	}
	repository, err := skillrepo.Open(skillLibrary)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := repository.Resolve("cache-key-validation", "")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(entry.Path, "SKILL.md"))
	if err != nil || !strings.Contains(string(generated), "explicitly converted") || !strings.Contains(string(generated), "Validate cache keys") {
		t.Fatalf("converted Skill lost candidate provenance or workflow: %q %v", generated, err)
	}
	if entries, err := os.ReadDir(canonicalRoot); err != nil || len(entries) != 0 {
		t.Fatalf("candidate review wrote canonical Memory: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("candidate review created a Skill repository entry: %v", err)
	}
}
