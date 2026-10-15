package memorypipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

var (
	ErrRecordNotFound = errors.New("memory pipeline record not found")
	ErrConflict       = errors.New("memory pipeline conflict")
	ErrInvalidRequest = errors.New("invalid memory pipeline request")
)

const (
	maxSearchLimit     = 100
	defaultSearchLimit = 20
	maxSearchTerms     = 16
	maxProjectSummary  = 8 * 1024
	maxSummaryEntries  = 12
	maxActiveMemories  = 500
)

var (
	projectIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	evidenceIDPattern  = regexp.MustCompile(`^ev_[a-f0-9]{32}$`)
	contentHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	runtimePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)
	nativeRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)
)

type ProjectSummary struct {
	ProjectID   string    `json:"projectId"`
	Content     string    `json:"content"`
	TrustLevel  string    `json:"trustLevel"`
	Generation  uint64    `json:"generation"`
	UpdatedAt   time.Time `json:"updatedAt"`
	EvidenceIDs []string  `json:"evidenceIds"`
}

type SearchResult struct {
	DerivedMemory
	Relevance float64 `json:"relevance"`
}

type sqlQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ImportEvidence stores only immutable source metadata. Raw run records remain
// in their native run store and are resolved through RunEvidenceProvider.
func (s *Store) ImportEvidence(ctx context.Context, ref RunEvidenceRef) (RunEvidenceRef, error) {
	if err := validateEvidenceRef(ref); err != nil {
		return RunEvidenceRef{}, err
	}
	importedAt := time.Now().UTC()
	err := s.writeTx(ctx, ref.ProjectID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO evidence_refs(id, project_id, runtime, native_run_id, source_ref, created_at, content_hash, import_status, imported_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(project_id, runtime, native_run_id, content_hash) DO NOTHING`,
			ref.ID, ref.ProjectID, ref.Runtime, ref.NativeRunID, ref.SourceRef, formatTime(ref.CreatedAt), ref.ContentHash, ref.Status, formatTime(importedAt))
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET evidence_generation = evidence_generation + 1 WHERE project_id = ?`, ref.ProjectID); err != nil {
				return err
			}
		}
		stored, err := evidenceByID(ctx, tx, ref.ProjectID, ref.ID)
		if err != nil {
			return err
		}
		if !sameReference(stored, ref) {
			return fmt.Errorf("%w: evidence identity collides with different source metadata", ErrConflict)
		}
		return nil
	})
	if err != nil {
		return RunEvidenceRef{}, err
	}
	return ref, nil
}

