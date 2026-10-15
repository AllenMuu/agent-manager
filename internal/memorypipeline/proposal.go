package memorypipeline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

var ErrPublicationUncertain = errors.New("memory proposal publication outcome is uncertain")

type ProposalStatus string

const (
	ProposalPending           ProposalStatus = "pending"
	ProposalPublishing        ProposalStatus = "publishing"
	ProposalReconcileRequired ProposalStatus = "reconcile_required"
	ProposalAccepted          ProposalStatus = "accepted"
	ProposalRejected          ProposalStatus = "rejected"
)

type Proposal struct {
	ID                string               `json:"id"`
	ProjectID         string               `json:"projectId"`
	DerivedID         string               `json:"derivedId"`
	Type              memory.KnowledgeType `json:"type"`
	Content           string               `json:"content"`
	Status            ProposalStatus       `json:"status"`
	OperationID       string               `json:"operationId"`
	CanonicalRecordID string               `json:"canonicalRecordId,omitempty"`
	EvidenceIDs       []string             `json:"evidenceIds"`
	CanonicalEvidence []string             `json:"canonicalEvidence"`
	CreatedAt         time.Time            `json:"createdAt"`
	UpdatedAt         time.Time            `json:"updatedAt"`
}

// Propose writes a provenance-linked review item only to the derived database.
func (s *Store) Propose(ctx context.Context, projectID, derivedID string) (Proposal, error) {
	if err := validateProjectID(projectID); err != nil {
		return Proposal{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return Proposal{}, err
	}
	if derivedID == "" {
		return Proposal{}, ErrInvalidRequest
	}
	proposalID := proposalIDFor(projectID, derivedID)
	now := time.Now().UTC()
	err := s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		item, err := derivedByID(ctx, tx, projectID, derivedID)
		if err != nil {
			return err
		}
		if item.State != DerivedActive || item.Verification != VerificationUnverified {
			return fmt.Errorf("%w: only active unverified derived memories can be proposed", ErrInvalidRequest)
		}
		_, err = ensureProposalTx(ctx, tx, item, now)
		return err
	})
	if err != nil {
		return Proposal{}, err
	}
	return s.Proposal(ctx, projectID, proposalID)
}

func eligibleForAutomaticProposal(item DerivedMemory) bool {
	switch item.Type {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeProjectContext:
		return true
	default:
		return false
	}
}

func ensureProposalTx(ctx context.Context, tx *sql.Tx, item DerivedMemory, now time.Time) (string, error) {
	proposalID := proposalIDFor(item.ProjectID, item.ID)
	operationID := "mpublish_" + proposalID
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO proposals(id, project_id, derived_id, knowledge_type, content, status, operation_id, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, proposalID, item.ProjectID, item.ID, item.Type, item.Content, ProposalPending, operationID, formatTime(now), formatTime(now)); err != nil {
		return "", err
	}
	var storedID, status string
	if err := tx.QueryRowContext(ctx, `SELECT id, status FROM proposals WHERE project_id = ? AND derived_id = ?`, item.ProjectID, item.ID).Scan(&storedID, &status); err != nil {
		return "", err
	}
	if storedID != proposalID {
		return "", fmt.Errorf("%w: proposal identity does not match its derived memory", ErrConflict)
	}
	if status != string(ProposalPending) {
		return storedID, nil
	}
	for _, evidenceID := range item.EvidenceIDs {
		if _, err := evidenceByID(ctx, tx, item.ProjectID, evidenceID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO proposal_evidence(proposal_id, evidence_id) VALUES(?, ?)`, storedID, evidenceID); err != nil {
			return "", err
		}
	}
	return storedID, nil
}

func (s *Store) Proposal(ctx context.Context, projectID, proposalID string) (Proposal, error) {
	if err := validateProjectID(projectID); err != nil {
		return Proposal{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return Proposal{}, err
	}
	return proposalByID(ctx, s.db, projectID, proposalID)
}

func (s *Store) ListProposals(ctx context.Context, projectID string) ([]Proposal, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM proposals WHERE project_id = ? ORDER BY created_at, id`, projectID)
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
	result := make([]Proposal, 0, len(ids))
	for _, id := range ids {
		proposal, err := proposalByID(ctx, s.db, projectID, id)
		if err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	return result, nil
}

// ListOutstandingProposals returns only proposals that still need operator
// attention. It is intended for local diagnostics and never changes state.
func (s *Store) ListOutstandingProposals(ctx context.Context) ([]Proposal, error) {
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, id FROM proposals WHERE status IN (?, ?, ?) ORDER BY project_id, created_at, id`, ProposalPending, ProposalPublishing, ProposalReconcileRequired)
	if err != nil {
		return nil, err
	}
	type key struct{ projectID, id string }
	keys := make([]key, 0)
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.projectID, &item.id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]Proposal, 0, len(keys))
	for _, item := range keys {
		proposal, err := proposalByID(ctx, s.db, item.projectID, item.id)
		if err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	return result, nil
}

func (s *Store) RejectProposal(ctx context.Context, projectID, proposalID string, confirmed bool) (Proposal, error) {
	if !confirmed {
		return Proposal{}, memory.ErrNotConfirmed
	}
	proposal, err := s.Proposal(ctx, projectID, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if proposal.Status == ProposalRejected {
		return proposal, nil
	}
	if proposal.Status != ProposalPending {
		return Proposal{}, fmt.Errorf("%w: proposal is already in %s state", ErrConflict, proposal.Status)
	}
	err = s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE proposals SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status = ?`, ProposalRejected, formatTime(time.Now().UTC()), projectID, proposalID, ProposalPending)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return Proposal{}, err
	}
	return s.Proposal(ctx, projectID, proposalID)
}

