package webconsole

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

const operationPlanTTL = 10 * time.Minute

type storedPlanState uint8

const (
	storedPlanPending storedPlanState = iota
	storedPlanExecuting
	storedPlanCompleted
	storedPlanCancelled
	storedPlanStale
)

type storedPlan struct {
	id                   string
	projectID            string
	kind                 string
	fingerprint          string
	conflictFingerprints map[string]string
	sourceFingerprints   map[string]string
	snapshotFingerprints map[string]string
	createdAt            time.Time
	expiresAt            time.Time
	activation           *activationRequest
	removal              *removalRequest
	state                storedPlanState
}

type activationRequest struct {
	SkillIdentifiers []string `json:"skillIdentifiers"`
	TargetAgents     []string `json:"targetAgents"`
	ConflictStrategy string   `json:"conflictStrategy,omitempty"`
	ForceConfirmed   bool     `json:"forceConfirmed,omitempty"`
}

type removalRequest struct {
	SkillIdentifier string `json:"skillIdentifier"`
	TargetAgent     string `json:"targetAgent"`
}

type operationPlanView struct {
	ID                        string      `json:"id"`
	ProjectID                 string      `json:"projectId"`
	Operation                 string      `json:"operation"`
	Version                   string      `json:"version"`
	ResourceKind              string      `json:"resourceKind"`
	Changes                   []changeDTO `json:"changes"`
	Warnings                  []string    `json:"warnings"`
	CreatedAt                 time.Time   `json:"createdAt"`
	ExpiresAt                 time.Time   `json:"expiresAt"`
	ForceReplacementConfirmed bool        `json:"forceReplacementConfirmed"`
}

type operationPlanEnvelope struct {
	Plan operationPlanView `json:"plan"`
}

func (s *Server) handleOperationAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "projects" && parts[4] == "plans" {
		if !s.requireProjectID(w, parts[3]) {
			return true
		}
		s.createOperationPlan(w, r, parts[5], parts[3])
		return true
	}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "plans" {
		if parts[4] == "execute" {
			s.executeOperationPlan(w, r, parts[3])
			return true
		}
		if parts[4] == "cancel" {
			s.cancelOperationPlan(w, r, parts[3])
			return true
		}
	}
	return false
}

func (s *Server) requireProjectID(w http.ResponseWriter, id string) bool {
	project := s.currentProject()
	if project == nil || id != project.ID {
		writeAPIError(w, http.StatusNotFound, "project_not_found", "The project is not registered in this console session.")
		return false
	}
	if err := project.validate(); err != nil {
		writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed. Restart the console to register it again.")
		return false
	}
	return true
}

func (s *Server) createOperationPlan(w http.ResponseWriter, r *http.Request, kind, projectID string) {
	project := s.currentProject()
	if project == nil || project.ID != projectID {
		writeAPIError(w, http.StatusNotFound, "project_not_found", "The project is not registered in this console session.")
		return
	}
	switch kind {
	case "activate":
		var request activationRequest
		if !decodeJSONBody(w, r, &request) {
			return
		}
		preview, fingerprint, conflictFingerprints, sourceFingerprints, err := s.previewActivation(project, request)
		if err != nil {
			writePlanError(w, err)
			return
		}
		stored, err := s.newStoredPlan(project.ID, "activate", fingerprint)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "plan_creation_failed", "A secure operation plan could not be created.")
			return
		}
		copy := cloneActivationRequest(request)
		stored.activation = &copy
		stored.conflictFingerprints = cloneFingerprints(conflictFingerprints)
		stored.sourceFingerprints = cloneFingerprints(sourceFingerprints)
		s.savePlan(stored)
		writeJSON(w, http.StatusCreated, operationPlanEnvelope{Plan: makePlanView(stored, preview)})
	case "remove":
		var request removalRequest
		if !decodeJSONBody(w, r, &request) {
			return
		}
		preview, fingerprint, err := s.previewRemoval(project, request)
		if err != nil {
			writePlanError(w, err)
			return
		}
		stored, err := s.newStoredPlan(project.ID, "remove", fingerprint)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "plan_creation_failed", "A secure operation plan could not be created.")
			return
		}
		copy := request
		stored.removal = &copy
		s.savePlan(stored)
		writeJSON(w, http.StatusCreated, operationPlanEnvelope{Plan: makePlanView(stored, preview)})
	case "undo":
		var request struct{}
		if !decodeJSONBody(w, r, &request) {
			return
		}
		preview, fingerprint, snapshotFingerprints, err := s.previewUndo(project)
		if err != nil {
			writePlanError(w, err)
			return
		}
		stored, err := s.newStoredPlan(project.ID, "undo", fingerprint)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "plan_creation_failed", "A secure operation plan could not be created.")
			return
		}
		stored.snapshotFingerprints = cloneFingerprints(snapshotFingerprints)
		s.savePlan(stored)
		writeJSON(w, http.StatusCreated, operationPlanEnvelope{Plan: makePlanView(stored, preview)})
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "The operation plan route was not found.")
	}
}