// EvidenceGeneration returns the generation of candidate inputs in a project.
// Imports and new operator procedure claims advance it so prior review cannot
// silently apply to changed evidence.
func (s *Store) EvidenceGeneration(ctx context.Context, projectID string) (uint64, error) {
	if err := validateProjectID(projectID); err != nil {
		return 0, err
	}
	if err := pipelineContext(ctx); err != nil {
		return 0, err
	}
	var generation uint64
	err := s.db.QueryRowContext(ctx, `SELECT evidence_generation FROM projects WHERE project_id = ?`, projectID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return generation, err
}

// Evidence performs an exact project-scoped lookup. The provider must still
// verify that the native source matches the immutable content hash.
func (s *Store) Evidence(ctx context.Context, projectID, evidenceID string) (RunEvidenceRef, error) {
	if err := validateProjectID(projectID); err != nil {
		return RunEvidenceRef{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return RunEvidenceRef{}, err
	}
	return evidenceByID(ctx, s.db, projectID, evidenceID)
}

func (s *Store) ListEvidence(ctx context.Context, projectID string) ([]RunEvidenceRef, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, runtime, native_run_id, source_ref, created_at, content_hash, import_status
		FROM evidence_refs WHERE project_id = ? ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RunEvidenceRef, 0)
	for rows.Next() {
		var ref RunEvidenceRef
		var created, status string
		if err := rows.Scan(&ref.ID, &ref.ProjectID, &ref.Runtime, &ref.NativeRunID, &ref.SourceRef, &created, &ref.ContentHash, &status); err != nil {
			return nil, err
		}
		ref.CreatedAt, ref.Status = parseTime(created), ImportStatus(status)
		result = append(result, ref)
	}
	return result, rows.Err()
}

// Consolidate atomically imports an extracted summary, deduplicates memories,
// attaches all evidence lineage, applies explicit supersessions, refreshes the
// bounded project summary and relies on SQLite triggers to maintain FTS.
func (s *Store) Consolidate(ctx context.Context, extraction Extraction) error {
	if err := validateExtraction(extraction); err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	return s.writeTx(ctx, extraction.ProjectID, func(tx *sql.Tx) error {
		consolidatedIDs := make(map[string]struct{}, len(extraction.Memories))
		for _, evidenceID := range extraction.SummaryEvidenceIDs {
			ref, err := evidenceByID(ctx, tx, extraction.ProjectID, evidenceID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO evidence_summaries(evidence_id, project_id, content, created_at)
				VALUES(?, ?, ?, ?) ON CONFLICT(evidence_id) DO UPDATE SET content=excluded.content, created_at=excluded.created_at`,
				evidenceID, extraction.ProjectID, extraction.Summary, formatTime(ref.CreatedAt))
			if err != nil {
				return err
			}
		}
		for _, item := range extraction.Memories {
			storedID, err := consolidateMemory(ctx, tx, extraction.ProjectID, item, updatedAt)
			if err != nil {
				return err
			}
			for _, evidenceID := range item.EvidenceIDs {
				if _, err := evidenceByID(ctx, tx, extraction.ProjectID, evidenceID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO derived_memory_evidence(derived_id, evidence_id) VALUES(?, ?)`, storedID, evidenceID); err != nil {
					return err
				}
			}
			for _, oldID := range item.Supersedes {
				if oldID == storedID {
					return fmt.Errorf("%w: memory cannot supersede itself", ErrInvalidRequest)
				}
				if err := supersedeMemory(ctx, tx, extraction.ProjectID, oldID, storedID, updatedAt); err != nil {
					return err
				}
			}
			consolidatedIDs[storedID] = struct{}{}
		}
		if err := pruneActiveMemories(ctx, tx, extraction.ProjectID); err != nil {
			return err
		}
		ids := make([]string, 0, len(consolidatedIDs))
		for id := range consolidatedIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			item, err := derivedByID(ctx, tx, extraction.ProjectID, id)
			if err != nil {
				return err
			}
			if item.State != DerivedActive || !eligibleForAutomaticProposal(item) {
				continue
			}
			if _, err := ensureProposalTx(ctx, tx, item, updatedAt); err != nil {
				return err
			}
		}
		return refreshSummary(ctx, tx, extraction.ProjectID, updatedAt)
	})
}

func (s *Store) Summary(ctx context.Context, projectID string) (ProjectSummary, error) {
	if err := validateProjectID(projectID); err != nil {
		return ProjectSummary{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return ProjectSummary{}, err
	}
	var result ProjectSummary
	var updated string
	if err := s.db.QueryRowContext(ctx, `SELECT project_id, content, generation, updated_at FROM project_summaries WHERE project_id = ?`, projectID).Scan(&result.ProjectID, &result.Content, &result.Generation, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectSummary{}, ErrRecordNotFound
		}
		return ProjectSummary{}, err
	}
	result.TrustLevel = "untrusted"
	result.UpdatedAt = parseTime(updated)
	rows, err := s.db.QueryContext(ctx, `SELECT evidence_id FROM summary_evidence WHERE project_id = ? ORDER BY evidence_id`, projectID)
	if err != nil {
		return ProjectSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ProjectSummary{}, err
		}
		result.EvidenceIDs = append(result.EvidenceIDs, id)
	}
	return result, rows.Err()
}