// ProposalAcceptancePreview carries a human-reviewable Memory Gateway plan.
// Its private confirmation token binds the later commit to this exact preview.
type ProposalAcceptancePreview struct {
	Proposal Proposal            `json:"proposal"`
	Plan     memory.MutationPlan `json:"memoryPlan"`
	store    *Store
	gateway  *memory.Gateway
	preview  memory.Preview
	owner    memory.Owner
	source   string
}

func (s *Store) PreviewProposalAcceptance(ctx context.Context, projectID, proposalID string, gateway *memory.Gateway, owner memory.Owner) (ProposalAcceptancePreview, error) {
	if gateway == nil || !exactProjectOwner(projectID, owner) {
		return ProposalAcceptancePreview{}, memory.ErrOwnershipDenied
	}
	if err := pipelineContext(ctx); err != nil {
		return ProposalAcceptancePreview{}, err
	}
	proposal, err := s.Proposal(ctx, projectID, proposalID)
	if err != nil {
		return ProposalAcceptancePreview{}, err
	}
	if proposal.Status == ProposalAccepted {
		return ProposalAcceptancePreview{}, ErrConflict
	}
	if proposal.Status == ProposalPublishing || proposal.Status == ProposalReconcileRequired {
		return ProposalAcceptancePreview{}, ErrPublicationUncertain
	}
	if proposal.Status != ProposalPending {
		return ProposalAcceptancePreview{}, fmt.Errorf("%w: proposal is already in %s state", ErrConflict, proposal.Status)
	}
	if len(proposal.CanonicalEvidence) == 0 {
		return ProposalAcceptancePreview{}, fmt.Errorf("%w: proposal has no source evidence", ErrInvalidRequest)
	}
	source := "experience-proposal-" + proposal.ID
	mutation := memory.Mutation{
		Operation:   memory.OperationAdd,
		Record:      memory.NewRecord{Owner: owner, Type: proposal.Type, Content: proposal.Content, Source: source, Evidence: append([]string(nil), proposal.CanonicalEvidence...), Layer: memory.LayerAtomic},
		OperationID: proposal.OperationID,
	}
	preview, err := gateway.Preview(ctx, mutation)
	if err != nil {
		return ProposalAcceptancePreview{}, err
	}
	return ProposalAcceptancePreview{Proposal: proposal, Plan: preview.Plan(), store: s, gateway: gateway, preview: preview, owner: owner, source: source}, nil
}