func (s *Server) previewActivation(project *registeredProject, request activationRequest) (operation.Plan, string, map[string]string, map[string]string, error) {
	if len(request.SkillIdentifiers) == 0 || len(request.SkillIdentifiers) > 100 || len(request.TargetAgents) == 0 || len(request.TargetAgents) > 20 {
		return operation.Plan{}, "", nil, nil, errors.New("select between 1 and 100 Skills and between 1 and 20 target agents")
	}
	if request.ConflictStrategy != "" && request.ConflictStrategy != string(lifecycle.ConflictReplace) {
		return operation.Plan{}, "", nil, nil, errors.New("unsupported conflict strategy")
	}
	if request.ForceConfirmed != (request.ConflictStrategy == string(lifecycle.ConflictReplace)) {
		return operation.Plan{}, "", nil, nil, errors.New("replacement requires the explicit force confirmation")
	}
	skills, _, err := catalog.Discover(s.libraryPath)
	if err != nil {
		return operation.Plan{}, "", nil, nil, errors.New("the configured Skill catalog could not be read")
	}
	byID := make(map[string]catalog.Skill, len(skills))
	for _, skill := range skills {
		byID[skill.Identifier] = skill
	}
	selected := make([]catalog.Skill, 0, len(request.SkillIdentifiers))
	seen := make(map[string]bool, len(request.SkillIdentifiers))
	for _, id := range request.SkillIdentifiers {
		if id == "" || seen[id] {
			return operation.Plan{}, "", nil, nil, errors.New("Skill identifiers must be non-empty and unique")
		}
		seen[id] = true
		skill, ok := byID[id]
		if !ok {
			return operation.Plan{}, "", nil, nil, fmt.Errorf("Skill %q is not eligible in the configured catalog", id)
		}
		selected = append(selected, skill)
	}
	targets := make([]adapter.Target, 0, len(request.TargetAgents))
	seen = make(map[string]bool, len(request.TargetAgents))
	for _, id := range request.TargetAgents {
		target := adapter.Target(id)
		if id == "" || seen[id] {
			return operation.Plan{}, "", nil, nil, errors.New("target agents must be non-empty and unique")
		}
		seen[id] = true
		if _, ok := adapter.For(target); !ok {
			return operation.Plan{}, "", nil, nil, fmt.Errorf("target agent %q is not supported", id)
		}
		targets = append(targets, target)
	}
	options := lifecycle.Options{}
	if request.ConflictStrategy == string(lifecycle.ConflictReplace) {
		options = lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	}
	journal, err := s.journalForProject(project)
	if err != nil {
		return operation.Plan{}, "", nil, nil, err
	}
	service := lifecycle.New(s.libraryPath, journal, func(operation.Plan) bool { return true })
	plan, err := service.PreviewAddMany(project.Path, selected, targets, options)
	if err != nil {
		return plan, "", nil, nil, fmt.Errorf("activation cannot be safely planned: %w", err)
	}
	conflictFingerprints := make(map[string]string)
	for _, change := range plan.Changes {
		if change.Action != "replace conflicting path with absolute link" {
			continue
		}
		digest, err := operation.FingerprintPath(change.Path)
		if err != nil {
			return plan, "", nil, nil, fmt.Errorf("replacement path cannot be safely fingerprinted: %w", err)
		}
		conflictFingerprints[change.Path] = digest
	}
	sourceFingerprints := make(map[string]string, len(selected))
	for _, skill := range selected {
		digest, err := operation.FingerprintPath(skill.SourcePath)
		if err != nil {
			return plan, "", nil, nil, fmt.Errorf("Skill source cannot be safely fingerprinted: %w", err)
		}
		sourceFingerprints[skill.SourcePath] = digest
	}
	fingerprint, err := fingerprint(struct {
		Plan                 operation.Plan
		Skills               []catalog.Skill
		Targets              []adapter.Target
		Options              lifecycle.Options
		ConflictFingerprints map[string]string
		SourceFingerprints   map[string]string
	}{plan, selected, targets, options, conflictFingerprints, sourceFingerprints})
	return plan, fingerprint, conflictFingerprints, sourceFingerprints, err
}

