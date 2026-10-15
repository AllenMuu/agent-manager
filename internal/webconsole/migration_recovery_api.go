package webconsole

import (
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillsync"
)

func (s *Server) handleMigrationPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create a migration preview with POST.")
		return
	}
	var request struct {
		SelectedSourceIDs []string `json:"selectedSourceIds"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	service, project, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	if project != nil {
		if err := project.validate(); err != nil {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed. Restart the console to register it again.")
			return
		}
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	preview, err := service.PreviewMigration(request.SelectedSourceIDs, adapter.MaterializationMode(s.currentSyncMode()))
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanMigration, migration: preview})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The migration preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID  string                     `json:"planId"`
		Preview skillsync.MigrationPreview `json:"preview"`
	}{plan.id, preview})
}

func (s *Server) handleMigrationApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm a migration preview with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanMigration)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The migration preview expired or was already used. Review a new preview.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The migration preview expired while waiting for another local operation. Review a new preview.")
		return
	}
	if adapter.MaterializationMode(s.currentSyncMode()) != adapter.MaterializationMode(plan.migration.SyncMode) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The synchronization setting changed after migration review. Review a new preview.")
		return
	}
	project := s.currentProject()
	if plan.migration.Scope == skillsync.ScopeProject {
		if project == nil || project.validate() != nil || filepath.Clean(project.Path) != filepath.Clean(plan.migration.Root()) {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project changed after migration review.")
			return
		}
	}
	service, _, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	service.Now = s.now
	result, err := service.ApplyMigrationUntil(plan.migration, plan.expiresAt)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	s.discardAllSyncPlans()
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMigrationUndoPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create a migration Undo preview with POST.")
		return
	}
	var request struct {
		Scope string `json:"scope"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	scope := lifecycle.ResyncScope(request.Scope)
	var project *registeredProject
	if scope == lifecycle.ResyncProject {
		project = s.currentProject()
		if project == nil || project.validate() != nil {
			writeAPIError(w, http.StatusConflict, "project_required", "Register a current project before reviewing migration Undo.")
			return
		}
	} else if scope != lifecycle.ResyncGlobal {
		writeAPIError(w, http.StatusBadRequest, "invalid_scope", "Choose global or project scope.")
		return
	}
	_, journal, err := s.newLifecycleService(scope, project)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.Operation != skillsync.MigrationOperation || entry.ResourceKind != "skill" {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "There is no latest Skill reference migration available to undo in this scope.")
		return
	}
	if err := validateMigrationUndoEntry(scope, project, s.homeDir, s.libraryPath, entry); err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The migration operation is outside its safe Undo boundary.")
		return
	}
	available, err := journal.UndoAvailable()
	if err != nil || !available {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "The migrated entries changed after the operation was recorded.")
		return
	}
	if err := lifecycle.ValidateLinkDependenciesWithBefore(entry.LinkDependencies, entry.Before); err != nil {
		writeSyncError(w, errors.Join(operation.ErrUnexpectedState, err))
		return
	}
	undoPlan, err := journal.PreviewUndo()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanMigrationUndo, undoScope: scope, undoJournal: journal, undoPlan: undoPlan, undoEntry: entry})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The migration Undo preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID string         `json:"planId"`
		Plan   operation.Plan `json:"plan"`
	}{plan.id, undoPlan})
}

func (s *Server) handleMigrationUndoApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm migration Undo with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanMigrationUndo)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The migration Undo preview expired or was already used.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) || plan.undoJournal == nil {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The migration Undo preview expired or its journal changed.")
		return
	}
	var project *registeredProject
	if plan.undoScope == lifecycle.ResyncProject {
		project = s.currentProject()
		if project == nil || project.validate() != nil || filepath.Clean(plan.undoJournal.Path) != filepath.Join(project.Path, ".skill-manager", "journal.json") {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project changed after migration Undo review.")
			return
		}
	}
	entry, found, err := plan.undoJournal.Latest()
	if err != nil || !found || !reflect.DeepEqual(entry, plan.undoEntry) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The latest scope operation changed after migration Undo review.")
		return
	}
	if err := validateMigrationUndoEntry(plan.undoScope, project, s.homeDir, s.libraryPath, entry); err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The migration operation is outside its safe Undo boundary.")
		return
	}
	previous := plan.undoJournal.ValidateUndoEntry
	plan.undoJournal.ValidateUndoEntry = func(current operation.Entry) error {
		if previous != nil {
			if err := previous(current); err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(current, plan.undoEntry) {
			return lifecycle.ErrPlanChanged
		}
		return validateMigrationUndoEntry(plan.undoScope, project, s.homeDir, s.libraryPath, current)
	}
	beforePublish := plan.undoJournal.BeforeRestorePublish
	plan.undoJournal.BeforeRestorePublish = func(path string) error {
		if beforePublish != nil {
			if err := beforePublish(path); err != nil {
				return err
			}
		}
		if !s.now().Before(plan.expiresAt) {
			return operation.ErrUnexpectedState
		}
		return nil
	}
	service := s.lifecycleService(plan.undoJournal, func(current operation.Plan) bool { return reflect.DeepEqual(current, plan.undoPlan) })
	fingerprints, err := undoBackupFingerprints(entry)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	if err := service.UndoWithFingerprints(fingerprints); err != nil {
		writeSyncError(w, err)
		return
	}
	s.discardAllSyncPlans()
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{"undone"})
}