// ConfirmProposalAcceptance publishes only the exact plan shown to the
// operator. The publishing state is durable before the single Gateway call.
func (s *Store) ConfirmProposalAcceptance(ctx context.Context, prepared ProposalAcceptancePreview, confirmed bool) (Proposal, error) {
	if !confirmed {
		return Proposal{}, memory.ErrNotConfirmed
	}
	if prepared.store != s || prepared.gateway == nil || prepared.owner.ProjectID != prepared.Proposal.ProjectID || !exactProjectOwner(prepared.Proposal.ProjectID, prepared.owner) {
		return Proposal{}, memory.ErrNotConfirmed
	}
	if !reflect.DeepEqual(prepared.Plan, prepared.preview.Plan()) {
		return Proposal{}, memory.ErrNotConfirmed
	}
	if err := pipelineContext(ctx); err != nil {
		return Proposal{}, err
	}
	projectID, proposalID := prepared.Proposal.ProjectID, prepared.Proposal.ID
	current, err := s.Proposal(ctx, projectID, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if current.Status != ProposalPending || current.OperationID != prepared.Proposal.OperationID || current.Content != prepared.Proposal.Content || current.Type != prepared.Proposal.Type || !sameEvidenceStrings(current.CanonicalEvidence, prepared.Proposal.CanonicalEvidence) {
		return Proposal{}, ErrConflict
	}
	if err := s.beginPublication(ctx, projectID, proposalID, prepared.Proposal.OperationID); err != nil {
		return Proposal{}, err
	}
	records, commitErr := prepared.gateway.Commit(ctx, prepared.preview, memory.Confirmation{Confirmed: true, IntentID: prepared.preview.IntentID(), Owner: prepared.owner, Source: prepared.source})
	if commitErr != nil {
		stateErr := s.setProposalReconciliation(ctx, projectID, proposalID)
		current, readErr := s.Proposal(ctx, projectID, proposalID)
		if stateErr != nil || readErr != nil {
			return current, errors.Join(ErrPublicationUncertain, commitErr, stateErr, readErr)
		}
		return current, fmt.Errorf("%w: %w", ErrPublicationUncertain, commitErr)
	}
	if len(records) != 1 || records[0].Owner != prepared.owner || records[0].Source != prepared.source || !sameEvidenceStrings(records[0].Evidence, prepared.Proposal.CanonicalEvidence) {
		_ = s.setProposalReconciliation(ctx, projectID, proposalID)
		return s.Proposal(ctx, projectID, proposalID)
	}
	if err := s.markProposalAccepted(ctx, projectID, proposalID, string(records[0].ID)); err != nil {
		return Proposal{}, fmt.Errorf("%w: canonical write succeeded but local proposal receipt could not be stored: %v", ErrPublicationUncertain, err)
	}
	return s.Proposal(ctx, projectID, proposalID)
}

// ReconcileProposal checks canonical Memory for the exact proposal source and
// evidence. It never calls Gateway.Commit or retries a write.
func (s *Store) ReconcileProposal(ctx context.Context, projectID, proposalID string, gateway *memory.Gateway, owner memory.Owner) (Proposal, error) {
	if gateway == nil || !exactProjectOwner(projectID, owner) {
		return Proposal{}, memory.ErrOwnershipDenied
	}
	if err := pipelineContext(ctx); err != nil {
		return Proposal{}, err
	}
	proposal, err := s.Proposal(ctx, projectID, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if proposal.Status == ProposalAccepted || proposal.Status == ProposalRejected {
		return proposal, nil
	}
	if proposal.Status != ProposalPublishing && proposal.Status != ProposalReconcileRequired {
		return Proposal{}, fmt.Errorf("%w: proposal has no publication to reconcile", ErrInvalidRequest)
	}
	source := "experience-proposal-" + proposal.ID
	records, err := gateway.Search(ctx, memory.SearchRequest{Owner: owner, Text: proposal.Content, Type: proposal.Type})
	if err != nil {
		return proposal, err
	}
	var match *memory.Record
	for i := range records {
		if records[i].Source != source || !sameEvidenceStrings(records[i].Evidence, proposal.CanonicalEvidence) || records[i].Type != proposal.Type || records[i].Content != proposal.Content {
			continue
		}
		if match != nil {
			_ = s.setProposalReconciliation(ctx, projectID, proposalID)
			return s.Proposal(ctx, projectID, proposalID)
		}
		copy := records[i]
		match = &copy
	}
	if match == nil {
		_ = s.setProposalReconciliation(ctx, projectID, proposalID)
		return s.Proposal(ctx, projectID, proposalID)
	}
	if err := s.markProposalAccepted(ctx, projectID, proposalID, string(match.ID)); err != nil {
		return Proposal{}, fmt.Errorf("%w: canonical receipt found but local proposal state could not be updated: %v", ErrPublicationUncertain, err)
	}
	return s.Proposal(ctx, projectID, proposalID)
}

func (s *Store) beginPublication(ctx context.Context, projectID, proposalID, operationID string) error {
	return s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE proposals SET status = ?, operation_id = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status = ? AND operation_id = ?`, ProposalPublishing, operationID, formatTime(time.Now().UTC()), projectID, proposalID, ProposalPending, operationID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrConflict
		}
		return nil
	})
}

func (s *Store) setProposalReconciliation(ctx context.Context, projectID, proposalID string) error {
	return s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE proposals SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status IN (?, ?)`, ProposalReconcileRequired, formatTime(time.Now().UTC()), projectID, proposalID, ProposalPublishing, ProposalReconcileRequired)
		return err
	})
}

