package memorypipeline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/run"
)

type SkillCandidateReviewState string

const (
	SkillCandidatePendingReview   SkillCandidateReviewState = "pending_review"
	SkillCandidateApproved        SkillCandidateReviewState = "approved_for_manual_creation"
	SkillCandidateRejected        SkillCandidateReviewState = "rejected"
	SkillCandidateInvalidated     SkillCandidateReviewState = "invalidated_by_evidence"
	maxCandidateEvidenceRefs                                = 10000
	maxProcedureClaimsPerEvidence                           = 64
	maxProcedureClaimsPerProject                            = 1000
)

var procedureKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

// ProcedureClaim records an operator-attested procedure annotation linked to
// one immutable run reference. It remains untrusted and is only candidate
// input; approval and conversion are separate actions.
type ProcedureClaim struct {
	ID         string            `json:"id"`
	ProjectID  string            `json:"projectId"`
	EvidenceID string            `json:"evidenceId"`
	Procedure  ProcedureEvidence `json:"procedure"`
	Origin     string            `json:"origin"`
	CreatedAt  time.Time         `json:"createdAt"`
}

// SkillCandidate is a review item backed by independent, explicit evidence.
// Approval authorizes a person to consider manual authoring; it never creates
// or installs a Skill.
type SkillCandidate struct {
	ID                 string                    `json:"id"`
	ProjectID          string                    `json:"projectId"`
	Name               string                    `json:"name"`
	ProcedureKey       string                    `json:"procedureKey"`
	ProposedWorkflow   string                    `json:"proposedWorkflow"`
	Confidence         float64                   `json:"confidence"`
	ProjectSpecific    bool                      `json:"projectSpecific"`
	Status             SkillCandidateReviewState `json:"status"`
	EvidenceRefs       []string                  `json:"evidenceRefs"`
	EvidenceGeneration uint64                    `json:"evidenceGeneration"`
	CreatedAt          time.Time                 `json:"createdAt"`
	UpdatedAt          time.Time                 `json:"updatedAt"`
}

type procedureCandidateEvidence struct {
	key     string
	content string
	runs    map[string]map[string]struct{}
}