func (s *Store) GetDerived(ctx context.Context, projectID, id string) (DerivedMemory, error) {
	if err := validateProjectID(projectID); err != nil {
		return DerivedMemory{}, err
	}
	if err := pipelineContext(ctx); err != nil {
		return DerivedMemory{}, err
	}
	return derivedByID(ctx, s.db, projectID, id)
}

// Search performs project-scoped FTS5 retrieval and returns trust metadata
// independently from lexical relevance, confidence, recency and usage.
func (s *Store) Search(ctx context.Context, projectID, query string, limit int) ([]SearchResult, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := pipelineContext(ctx); err != nil {
		return nil, err
	}
	if !s.FTS5Available() {
		return nil, ErrFTS5Unavailable
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: search limit exceeds %d", ErrInvalidRequest, maxSearchLimit)
	}
	ftsQuery, empty, err := lexicalQuery(query)
	if err != nil {
		return nil, err
	}
	if empty {
		return []SearchResult{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.project_id, d.field, d.knowledge_type, d.content, d.confidence, d.state, d.verification,
		d.created_at, d.updated_at, d.last_used_at, d.usage_count, bm25(derived_memory_fts)
		FROM derived_memory_fts JOIN derived_memories d ON d.id = derived_memory_fts.id
		WHERE derived_memory_fts MATCH ? AND derived_memory_fts.project_id = ? AND d.project_id = ? AND d.state = ?
		ORDER BY bm25(derived_memory_fts) ASC, d.id ASC LIMIT ?`, ftsQuery, projectID, projectID, DerivedActive, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0)
	for rows.Next() {
		item := SearchResult{}
		var created, updated string
		var lastUsed sql.NullString
		var rank float64
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Field, &item.Type, &item.Content, &item.Confidence, &item.State, &item.Verification, &created, &updated, &lastUsed, &item.UsageCount, &rank); err != nil {
			return nil, err
		}
		item.CreatedAt, item.UpdatedAt = parseTime(created), parseTime(updated)
		if lastUsed.Valid {
			last := parseTime(lastUsed.String)
			item.LastUsedAt = &last
		}
		item.Relevance = 1 / (1 + math.Abs(rank))
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range results {
		results[i].EvidenceIDs, err = evidenceIDsForDerived(ctx, s.db, results[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// RecordUsage records retrieval without changing the memory's verification or
// proposal state.
func (s *Store) RecordUsage(ctx context.Context, projectID, id string, at time.Time) error {
	if s != nil && s.readOnly {
		return ErrReadOnlyStore
	}
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	if err := pipelineContext(ctx); err != nil {
		return err
	}
	if id == "" {
		return ErrInvalidRequest
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE derived_memories SET usage_count = usage_count + 1, last_used_at = ?, updated_at = updated_at
		WHERE project_id = ? AND id = ? AND state = ?`, formatTime(at), projectID, id, DerivedActive)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrRecordNotFound
	}
	return secureSidecars(s.path)
}