func (s *Store) markProposalAccepted(ctx context.Context, projectID, proposalID, canonicalID string) error {
	if canonicalID == "" {
		return ErrInvalidRequest
	}
	return s.writeTx(ctx, projectID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE proposals SET status = ?, canonical_record_id = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status IN (?, ?)`, ProposalAccepted, canonicalID, formatTime(time.Now().UTC()), projectID, proposalID, ProposalPublishing, ProposalReconcileRequired)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrConflict
		}
		return nil
	})
}

func proposalByID(ctx context.Context, queryer sqlQueryer, projectID, proposalID string) (Proposal, error) {
	result := Proposal{}
	var created, updated string
	if err := queryer.QueryRowContext(ctx, `SELECT id, project_id, COALESCE(derived_id, ''), knowledge_type, content, status, operation_id, COALESCE(canonical_record_id, ''), created_at, updated_at
		FROM proposals WHERE project_id = ? AND id = ?`, projectID, proposalID).Scan(
		&result.ID, &result.ProjectID, &result.DerivedID, &result.Type, &result.Content, &result.Status, &result.OperationID, &result.CanonicalRecordID, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Proposal{}, ErrRecordNotFound
		}
		return Proposal{}, err
	}
	result.CreatedAt, result.UpdatedAt = parseTime(created), parseTime(updated)
	rows, err := queryer.QueryContext(ctx, `SELECT e.id, e.source_ref, e.content_hash FROM proposal_evidence pe JOIN evidence_refs e ON e.id = pe.evidence_id
		WHERE pe.proposal_id = ? AND e.project_id = ? ORDER BY e.id`, proposalID, projectID)
	if err != nil {
		return Proposal{}, err
	}
	for rows.Next() {
		var id, sourceRef, contentHash string
		if err := rows.Scan(&id, &sourceRef, &contentHash); err != nil {
			_ = rows.Close()
			return Proposal{}, err
		}
		result.EvidenceIDs = append(result.EvidenceIDs, id)
		result.CanonicalEvidence = append(result.CanonicalEvidence, sourceRef+"#"+contentHash)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Proposal{}, err
	}
	if err := rows.Close(); err != nil {
		return Proposal{}, err
	}
	sort.Strings(result.EvidenceIDs)
	sort.Strings(result.CanonicalEvidence)
	return result, nil
}

func proposalIDFor(projectID, derivedID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{projectID, derivedID}, "\x00")))
	return "prop_" + hex.EncodeToString(sum[:16])
}

func exactProjectOwner(projectID string, owner memory.Owner) bool {
	return owner.Kind == memory.OwnerProject && owner.ProjectID == projectID && owner.UserID == "" && owner.AgentID == "" && owner.SessionID == ""
}

func sameEvidenceStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