func (s *Server) handleRecoveryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read pending recovery batches with GET.")
		return
	}
	service, project, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	preview, err := service.PreviewRecovery()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	filtered := preview.Batches[:0]
	store := operation.NewBatchStore(filepath.Join(s.homeDir, ".agent-manager", "skill-sync", "batches"))
	for _, candidate := range preview.Batches {
		batch, found, getErr := store.Get(candidate.BatchID)
		if getErr != nil {
			writeSyncError(w, getErr)
			return
		}
		if found && s.recoveryBatchAccessible(batch, project) {
			filtered = append(filtered, candidate)
		}
	}
	preview.Batches = filtered
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleRecoveryPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create a recovery preview with POST.")
		return
	}
	var request struct {
		BatchID string                   `json:"batchId"`
		Action  skillsync.RecoveryAction `json:"action"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	service, project, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	store := operation.NewBatchStore(filepath.Join(s.homeDir, ".agent-manager", "skill-sync", "batches"))
	batch, found, err := store.Get(request.BatchID)
	if err != nil || !found || !s.recoveryBatchAccessible(batch, project) {
		writeAPIError(w, http.StatusConflict, "recovery_unavailable", "The pending batch is unavailable in the current scope.")
		return
	}
	preview, err := service.PreviewRecovery()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	var candidate *skillsync.RecoveryCandidate
	for i := range preview.Batches {
		if preview.Batches[i].BatchID == request.BatchID {
			candidate = &preview.Batches[i]
			break
		}
	}
	if candidate == nil || !containsRecoveryAction(candidate.Actions, request.Action) {
		writeAPIError(w, http.StatusConflict, "recovery_conflict", "The batch changed or cannot safely perform that recovery action.")
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanRecovery, recovery: *candidate, recoveryAction: request.Action})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The recovery preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID  string                      `json:"planId"`
		Preview skillsync.RecoveryCandidate `json:"preview"`
	}{plan.id, *candidate})
}

func (s *Server) handleRecoveryApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm a recovery preview with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanRecovery)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The recovery preview expired or was already used. Review the current batch again.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The recovery preview expired while waiting for another local operation.")
		return
	}
	service, project, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	store := operation.NewBatchStore(filepath.Join(s.homeDir, ".agent-manager", "skill-sync", "batches"))
	batch, found, err := store.Get(plan.recovery.BatchID)
	if err != nil || !found || !s.recoveryBatchAccessible(batch, project) {
		writeAPIError(w, http.StatusConflict, "recovery_unavailable", "The pending batch is unavailable in the current scope.")
		return
	}
	result, err := service.ApplyRecovery(plan.recovery.BatchID, plan.recoveryAction)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	s.discardAllSyncPlans()
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) latestMigrationUndoAvailable(scope lifecycle.ResyncScope, project *registeredProject) bool {
	_, journal, err := s.newLifecycleService(scope, project)
	if err != nil {
		return false
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.Operation != skillsync.MigrationOperation || entry.ResourceKind != "skill" {
		return false
	}
	if err := validateMigrationUndoEntry(scope, project, s.homeDir, s.libraryPath, entry); err != nil {
		return false
	}
	if err := lifecycle.ValidateLinkDependenciesWithBefore(entry.LinkDependencies, entry.Before); err != nil {
		return false
	}
	available, err := journal.UndoAvailable()
	return err == nil && available
}

func validateMigrationUndoEntry(scope lifecycle.ResyncScope, project *registeredProject, home, library string, entry operation.Entry) error {
	if entry.Operation != skillsync.MigrationOperation || entry.ResourceKind != "skill" || len(entry.Before) == 0 || len(entry.Before) != len(entry.After) {
		return lifecycle.ErrUnsafePath
	}
	if scope == lifecycle.ResyncGlobal {
		return validateGlobalJournalEntry(home, entry)
	}
	if scope != lifecycle.ResyncProject || project == nil {
		return lifecycle.ErrUnsafePath
	}
	return validateConsoleUndoEntry(project, library, entry)
}

func undoBackupFingerprints(entry operation.Entry) (map[string]string, error) {
	fingerprints := make(map[string]string)
	for _, snapshot := range entry.Before {
		if !snapshot.Exists {
			continue
		}
		fingerprint, err := operation.FingerprintPath(snapshot.Backup)
		if err != nil {
			return nil, err
		}
		fingerprints[snapshot.Backup] = fingerprint
	}
	return fingerprints, nil
}

func (s *Server) recoveryBatchAccessible(batch operation.Batch, project *registeredProject) bool {
	if batch.Kind == "import" {
		return batch.Scope == "local" && pathsWithin(s.libraryPath, batch.Resources)
	}
	switch batch.Scope {
	case "global":
		return pathsWithin(s.homeDir, batch.Resources)
	case "project":
		return project != nil && project.validate() == nil && pathsWithin(project.Path, batch.Resources)
	default:
		return false
	}
}

func pathsWithin(root string, paths []string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil || len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return false
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return false
		}
	}
	return true
}

func containsRecoveryAction(actions []skillsync.RecoveryAction, wanted skillsync.RecoveryAction) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}