// DetectSkillCandidates reads every imported source reference through its
// provider, then records only procedures explicitly marked generalizable and
// successful/verified in at least two distinct completed native runs.
func (s *Store) DetectSkillCandidates(ctx context.Context, projectID string, provider RunEvidenceProvider) ([]SkillCandidate, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("%w: candidate detection requires an evidence provider", ErrInvalidEvidence)
	}
	refs, err := s.ListEvidence(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(refs) > maxCandidateEvidenceRefs {
		return nil, fmt.Errorf("%w: candidate detection exceeds %d imported evidence references", ErrInvalidRequest, maxCandidateEvidenceRefs)
	}
	grouped := make(map[string]*procedureCandidateEvidence)
	failedProcedureKeys := make(map[string]struct{})
	projectSpecificKeys := make(map[string]struct{})
	disqualifyingEvidence := make(map[string]map[string]struct{})
	markDisqualifyingEvidence := func(key, evidenceID string) {
		if disqualifyingEvidence[key] == nil {
			disqualifyingEvidence[key] = make(map[string]struct{})
		}
		disqualifyingEvidence[key][evidenceID] = struct{}{}
	}
	for _, importedRef := range refs {
		item, err := provider.Read(ctx, importedRef)
		if err != nil {
			return nil, err
		}
		ref, err := referenceFor(item)
		if err != nil || item.Ref.ProjectID != projectID || !sameReference(ref, item.Ref) || !sameReference(importedRef, ref) {
			return nil, fmt.Errorf("%w: candidate evidence must be an unchanged project run", ErrInvalidRequest)
		}
		claims, err := procedureClaimsForEvidence(ctx, s.db, projectID, ref.ID)
		if err != nil {
			return nil, err
		}
		procedures := append(append([]ProcedureEvidence(nil), item.Procedures...), claims...)
		for _, procedure := range normalizeProcedures(procedures) {
			if procedure.Key == "" {
				continue
			}
			if !procedure.Generalizable {
				projectSpecificKeys[procedure.Key] = struct{}{}
				markDisqualifyingEvidence(procedure.Key, ref.ID)
			}
			if procedure.Outcome == ProcedureOutcomeFailed {
				failedProcedureKeys[procedure.Key] = struct{}{}
				markDisqualifyingEvidence(procedure.Key, ref.ID)
				continue
			}
			if procedure.Content == "" {
				continue
			}
			groupKey := procedure.Key + "\x00" + procedure.Content
			candidate := grouped[groupKey]
			if candidate == nil {
				candidate = &procedureCandidateEvidence{key: procedure.Key, content: procedure.Content, runs: make(map[string]map[string]struct{})}
				grouped[groupKey] = candidate
			}
			nativeRun := item.Ref.Runtime + "\x00" + item.Ref.NativeRunID
			if item.Status != string(run.Completed) || !procedure.Generalizable || !supportedProcedureOutcome(procedure.Outcome) {
				continue
			}
			if candidate.runs[nativeRun] == nil {
				candidate.runs[nativeRun] = make(map[string]struct{})
			}
			candidate.runs[nativeRun][ref.ID] = struct{}{}
		}
	}

	eligible := make([]*procedureCandidateEvidence, 0, len(grouped))
	for _, candidate := range grouped {
		_, failed := failedProcedureKeys[candidate.key]
		_, projectSpecific := projectSpecificKeys[candidate.key]
		if len(candidate.runs) >= 2 && !failed && !projectSpecific {
			eligible = append(eligible, candidate)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].key == eligible[j].key {
			return eligible[i].content < eligible[j].content
		}
		return eligible[i].key < eligible[j].key
	})
	if len(eligible) == 0 && len(disqualifyingEvidence) == 0 {
		return []SkillCandidate{}, nil
	}
	now := time.Now().UTC()
	result := make([]SkillCandidate, 0, len(eligible))
	err = s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		var evidenceGeneration uint64
		if err := tx.QueryRowContext(ctx, `SELECT evidence_generation FROM projects WHERE project_id = ?`, projectID).Scan(&evidenceGeneration); err != nil {
			return err
		}
		ineligibleKeys := make([]string, 0, len(disqualifyingEvidence))
		for key := range disqualifyingEvidence {
			ineligibleKeys = append(ineligibleKeys, key)
		}
		sort.Strings(ineligibleKeys)
		for _, key := range ineligibleKeys {
			rows, err := tx.QueryContext(ctx, `SELECT id FROM skill_candidates WHERE project_id = ? AND procedure_key = ?`, projectID, key)
			if err != nil {
				return err
			}
			var candidateIDs []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return err
				}
				candidateIDs = append(candidateIDs, id)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			evidenceIDs := make([]string, 0, len(disqualifyingEvidence[key]))
			for evidenceID := range disqualifyingEvidence[key] {
				evidenceIDs = append(evidenceIDs, evidenceID)
			}
			sort.Strings(evidenceIDs)
			for _, candidateID := range candidateIDs {
				if _, err := tx.ExecContext(ctx, `UPDATE skill_candidates SET review_state = ?, evidence_generation = ?, updated_at = ?
					WHERE id = ? AND project_id = ? AND review_state IN (?, ?)`, SkillCandidateInvalidated, evidenceGeneration, formatTime(now), candidateID, projectID, SkillCandidatePendingReview, SkillCandidateApproved); err != nil {
					return err
				}
				for _, evidenceID := range evidenceIDs {
					if _, err := evidenceByID(ctx, tx, projectID, evidenceID); err != nil {
						return err
					}
					if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO skill_candidate_evidence(candidate_id, evidence_id) VALUES(?, ?)`, candidateID, evidenceID); err != nil {
						return err
					}
				}
				invalidated, err := skillCandidateByID(ctx, tx, projectID, candidateID)
				if err != nil {
					return err
				}
				result = append(result, invalidated)
			}
		}
		for _, candidate := range eligible {
			id := skillCandidateID(projectID, candidate.key, candidate.content)
			evidenceIDs := candidateEvidenceIDs(candidate)
			confidence := float64(len(candidate.runs)) / float64(len(candidate.runs)+1)
			var existingContent string
			err := tx.QueryRowContext(ctx, `SELECT content FROM skill_candidates WHERE project_id = ? AND procedure_key = ?`, projectID, candidate.key).Scan(&existingContent)
			switch {
			case err == nil && existingContent != candidate.content:
				return fmt.Errorf("%w: procedure key %q already has a different reviewed candidate", ErrConflict, candidate.key)
			case err != nil && err != sql.ErrNoRows:
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO skill_candidates(id, project_id, procedure_key, content, confidence, evidence_generation, review_state, created_at, updated_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(project_id, procedure_key) DO UPDATE SET confidence=excluded.confidence, evidence_generation=excluded.evidence_generation, updated_at=excluded.updated_at`,
				id, projectID, candidate.key, candidate.content, confidence, evidenceGeneration, SkillCandidatePendingReview, formatTime(now), formatTime(now))
			if err != nil {
				return err
			}
			for _, evidenceID := range evidenceIDs {
				if _, err := evidenceByID(ctx, tx, projectID, evidenceID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO skill_candidate_evidence(candidate_id, evidence_id) VALUES(?, ?)`, id, evidenceID); err != nil {
					return err
				}
			}
			stored, err := skillCandidateByID(ctx, tx, projectID, id)
			if err != nil {
				return err
			}
			result = append(result, stored)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ProcedureKey == result[j].ProcedureKey {
			return result[i].ID < result[j].ID
		}
		return result[i].ProcedureKey < result[j].ProcedureKey
	})
	return result, nil
}

// RecordProcedureClaim stores a confirmed, local annotation against an
// imported immutable source reference. New annotations invalidate previous
// candidate review freshness until detection runs again.
func (s *Store) RecordProcedureClaim(ctx context.Context, projectID, evidenceID string, procedure ProcedureEvidence, confirmed bool) (ProcedureClaim, error) {
	if !confirmed {
		return ProcedureClaim{}, memory.ErrNotConfirmed
	}
	if err := validateProjectID(projectID); err != nil {
		return ProcedureClaim{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return ProcedureClaim{}, err
	}
	procedure, err := NormalizeProcedureClaim(procedure)
	if err != nil {
		return ProcedureClaim{}, err
	}
	claimID := procedureClaimID(projectID, evidenceID, procedure)
	createdAt := time.Now().UTC()
	err = s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		if _, err := evidenceByID(ctx, tx, projectID, evidenceID); err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM procedure_claims WHERE id = ?`, claimID).Scan(&existing); err != nil {
			return err
		}
		if existing == 0 {
			var perEvidence, perProject int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM procedure_claims WHERE project_id = ? AND evidence_id = ?`, projectID, evidenceID).Scan(&perEvidence); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM procedure_claims WHERE project_id = ?`, projectID).Scan(&perProject); err != nil {
				return err
			}
			if perEvidence >= maxProcedureClaimsPerEvidence || perProject >= maxProcedureClaimsPerProject {
				return fmt.Errorf("%w: procedure claim limit reached (%d per evidence, %d per project)", ErrInvalidRequest, maxProcedureClaimsPerEvidence, maxProcedureClaimsPerProject)
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO procedure_claims(id, project_id, evidence_id, procedure_key, content, outcome, generalizable, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, claimID, projectID, evidenceID, procedure.Key, procedure.Content, procedure.Outcome, procedure.Generalizable, formatTime(createdAt))
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET evidence_generation = evidence_generation + 1 WHERE project_id = ?`, projectID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProcedureClaim{}, err
	}
	stored, err := procedureClaimByID(ctx, s.db, projectID, claimID)
	if err != nil {
		return ProcedureClaim{}, err
	}
	if stored.EvidenceID != evidenceID || stored.Procedure != procedure {
		return ProcedureClaim{}, fmt.Errorf("%w: procedure claim identity collided with different data", ErrConflict)
	}
	return stored, nil
}

// NormalizeProcedureClaim validates and canonicalizes a human-attested
// procedure annotation before it is shown for confirmation or persisted.
func NormalizeProcedureClaim(procedure ProcedureEvidence) (ProcedureEvidence, error) {
	procedure = normalizeProcedures([]ProcedureEvidence{procedure})[0]
	if !procedureKeyPattern.MatchString(procedure.Key) || strings.TrimSpace(procedure.Content) == "" || len(procedure.Content) > maxEvidenceFieldBytes || strings.IndexByte(procedure.Content, 0) >= 0 ||
		(procedure.Outcome != ProcedureOutcomeSucceeded && procedure.Outcome != ProcedureOutcomeVerified && procedure.Outcome != ProcedureOutcomeFailed) {
		return ProcedureEvidence{}, fmt.Errorf("%w: procedure claim needs a safe key, bounded workflow, and succeeded, verified, or failed outcome", ErrInvalidRequest)
	}
	if err := validateEvidenceBounds(RunEvidence{Procedures: []ProcedureEvidence{procedure}}); err != nil {
		return ProcedureEvidence{}, err
	}
	return procedure, nil
}

func procedureClaimsForEvidence(ctx context.Context, queryer sqlQueryer, projectID, evidenceID string) ([]ProcedureEvidence, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT procedure_key, content, outcome, generalizable FROM procedure_claims
		WHERE project_id = ? AND evidence_id = ? ORDER BY procedure_key, content, outcome, generalizable`, projectID, evidenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := make([]ProcedureEvidence, 0)
	for rows.Next() {
		var claim ProcedureEvidence
		if err := rows.Scan(&claim.Key, &claim.Content, &claim.Outcome, &claim.Generalizable); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func procedureClaimByID(ctx context.Context, queryer sqlQueryer, projectID, claimID string) (ProcedureClaim, error) {
	var claim ProcedureClaim
	var created string
	if err := queryer.QueryRowContext(ctx, `SELECT id, project_id, evidence_id, procedure_key, content, outcome, generalizable, created_at
		FROM procedure_claims WHERE project_id = ? AND id = ?`, projectID, claimID).Scan(
		&claim.ID, &claim.ProjectID, &claim.EvidenceID, &claim.Procedure.Key, &claim.Procedure.Content, &claim.Procedure.Outcome, &claim.Procedure.Generalizable, &created); err != nil {
		if err == sql.ErrNoRows {
			return ProcedureClaim{}, ErrRecordNotFound
		}
		return ProcedureClaim{}, err
	}
	claim.Origin = "operator"
	claim.CreatedAt = parseTime(created)
	return claim, nil
}

func procedureClaimID(projectID, evidenceID string, procedure ProcedureEvidence) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{projectID, evidenceID, procedure.Key, procedure.Content, procedure.Outcome, strconv.FormatBool(procedure.Generalizable)}, "\x00")))
	return "pc_" + hex.EncodeToString(sum[:16])
}