func (s *Store) writeTx(ctx context.Context, projectID string, operation func(*sql.Tx) error) error {
	if s != nil && s.readOnly {
		return ErrReadOnlyStore
	}
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	if err := pipelineContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return errors.New("memory pipeline store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	now := formatTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(project_id, created_at, updated_at) VALUES(?, ?, ?) ON CONFLICT(project_id) DO NOTHING`, projectID, now, now); err != nil {
		return rollback(err)
	}
	// The first write acquires SQLite's cross-connection writer lock before any
	// reads used to build a consolidation result.
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET revision = revision + 1, updated_at = ? WHERE project_id = ?`, now, projectID); err != nil {
		return rollback(err)
	}
	if err := operation(tx); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return secureSidecars(s.path)
}

func consolidateMemory(ctx context.Context, tx *sql.Tx, projectID string, item DerivedMemory, updatedAt time.Time) (string, error) {
	if err := validateDerivedMemory(projectID, item); err != nil {
		return "", err
	}
	var supersedes any
	if len(item.Supersedes) == 1 {
		supersedes = item.Supersedes[0]
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO derived_memories(id, project_id, field, knowledge_type, content, confidence, state, verification, created_at, updated_at, usage_count, supersedes_derived_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
		ON CONFLICT(project_id, field, content) DO UPDATE SET updated_at = excluded.updated_at`,
		item.ID, projectID, item.Field, item.Type, item.Content, item.Confidence, item.State, item.Verification,
		formatTime(item.CreatedAt), formatTime(updatedAt), supersedes); err != nil {
		return "", err
	}
	var storedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM derived_memories WHERE project_id = ? AND field = ? AND content = ?`, projectID, item.Field, item.Content).Scan(&storedID); err != nil {
		return "", err
	}
	if len(item.Supersedes) > 1 {
		return "", fmt.Errorf("%w: one derived memory may explicitly supersede at most one record", ErrInvalidRequest)
	}
	return storedID, nil
}

func supersedeMemory(ctx context.Context, tx *sql.Tx, projectID, oldID, newID string, at time.Time) error {
	if oldID == "" {
		return ErrInvalidRequest
	}
	var oldProject string
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM derived_memories WHERE id = ?`, oldID).Scan(&oldProject); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRecordNotFound
		}
		return err
	}
	if oldProject != projectID {
		return ErrInvalidRequest
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO derived_supersessions(old_id, new_id, project_id, created_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(old_id) DO NOTHING`, oldID, newID, projectID, formatTime(at)); err != nil {
		return err
	}
	var currentSuccessor string
	if err := tx.QueryRowContext(ctx, `SELECT new_id FROM derived_supersessions WHERE old_id = ?`, oldID).Scan(&currentSuccessor); err != nil {
		return err
	}
	if currentSuccessor != newID {
		return fmt.Errorf("%w: derived memory already has a different superseding record", ErrConflict)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE derived_memories SET state = ?, updated_at = ? WHERE id = ? AND project_id = ?`, DerivedSuperseded, formatTime(at), oldID, projectID); err != nil {
		return err
	}
	return nil
}