func (s *Server) previewRemoval(project *registeredProject, request removalRequest) (operation.Plan, string, error) {
	if request.SkillIdentifier == "" || request.TargetAgent == "" {
		return operation.Plan{}, "", errors.New("a Skill identifier and target agent are required")
	}
	target := adapter.Target(request.TargetAgent)
	if _, ok := adapter.For(target); !ok {
		return operation.Plan{}, "", fmt.Errorf("target agent %q is not supported", request.TargetAgent)
	}
	journal, err := s.journalForProject(project)
	if err != nil {
		return operation.Plan{}, "", err
	}
	service := lifecycle.New(s.libraryPath, journal, func(operation.Plan) bool { return true })
	plan, err := service.PreviewRemove(project.Path, target, request.SkillIdentifier)
	if err != nil {
		return plan, "", fmt.Errorf("removal cannot be safely planned: %w", err)
	}
	fingerprint, err := fingerprint(struct {
		Plan    operation.Plan
		Request removalRequest
	}{plan, request})
	return plan, fingerprint, err
}

func (s *Server) previewUndo(project *registeredProject) (operation.Plan, string, map[string]string, error) {
	journal, err := s.journalForProject(project)
	if err != nil {
		return operation.Plan{}, "", nil, err
	}
	entry, ok, err := journal.Latest()
	if err != nil {
		return operation.Plan{}, "", nil, errors.Join(lifecycle.ErrUnsafePath, errors.New("operation journal could not be read safely"))
	}
	if !ok {
		return operation.Plan{}, "", nil, errors.New("there is no operation available to undo")
	}
	if err := validateConsoleUndoEntry(project, s.libraryPath, entry); err != nil {
		return operation.Plan{}, "", nil, err
	}
	available, err := journal.UndoAvailable()
	if err != nil {
		return operation.Plan{}, "", nil, errors.Join(lifecycle.ErrUnsafePath, errors.New("operation journal could not be inspected safely"))
	}
	if !available {
		return operation.Plan{}, "", nil, errors.New("there is no current operation available to undo")
	}
	plan, err := journal.PreviewUndo()
	if err != nil {
		if errors.Is(err, lifecycle.ErrUnsafePath) {
			return operation.Plan{}, "", nil, errors.Join(lifecycle.ErrUnsafePath, errors.New("operation journal could not be inspected safely"))
		}
		return operation.Plan{}, "", nil, err
	}
	snapshotFingerprints := make(map[string]string)
	for _, snapshot := range entry.Before {
		if !snapshot.Exists {
			continue
		}
		digest, err := operation.FingerprintPath(snapshot.Backup)
		if err != nil {
			return operation.Plan{}, "", nil, errors.Join(lifecycle.ErrUnsafePath, err)
		}
		snapshotFingerprints[snapshot.Backup] = digest
	}
	fingerprint, err := fingerprint(struct {
		Entry                operation.Entry
		Plan                 operation.Plan
		SnapshotFingerprints map[string]string
	}{entry, plan, snapshotFingerprints})
	return plan, fingerprint, snapshotFingerprints, err
}

func (s *Server) newStoredPlan(projectID, kind, fingerprint string) (*storedPlan, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	created := s.now().UTC()
	return &storedPlan{id: hex.EncodeToString(idBytes), projectID: projectID, kind: kind, fingerprint: fingerprint, createdAt: created, expiresAt: created.Add(operationPlanTTL), state: storedPlanPending}, nil
}

func (s *Server) savePlan(plan *storedPlan) {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	now := s.now()
	for id, existing := range s.plans {
		if existing.state != storedPlanExecuting && !now.Before(existing.expiresAt) {
			delete(s.plans, id)
		}
	}
	s.plans[plan.id] = plan
}