func (s *Store) ListSkillCandidates(ctx context.Context, projectID string) ([]SkillCandidate, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM skill_candidates WHERE project_id = ? ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]SkillCandidate, 0, len(ids))
	for _, id := range ids {
		candidate, err := skillCandidateByID(ctx, s.db, projectID, id)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (s *Store) SkillCandidate(ctx context.Context, projectID, candidateID string) (SkillCandidate, error) {
	if err := validateProjectID(projectID); err != nil {
		return SkillCandidate{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return SkillCandidate{}, err
	}
	return skillCandidateByID(ctx, s.db, projectID, candidateID)
}

// ReviewSkillCandidate requires a confirmed pending candidate. Approval is a
// terminal review decision for manual authoring, not a Skill Manager mutation.
func (s *Store) ReviewSkillCandidate(ctx context.Context, projectID, candidateID string, state SkillCandidateReviewState, confirmed bool) (SkillCandidate, error) {
	if err := validateProjectID(projectID); err != nil {
		return SkillCandidate{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return SkillCandidate{}, err
	}
	if !confirmed {
		return SkillCandidate{}, memory.ErrNotConfirmed
	}
	if state != SkillCandidateApproved && state != SkillCandidateRejected {
		return SkillCandidate{}, fmt.Errorf("%w: invalid Skill candidate review decision", ErrInvalidRequest)
	}
	now := time.Now().UTC()
	err := s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		var currentGeneration, candidateGeneration uint64
		if err := tx.QueryRowContext(ctx, `SELECT evidence_generation FROM projects WHERE project_id = ?`, projectID).Scan(&currentGeneration); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT evidence_generation FROM skill_candidates WHERE id = ? AND project_id = ?`, candidateID, projectID).Scan(&candidateGeneration); err != nil {
			if err == sql.ErrNoRows {
				return ErrRecordNotFound
			}
			return err
		}
		if candidateGeneration != currentGeneration {
			return fmt.Errorf("%w: candidate evidence changed; run candidate detection again", ErrConflict)
		}
		result, err := tx.ExecContext(ctx, `UPDATE skill_candidates SET review_state = ?, updated_at = ? WHERE id = ? AND project_id = ? AND review_state = ?`, state, formatTime(now), candidateID, projectID, SkillCandidatePendingReview)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return SkillCandidate{}, err
	}
	return skillCandidateByID(ctx, s.db, projectID, candidateID)
}

func skillCandidateByID(ctx context.Context, queryer sqlQueryer, projectID, candidateID string) (SkillCandidate, error) {
	var candidate SkillCandidate
	var created, updated string
	if err := queryer.QueryRowContext(ctx, `SELECT id, project_id, procedure_key, content, confidence, evidence_generation, review_state, created_at, updated_at FROM skill_candidates WHERE project_id = ? AND id = ?`, projectID, candidateID).
		Scan(&candidate.ID, &candidate.ProjectID, &candidate.ProcedureKey, &candidate.ProposedWorkflow, &candidate.Confidence, &candidate.EvidenceGeneration, &candidate.Status, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return SkillCandidate{}, ErrRecordNotFound
		}
		return SkillCandidate{}, err
	}
	candidate.Name = procedureDisplayName(candidate.ProcedureKey)
	candidate.ProjectSpecific = false
	candidate.CreatedAt, candidate.UpdatedAt = parseTime(created), parseTime(updated)
	rows, err := queryer.QueryContext(ctx, `SELECT evidence_id FROM skill_candidate_evidence WHERE candidate_id = ? ORDER BY evidence_id`, candidate.ID)
	if err != nil {
		return SkillCandidate{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var evidenceID string
		if err := rows.Scan(&evidenceID); err != nil {
			return SkillCandidate{}, err
		}
		candidate.EvidenceRefs = append(candidate.EvidenceRefs, evidenceID)
	}
	return candidate, rows.Err()
}

func candidateEvidenceIDs(candidate *procedureCandidateEvidence) []string {
	ids := make([]string, 0)
	for _, runEvidence := range candidate.runs {
		for evidenceID := range runEvidence {
			ids = append(ids, evidenceID)
		}
	}
	sort.Strings(ids)
	return ids
}

func supportedProcedureOutcome(outcome string) bool {
	return outcome == ProcedureOutcomeSucceeded || outcome == ProcedureOutcomeVerified
}

func procedureDisplayName(key string) string {
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(key))
	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		upper := []rune(strings.ToUpper(string(runes[0])))
		words[i] = string(append(upper, runes[1:]...))
	}
	return strings.Join(words, " ")
}

func skillCandidateID(projectID, key, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{projectID, key, content}, "\x00")))
	return "sc_" + hex.EncodeToString(sum[:16])
}