func pruneActiveMemories(ctx context.Context, tx *sql.Tx, projectID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE derived_memories SET state = ?
		WHERE project_id = ? AND state = ? AND id IN (
			SELECT id FROM derived_memories WHERE project_id = ? AND state = ?
			ORDER BY usage_count DESC, COALESCE(last_used_at, updated_at) DESC, id ASC
			LIMIT -1 OFFSET ?
	)`, DerivedExpired, projectID, DerivedActive, projectID, DerivedActive, maxActiveMemories)
	return err
}

func refreshSummary(ctx context.Context, tx *sql.Tx, projectID string, updatedAt time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.evidence_id, s.content FROM evidence_summaries s
		JOIN evidence_refs e ON e.id = s.evidence_id
		WHERE s.project_id = ? ORDER BY e.created_at DESC, s.evidence_id ASC LIMIT ?`, projectID, maxSummaryEntries)
	if err != nil {
		return err
	}
	type entry struct{ evidenceID, content string }
	entries := make([]entry, 0, maxSummaryEntries)
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.evidenceID, &item.content); err != nil {
			_ = rows.Close()
			return err
		}
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString("Generated untrusted project summary:")
	selected := make([]string, 0, len(entries))
	for _, item := range entries {
		line := "\n- " + strings.TrimSpace(item.content)
		if builder.Len()+len(line) > maxProjectSummary {
			break
		}
		builder.WriteString(line)
		selected = append(selected, item.evidenceID)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_summaries(project_id, content, generation, updated_at) VALUES(?, ?, 1, ?)
		ON CONFLICT(project_id) DO UPDATE SET content = excluded.content, generation = project_summaries.generation + 1, updated_at = excluded.updated_at`, projectID, builder.String(), formatTime(updatedAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM summary_evidence WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, evidenceID := range selected {
		if _, err := tx.ExecContext(ctx, `INSERT INTO summary_evidence(project_id, evidence_id) VALUES(?, ?)`, projectID, evidenceID); err != nil {
			return err
		}
	}
	return nil
}

func validateEvidenceRef(ref RunEvidenceRef) error {
	if !projectIDPattern.MatchString(ref.ProjectID) || !evidenceIDPattern.MatchString(ref.ID) || !runtimePattern.MatchString(ref.Runtime) ||
		ref.NativeRunID == "" || ref.CreatedAt.IsZero() || ref.Status != ImportCompleted || !contentHashPattern.MatchString(ref.ContentHash) ||
		!nativeRunIDPattern.MatchString(ref.NativeRunID) || !validSourceReference(ref.SourceRef, ref.NativeRunID) {
		return fmt.Errorf("%w: evidence reference is incomplete or unsafe", ErrInvalidRequest)
	}
	if ref.ID != evidenceIdentity(ref.ProjectID, ref.Runtime, ref.NativeRunID, ref.ContentHash) {
		return fmt.Errorf("%w: evidence reference identity does not match its source hash", ErrInvalidRequest)
	}
	return nil
}

func validSourceReference(value, nativeRunID string) bool {
	if len(value) == 0 || len(value) > 512 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Opaque == "" && parsed.Host != "" &&
		(parsed.Host == "runs" || parsed.Host == "sessions") && parsed.User == nil && parsed.RawQuery == "" &&
		parsed.Fragment == "" && parsed.RawPath == "" && parsed.Path == "/"+nativeRunID
}

func validateExtraction(extraction Extraction) error {
	if err := validateProjectID(extraction.ProjectID); err != nil {
		return err
	}
	if extraction.EvidenceID == "" || strings.TrimSpace(extraction.Summary) == "" || len(extraction.Summary) > maxProjectSummary || len(extraction.Memories) > maxActiveMemories {
		return fmt.Errorf("%w: extraction is incomplete or exceeds its bound", ErrInvalidRequest)
	}
	foundPrimary := false
	seenEvidence := make(map[string]struct{}, len(extraction.SummaryEvidenceIDs))
	for _, id := range extraction.SummaryEvidenceIDs {
		if !evidenceIDPattern.MatchString(id) {
			return fmt.Errorf("%w: invalid summary evidence reference", ErrInvalidRequest)
		}
		seenEvidence[id] = struct{}{}
		if id == extraction.EvidenceID {
			foundPrimary = true
		}
	}
	if !foundPrimary {
		return fmt.Errorf("%w: primary evidence must be linked to the summary", ErrInvalidRequest)
	}
	for _, item := range extraction.Memories {
		if err := validateDerivedMemory(extraction.ProjectID, item); err != nil {
			return err
		}
		for _, id := range item.EvidenceIDs {
			if !evidenceIDPattern.MatchString(id) {
				return fmt.Errorf("%w: invalid derived evidence reference", ErrInvalidRequest)
			}
			seenEvidence[id] = struct{}{}
		}
	}
	if len(seenEvidence) > maxEvidenceItems {
		return fmt.Errorf("%w: extraction links too many evidence references", ErrInvalidRequest)
	}
	return nil
}

func validateDerivedMemory(projectID string, item DerivedMemory) error {
	if item.ProjectID != projectID || item.ID == "" || item.Field == "" || strings.TrimSpace(item.Content) == "" || len(item.Content) > maxEvidenceFieldBytes ||
		len(item.EvidenceIDs) == 0 || math.IsNaN(item.Confidence) || math.IsInf(item.Confidence, 0) || item.Confidence < 0 || item.Confidence > 1 ||
		item.State != DerivedActive || item.Verification != VerificationUnverified || item.CreatedAt.IsZero() {
		return fmt.Errorf("%w: derived memory is incomplete or outside project scope", ErrInvalidRequest)
	}
	seen := make(map[string]struct{}, len(item.EvidenceIDs))
	for _, id := range item.EvidenceIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
	}
	for _, id := range item.Supersedes {
		if id == "" || id == item.ID {
			return fmt.Errorf("%w: invalid supersession reference", ErrInvalidRequest)
		}
	}
	if len(item.Supersedes) > 1 {
		return fmt.Errorf("%w: one derived memory may supersede at most one record", ErrInvalidRequest)
	}
	return nil
}

func validateProjectID(id string) error {
	if !projectIDPattern.MatchString(id) {
		return fmt.Errorf("%w: invalid project identity", ErrInvalidRequest)
	}
	return nil
}

func lexicalQuery(query string) (string, bool, error) {
	if len(query) > 512 {
		return "", false, fmt.Errorf("%w: search query exceeds 512 bytes", ErrInvalidRequest)
	}
	terms := strings.FieldsFunc(strings.TrimSpace(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(terms) == 0 {
		return "", true, nil
	}
	if len(terms) > maxSearchTerms {
		return "", false, fmt.Errorf("%w: search query exceeds %d terms", ErrInvalidRequest, maxSearchTerms)
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		if len(term) > 64 {
			return "", false, fmt.Errorf("%w: search term exceeds 64 bytes", ErrInvalidRequest)
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND "), false, nil
}

func evidenceByID(ctx context.Context, queryer sqlQueryer, projectID, evidenceID string) (RunEvidenceRef, error) {
	var result RunEvidenceRef
	var created, status string
	if err := queryer.QueryRowContext(ctx, `SELECT id, project_id, runtime, native_run_id, source_ref, created_at, content_hash, import_status
		FROM evidence_refs WHERE project_id = ? AND id = ?`, projectID, evidenceID).Scan(
		&result.ID, &result.ProjectID, &result.Runtime, &result.NativeRunID, &result.SourceRef, &created, &result.ContentHash, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunEvidenceRef{}, ErrRecordNotFound
		}
		return RunEvidenceRef{}, err
	}
	result.CreatedAt, result.Status = parseTime(created), ImportStatus(status)
	return result, nil
}

func derivedByID(ctx context.Context, queryer sqlQueryer, projectID, id string) (DerivedMemory, error) {
	item := DerivedMemory{}
	var created, updated string
	var lastUsed sql.NullString
	var supersedes sql.NullString
	if err := queryer.QueryRowContext(ctx, `SELECT id, project_id, field, knowledge_type, content, confidence, state, verification, created_at, updated_at, last_used_at, usage_count, supersedes_derived_id
		FROM derived_memories WHERE project_id = ? AND id = ?`, projectID, id).Scan(
		&item.ID, &item.ProjectID, &item.Field, &item.Type, &item.Content, &item.Confidence, &item.State, &item.Verification, &created, &updated, &lastUsed, &item.UsageCount, &supersedes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DerivedMemory{}, ErrRecordNotFound
		}
		return DerivedMemory{}, err
	}
	item.CreatedAt, item.UpdatedAt = parseTime(created), parseTime(updated)
	if lastUsed.Valid {
		value := parseTime(lastUsed.String)
		item.LastUsedAt = &value
	}
	if supersedes.Valid {
		item.Supersedes = []string{supersedes.String}
	}
	ids, err := evidenceIDsForDerived(ctx, queryer, item.ID)
	if err != nil {
		return DerivedMemory{}, err
	}
	item.EvidenceIDs = ids
	return item, nil
}

func evidenceIDsForDerived(ctx context.Context, queryer sqlQueryer, derivedID string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT evidence_id FROM derived_memory_evidence WHERE derived_id = ? ORDER BY evidence_id`, derivedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func pipelineContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidRequest)
	}
	return ctx.Err()
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed.UTC()
}

func secureSidecars(databasePath string) error {
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := rejectSymlinkOrSpecial(path); err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			if err := os.Chmod(path, 0o600); err != nil {
				return fmt.Errorf("restrict memory pipeline file permissions: %w", err)
			}
		}
	}
	return nil
}