func makePlanView(plan *storedPlan, preview operation.Plan) operationPlanView {
	converted := toPlanDTO(preview)
	return operationPlanView{ID: plan.id, ProjectID: plan.projectID, Operation: converted.Operation, Version: converted.Version, ResourceKind: converted.ResourceKind, Changes: converted.Changes, Warnings: converted.Warnings, CreatedAt: plan.createdAt, ExpiresAt: plan.expiresAt, ForceReplacementConfirmed: plan.activation != nil && plan.activation.ForceConfirmed}
}

func (s *Server) executeOperationPlan(w http.ResponseWriter, r *http.Request, id string) {
	var request struct{}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	s.plansMu.Lock()
	plan, ok := s.plans[id]
	if !ok {
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusNotFound, "plan_not_found", "The operation plan does not exist in this session.")
		return
	}
	if !s.now().Before(plan.expiresAt) {
		plan.state = storedPlanStale
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusGone, "plan_expired", "The operation plan expired; create a fresh preview.")
		return
	}
	switch plan.state {
	case storedPlanCancelled:
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusConflict, "plan_cancelled", "The operation plan was cancelled.")
		return
	case storedPlanCompleted:
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusConflict, "plan_consumed", "The operation plan has already been executed.")
		return
	case storedPlanStale:
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusConflict, "stale_plan", "The operation plan is no longer current; create a fresh preview.")
		return
	case storedPlanExecuting:
		s.plansMu.Unlock()
		writeAPIError(w, http.StatusConflict, "plan_executing", "The operation plan is already being executed.")
		return
	}
	plan.state = storedPlanExecuting
	s.plansMu.Unlock()
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		s.finishPlan(plan, storedPlanStale)
		writeAPIError(w, http.StatusGone, "plan_expired", "The operation plan expired while waiting for another local operation; create a fresh preview.")
		return
	}

	project := s.currentProject()
	if project == nil || project.ID != plan.projectID || project.validate() != nil {
		s.finishPlan(plan, storedPlanStale)
		writeAPIError(w, http.StatusConflict, "stale_plan", "The registered project changed; create a fresh preview.")
		return
	}
	var preview operation.Plan
	var currentFingerprint string
	var err error
	switch plan.kind {
	case "activate":
		preview, currentFingerprint, _, _, err = s.previewActivation(project, *plan.activation)
	case "remove":
		preview, currentFingerprint, err = s.previewRemoval(project, *plan.removal)
	case "undo":
		preview, currentFingerprint, _, err = s.previewUndo(project)
	default:
		err = errors.New("unknown operation plan kind")
	}
	if err != nil || currentFingerprint != plan.fingerprint {
		s.finishPlan(plan, storedPlanStale)
		writeAPIError(w, http.StatusConflict, "stale_plan", "Project, library, or journal state changed after review; create a fresh preview.")
		return
	}

	switch plan.kind {
	case "activate":
		skills, _, discoverErr := catalog.Discover(s.libraryPath)
		if discoverErr != nil {
			err = discoverErr
			break
		}
		byID := make(map[string]catalog.Skill, len(skills))
		for _, skill := range skills {
			byID[skill.Identifier] = skill
		}
		selected := make([]catalog.Skill, 0, len(plan.activation.SkillIdentifiers))
		for _, id := range plan.activation.SkillIdentifiers {
			selected = append(selected, byID[id])
		}
		targets := make([]adapter.Target, 0, len(plan.activation.TargetAgents))
		for _, id := range plan.activation.TargetAgents {
			targets = append(targets, adapter.Target(id))
		}
		options := lifecycle.Options{ExpectedConflictFingerprints: cloneFingerprints(plan.conflictFingerprints), ExpectedSourceFingerprints: cloneFingerprints(plan.sourceFingerprints)}
		if plan.activation.ConflictStrategy == string(lifecycle.ConflictReplace) && plan.activation.ForceConfirmed {
			options.Conflict = lifecycle.ConflictReplace
			options.Force = true
		}
		journal, journalErr := s.journalForProject(project)
		if journalErr != nil {
			err = journalErr
			break
		}
		service := lifecycle.New(s.libraryPath, journal, func(operation.Plan) bool { return true })
		service.BeforeFinalPublish = func() error {
			if err := validateProjectJournalLayout(project); err != nil {
				return err
			}
			for path, expected := range plan.sourceFingerprints {
				actual, err := operation.FingerprintPath(path)
				if err != nil {
					return errors.Join(lifecycle.ErrUnsafePath, err)
				}
				if actual != expected {
					return lifecycle.ErrUnsafePath
				}
			}
			return nil
		}
		_, err = service.AddMany(project.Path, selected, targets, options)
	case "remove":
		journal, journalErr := s.journalForProject(project)
		if journalErr != nil {
			err = journalErr
			break
		}
		service := lifecycle.New(s.libraryPath, journal, func(operation.Plan) bool { return true })
		service.BeforePublish = func(string) error { return validateProjectJournalLayout(project) }
		_, err = service.Remove(project.Path, adapter.Target(plan.removal.TargetAgent), plan.removal.SkillIdentifier)
	case "undo":
		journal, journalErr := s.journalForProject(project)
		if journalErr != nil {
			err = journalErr
			break
		}
		journal.BeforeRestorePublish = func(string) error { return validateProjectJournalLayout(project) }
		err = journal.UndoLatestWithFingerprints(func(operation.Plan) bool { return true }, cloneFingerprints(plan.snapshotFingerprints))
	}
	if err != nil {
		s.finishPlan(plan, storedPlanCompleted)
		if errors.Is(err, operation.ErrJournalCommitted) {
			writeAPIError(w, http.StatusInternalServerError, "journal_committed", "The operation was applied and its journal record was published, but journal-directory sync failed. Current project state was not rolled back. Refresh before creating another plan.")
			return
		}
		if errors.Is(err, operation.ErrUnexpectedState) || errors.Is(err, lifecycle.ErrConflict) || errors.Is(err, lifecycle.ErrUnsafePath) {
			writeAPIError(w, http.StatusConflict, "stale_plan", "Operation state changed during execution. Inspect the project and operation journal before creating a fresh preview.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "operation_failed", "The operation failed. Inspect the project and operation journal before retrying.")
		return
	}
	s.finishPlan(plan, storedPlanCompleted)
	writeJSON(w, http.StatusOK, struct {
		Operation string `json:"operation"`
		Status    string `json:"status"`
	}{Operation: preview.Operation, Status: "completed"})
}

func (s *Server) cancelOperationPlan(w http.ResponseWriter, r *http.Request, id string) {
	var request struct{}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	plan, ok := s.plans[id]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "plan_not_found", "The operation plan does not exist in this session.")
		return
	}
	if !s.now().Before(plan.expiresAt) {
		plan.state = storedPlanStale
		writeAPIError(w, http.StatusGone, "plan_expired", "The operation plan expired and can no longer be cancelled or executed.")
		return
	}
	if plan.state != storedPlanPending {
		writeAPIError(w, http.StatusConflict, "plan_not_pending", "Only a pending operation plan can be cancelled.")
		return
	}
	plan.state = storedPlanCancelled
	writeJSON(w, http.StatusOK, struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}{ID: plan.id, Status: "cancelled"})
}

func (s *Server) finishPlan(plan *storedPlan, state storedPlanState) {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	if current := s.plans[plan.id]; current == plan && current.state == storedPlanExecuting {
		current.state = state
	}
}

func writePlanError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "invalid_plan"
	message := err.Error()
	if errors.Is(err, lifecycle.ErrUnsafePath) || errors.Is(err, lifecycle.ErrForceRequired) || errors.Is(err, lifecycle.ErrConflict) {
		status = http.StatusConflict
		code = "operation_conflict"
	}
	if strings.Contains(message, "there is no operation available to undo") || strings.Contains(message, "there is no current operation available to undo") {
		status = http.StatusConflict
		code = "nothing_to_undo"
	}
	writeAPIError(w, status, code, message)
}

func fingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func cloneActivationRequest(request activationRequest) activationRequest {
	request.SkillIdentifiers = append([]string(nil), request.SkillIdentifiers...)
	request.TargetAgents = append([]string(nil), request.TargetAgents...)
	return request
}

func cloneFingerprints(fingerprints map[string]string) map[string]string {
	if len(fingerprints) == 0 {
		return nil
	}
	clone := make(map[string]string, len(fingerprints))
	for path, digest := range fingerprints {
		clone[path] = digest
	}
	return clone
}
