package webconsole

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"github.com/AllenMuu/skill-manager/internal/skillsync"
)

const syncPlanTTL = 10 * time.Minute

type syncPlanKind string

const (
	syncPlanImport     syncPlanKind = "import"
	syncPlanResync     syncPlanKind = "resync"
	syncPlanResyncUndo syncPlanKind = "resync-undo"
	syncPlanUpdate     syncPlanKind = "upstream-update"
	syncPlanUpdateUndo syncPlanKind = "upstream-update-undo"
)

type storedSyncPlan struct {
	id            string
	kind          syncPlanKind
	expiresAt     time.Time
	importPreview skillsync.Preview
	upstreamPlan  skillsync.PreparedUpdatePlan
	resync        lifecycle.ResyncPlan
	undoScope     lifecycle.ResyncScope
	undoJournal   *operation.Journal
	undoPlan      operation.Plan
	undoEntry     operation.Entry
}

func (s *Server) handleSyncAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/sync/") {
		return false
	}
	if r.URL.Path == "/api/v1/sync/settings" {
		s.handleSyncSettings(w, r)
		return true
	}
	switch r.URL.Path {
	case "/api/v1/sync/sources":
		s.handleSyncSources(w, r)
	case "/api/v1/sync/updates/check":
		s.handleUpdatesCheck(w, r)
	case "/api/v1/sync/updates/preview":
		s.handleUpdatesPreview(w, r)
	case "/api/v1/sync/updates/apply":
		s.handleUpdatesApply(w, r)
	case "/api/v1/sync/updates/undo/preview":
		s.handleUpdatesUndoPreview(w, r)
	case "/api/v1/sync/updates/undo/apply":
		s.handleUpdatesUndoApply(w, r)
	case "/api/v1/sync/import/preview":
		s.handleImportPreview(w, r)
	case "/api/v1/sync/import/apply":
		s.handleImportApply(w, r)
	case "/api/v1/sync/resync/preview":
		s.handleResyncPreview(w, r)
	case "/api/v1/sync/resync/apply":
		s.handleResyncApply(w, r)
	case "/api/v1/sync/resync/undo/preview":
		s.handleResyncUndoPreview(w, r)
	case "/api/v1/sync/resync/undo/apply":
		s.handleResyncUndoApply(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "The synchronization API route was not found.")
	}
	return true
}

func (s *Server) handleSyncSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		settings, err := config.Load(s.configPath)
		if err != nil {
			s.mu.Unlock()
			writeAPIError(w, http.StatusInternalServerError, "settings_unavailable", "Agent Manager settings could not be read.")
			return
		}
		mode := settings.SyncMode.Effective()
		s.syncMode = string(mode)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, struct {
			RepositoryPath string          `json:"repositoryPath"`
			SyncMode       config.SyncMode `json:"syncMode"`
		}{RepositoryPath: s.libraryPath, SyncMode: mode})
	case http.MethodPut:
		var request struct {
			SyncMode string `json:"syncMode"`
		}
		if !decodeJSONBody(w, r, &request) {
			return
		}
		mode := config.SyncMode(request.SyncMode)
		if err := mode.Validate(); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_sync_mode", "Choose auto, symlink-only, or copy-only.")
			return
		}
		s.mu.Lock()
		if err := config.SaveSyncMode(s.configPath, mode); err != nil {
			s.mu.Unlock()
			writeAPIError(w, http.StatusInternalServerError, "settings_write_failed", "The synchronization setting could not be saved.")
			return
		}
		s.syncMode = string(mode)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, struct {
			RepositoryPath string          `json:"repositoryPath"`
			SyncMode       config.SyncMode `json:"syncMode"`
		}{RepositoryPath: s.libraryPath, SyncMode: mode})
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or PUT for synchronization settings.")
	}
}

func (s *Server) handleSyncSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read source inventory with GET.")
		return
	}
	service, _, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	inventory, groups, err := service.Inventory()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	globalUndoAvailable := s.latestResyncUndoAvailable(lifecycle.ResyncGlobal, nil)
	project := s.currentProject()
	projectUndoAvailable := project != nil && s.latestResyncUndoAvailable(lifecycle.ResyncProject, project)
	updateUndoAvailable := s.latestUpdateUndoAvailable(project)
	writeJSON(w, http.StatusOK, struct {
		RepositoryPath             string                     `json:"repositoryPath"`
		Locations                  []skillsync.SourceLocation `json:"locations"`
		Sources                    []skillsync.Source         `json:"sources"`
		Groups                     []skillsync.SourceGroup    `json:"groups"`
		Diagnostics                []skillsync.Diagnostic     `json:"diagnostics"`
		GlobalResyncUndoAvailable  bool                       `json:"globalResyncUndoAvailable"`
		ProjectResyncUndoAvailable bool                       `json:"projectResyncUndoAvailable"`
		UpdateUndoAvailable        bool                       `json:"updateUndoAvailable"`
	}{s.libraryPath, inventory.Locations, inventory.Sources, groups, inventory.Diagnostics, globalUndoAvailable, projectUndoAvailable, updateUndoAvailable})
}

func (s *Server) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Check upstream Skill updates with POST.")
		return
	}
	service, _, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := service.CheckUpdates(ctx)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpdatesPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Prepare selected upstream Skill updates with POST.")
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
	s.upstreamUpdateMu.Lock()
	defer s.upstreamUpdateMu.Unlock()
	s.discardSyncPlans(syncPlanUpdate)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	prepared, err := service.PrepareUpdates(ctx, request.SelectedSourceIDs)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanUpdate, upstreamPlan: prepared})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The update preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID  string                  `json:"planId"`
		Preview skillsync.UpdatePreview `json:"preview"`
	}{plan.id, prepared.Preview})
}

func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm selected upstream Skill updates with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	s.upstreamUpdateMu.Lock()
	defer s.upstreamUpdateMu.Unlock()
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanUpdate)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The update preview expired or was already used. Check and review again.")
		return
	}
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The update preview expired while waiting for another local operation. Check and review again.")
		return
	}
	service, project, err := s.newSkillSyncService()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	if project != nil {
		if err := project.validate(); err != nil {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed after update review.")
			return
		}
	}
	journal, err := s.newUpstreamJournal(project)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	service.Now = s.now
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	validated, err := service.RevalidateUpdates(ctx, plan.upstreamPlan)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			writeAPIError(w, http.StatusGatewayTimeout, "update_check_timeout", "Upstream revalidation did not finish in time. Check and review again.")
			return
		}
		writeSyncError(w, err)
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		writeAPIError(w, http.StatusGatewayTimeout, "update_check_timeout", "The update plan expired while waiting for another local operation. Check and review again.")
		return
	}
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The update preview expired while waiting for another local operation. Check and review again.")
		return
	}
	if project != nil {
		currentProject := s.currentProject()
		if currentProject == nil || currentProject.ID != project.ID || currentProject.validate() != nil {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed after update review.")
			return
		}
	}
	if err := service.ApplyRevalidatedUpdates(ctx, validated, journal); err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Updated int `json:"updated"`
	}{len(plan.upstreamPlan.Preview.Items)})
}

func (s *Server) handleUpdatesUndoPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create an update Undo preview with POST.")
		return
	}
	journal, err := s.newUpstreamJournal(s.currentProject())
	if err != nil {
		writeSyncError(w, err)
		return
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.Operation != upstreamUpdateOperation || entry.ResourceKind != "skill" {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "There is no upstream Skill update available to undo.")
		return
	}
	available, err := journal.UndoAvailable()
	if err != nil || !available {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "Updated Skills or lock metadata changed after the update.")
		return
	}
	undoPlan, err := journal.PreviewUndo()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanUpdateUndo, undoJournal: journal, undoPlan: undoPlan, undoEntry: entry})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The update Undo preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID string         `json:"planId"`
		Plan   operation.Plan `json:"plan"`
	}{plan.id, undoPlan})
}

func (s *Server) handleUpdatesUndoApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm update Undo with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanUpdateUndo)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The update Undo preview expired or was already used.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) || plan.undoJournal == nil || filepath.Clean(plan.undoJournal.Path) != filepath.Join(s.homeDir, ".agent-manager", "journal", "upstream-skills.json") {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The update Undo preview expired or its journal changed.")
		return
	}
	entry, found, err := plan.undoJournal.Latest()
	if err != nil || !found || !reflect.DeepEqual(entry, plan.undoEntry) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The latest upstream update changed after Undo review.")
		return
	}
	if err := plan.undoJournal.UndoLatest(func(current operation.Plan) bool { return reflect.DeepEqual(current, plan.undoPlan) }); err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{"undone"})
}

func (s *Server) latestUpdateUndoAvailable(project *registeredProject) bool {
	journal, err := s.newUpstreamJournal(project)
	if err != nil {
		return false
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.Operation != upstreamUpdateOperation || entry.ResourceKind != "skill" {
		return false
	}
	available, err := journal.UndoAvailable()
	return err == nil && available
}

func (s *Server) latestResyncUndoAvailable(scope lifecycle.ResyncScope, project *registeredProject) bool {
	_, journal, err := s.newLifecycleService(scope, project)
	if err != nil {
		return false
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.Operation != lifecycle.ResyncOperation || entry.ResourceKind != "skill" {
		return false
	}
	if scope == lifecycle.ResyncGlobal {
		err = validateGlobalJournalEntry(s.homeDir, entry)
	} else {
		err = validateConsoleUndoEntry(project, s.libraryPath, entry)
	}
	if err != nil {
		return false
	}
	available, err := journal.UndoAvailable()
	return err == nil && available
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create an import preview with POST.")
		return
	}
	var request struct {
		SelectedGroupIDs []string          `json:"selectedGroupIds"`
		CurrentChoices   map[string]string `json:"currentChoices,omitempty"`
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
	preview, err := service.Preview(request.SelectedGroupIDs, request.CurrentChoices)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanImport, importPreview: preview})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The synchronization preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID  string            `json:"planId"`
		Preview skillsync.Preview `json:"preview"`
	}{plan.id, preview})
}

func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm an import preview with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanImport)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The import preview expired or was already used. Review a new preview.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The import preview expired while waiting for another local operation. Review a new preview.")
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
	service.Now = s.now
	entries, err := service.ApplyUntil(plan.importPreview, plan.expiresAt)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Imported int               `json:"imported"`
		Entries  []skillrepo.Entry `json:"entries"`
	}{len(entries), entries})
}

func (s *Server) handleResyncPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create a resync preview with POST.")
		return
	}
	var request struct {
		Scope string `json:"scope"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	scope := lifecycle.ResyncScope(request.Scope)
	var root string
	var project *registeredProject
	if scope == lifecycle.ResyncGlobal {
		root = s.homeDir
	} else if scope == lifecycle.ResyncProject {
		project = s.currentProject()
		if project == nil {
			writeAPIError(w, http.StatusConflict, "project_required", "Register a project before resynchronizing its Skills.")
			return
		}
		if err := project.validate(); err != nil {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed.")
			return
		}
		root = project.Path
	} else {
		writeAPIError(w, http.StatusBadRequest, "invalid_scope", "Choose global or project scope.")
		return
	}
	service, _, err := s.newLifecycleService(scope, project)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	preview, err := service.PreviewResync(scope, root)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanResync, resync: preview})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The resync preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID string         `json:"planId"`
		Scope  string         `json:"scope"`
		Plan   operation.Plan `json:"plan"`
	}{plan.id, request.Scope, preview.Plan})
}

func (s *Server) handleResyncApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm a resync preview with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanResync)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The resync preview expired or was already used.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The resync preview expired while waiting for another local operation. Review a new preview.")
		return
	}
	var project *registeredProject
	if plan.resync.Scope == lifecycle.ResyncProject {
		project = s.currentProject()
		if project == nil || project.validate() != nil || filepath.Clean(project.Path) != filepath.Clean(plan.resync.Root) {
			writeAPIError(w, http.StatusConflict, "stale_project", "The registered project changed after preview.")
			return
		}
	}
	service, journal, err := s.newLifecycleService(plan.resync.Scope, project)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	unlock, err := journal.LockOperation()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	defer unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The resync preview expired while waiting for another local operation. Review a new preview.")
		return
	}
	service.BeforeFinalPublish = func() error {
		if !s.now().Before(plan.expiresAt) {
			return lifecycle.ErrPlanChanged
		}
		return nil
	}
	result, err := service.ApplyResync(plan.resync, true)
	if err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Operation string `json:"operation"`
		Changes   int    `json:"changes"`
	}{result.Operation, len(result.Changes)})
}

func (s *Server) handleResyncUndoPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Create a resync Undo preview with POST.")
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
			writeAPIError(w, http.StatusConflict, "project_required", "Register a current project before reviewing Undo.")
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
	if err != nil {
		writeSyncError(w, err)
		return
	}
	if !found {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "There is no operation to undo in this scope.")
		return
	}
	if entry.Operation != lifecycle.ResyncOperation || entry.ResourceKind != "skill" {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "The latest scope operation is not a Skill resynchronization.")
		return
	}
	if scope == lifecycle.ResyncGlobal {
		if err := validateGlobalJournalEntry(s.homeDir, entry); err != nil {
			writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The global operation journal is outside its safe boundary.")
			return
		}
	} else if err := validateConsoleUndoEntry(project, s.libraryPath, entry); err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The project operation is outside its safe Undo boundary.")
		return
	}
	available, err := journal.UndoAvailable()
	if err != nil || !available {
		writeAPIError(w, http.StatusConflict, "undo_unavailable", "The latest operation changed after it was recorded.")
		return
	}
	undoPlan, err := journal.PreviewUndo()
	if err != nil {
		writeSyncError(w, err)
		return
	}
	plan, err := s.storeSyncPlan(&storedSyncPlan{kind: syncPlanResyncUndo, undoScope: scope, undoJournal: journal, undoPlan: undoPlan, undoEntry: entry})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "plan_unavailable", "The Undo preview could not be stored in this session.")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		PlanID string         `json:"planId"`
		Plan   operation.Plan `json:"plan"`
	}{plan.id, undoPlan})
}

func (s *Server) handleResyncUndoApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Confirm resync Undo with POST.")
		return
	}
	var request struct {
		PlanID string `json:"planId"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	plan, ok := s.consumeSyncPlan(request.PlanID, syncPlanResyncUndo)
	if !ok {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The Undo preview expired or was already used.")
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if !s.now().Before(plan.expiresAt) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The Undo preview expired while waiting for another local operation. Review a new preview.")
		return
	}
	if plan.undoScope == lifecycle.ResyncProject {
		project := s.currentProject()
		if project == nil || project.validate() != nil || plan.undoJournal == nil || filepath.Clean(plan.undoJournal.Path) != filepath.Join(project.Path, ".skill-manager", "journal.json") {
			writeAPIError(w, http.StatusConflict, "stale_project", "The project changed after Undo preview.")
			return
		}
	}
	entry, found, err := plan.undoJournal.Latest()
	if err != nil || !found || !reflect.DeepEqual(entry, plan.undoEntry) {
		writeAPIError(w, http.StatusConflict, "stale_plan", "The latest operation changed after Undo preview.")
		return
	}
	if plan.undoScope == lifecycle.ResyncGlobal {
		if err := validateGlobalJournalEntry(s.homeDir, entry); err != nil {
			writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The global operation journal is outside its safe boundary.")
			return
		}
	}
	validateUndoEntry := plan.undoJournal.ValidateUndoEntry
	plan.undoJournal.ValidateUndoEntry = func(current operation.Entry) error {
		if validateUndoEntry != nil {
			if err := validateUndoEntry(current); err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(current, plan.undoEntry) {
			return lifecycle.ErrPlanChanged
		}
		return nil
	}
	beforeRestorePublish := plan.undoJournal.BeforeRestorePublish
	plan.undoJournal.BeforeRestorePublish = func(path string) error {
		if beforeRestorePublish != nil {
			if err := beforeRestorePublish(path); err != nil {
				return err
			}
		}
		if !s.now().Before(plan.expiresAt) {
			return operation.ErrUnexpectedState
		}
		return nil
	}
	if err := plan.undoJournal.UndoLatest(func(current operation.Plan) bool { return reflect.DeepEqual(current, plan.undoPlan) }); err != nil {
		writeSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{"undone"})
}

func (s *Server) newSkillSyncService() (*skillsync.Service, *registeredProject, error) {
	project := s.currentProject()
	if project != nil {
		if err := project.validate(); err != nil {
			return nil, project, err
		}
	}
	global := operation.GlobalJournal(s.homeDir)
	var projectJournal *operation.Journal
	if project != nil {
		var err error
		projectJournal, err = s.journalForProject(project)
		if err != nil {
			return nil, project, err
		}
	}
	service, err := skillsync.New(s.libraryPath, s.homeDir, pathForProject(project), global, projectJournal)
	return service, project, err
}

func (s *Server) newLifecycleService(scope lifecycle.ResyncScope, project *registeredProject) (*lifecycle.Service, *operation.Journal, error) {
	var journal *operation.Journal
	if scope == lifecycle.ResyncGlobal {
		journal = operation.GlobalJournal(s.homeDir)
		journal.ValidateEntry = func(entry operation.Entry) error {
			return validateGlobalJournalEntry(s.homeDir, entry)
		}
		journal.ValidateUndoEntry = func(entry operation.Entry) error {
			return validateGlobalJournalEntry(s.homeDir, entry)
		}
	} else if scope == lifecycle.ResyncProject {
		if project == nil {
			return nil, nil, fmt.Errorf("registered project is required")
		}
		if err := project.validate(); err != nil {
			return nil, nil, err
		}
		var err error
		journal, err = s.journalForProject(project)
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("unsupported resync scope %q", scope)
	}
	mode := adapter.MaterializationMode(s.currentSyncMode())
	service := lifecycle.New(s.libraryPath, journal, nil)
	service.Materialization = mode
	return service, journal, nil
}

func (s *Server) currentSyncMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.syncMode == "" {
		return string(config.SyncAuto)
	}
	return s.syncMode
}

func pathForProject(project *registeredProject) string {
	if project == nil {
		return ""
	}
	return project.Path
}

func (s *Server) storeSyncPlan(plan *storedSyncPlan) (*storedSyncPlan, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	plan.id = hex.EncodeToString(value)
	now := s.now()
	plan.expiresAt = now.Add(syncPlanTTL)
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	for id, existing := range s.syncPlans {
		if !now.Before(existing.expiresAt) {
			delete(s.syncPlans, id)
		}
	}
	if plan.kind == syncPlanUpdate {
		for id, existing := range s.syncPlans {
			if existing.kind == syncPlanUpdate {
				delete(s.syncPlans, id)
			}
		}
	}
	s.syncPlans[plan.id] = plan
	return plan, nil
}

func (s *Server) discardSyncPlans(kind syncPlanKind) {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	for id, existing := range s.syncPlans {
		if existing.kind == kind {
			delete(s.syncPlans, id)
		}
	}
}

func (s *Server) consumeSyncPlan(id string, kind syncPlanKind) (*storedSyncPlan, bool) {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	plan, exists := s.syncPlans[id]
	if !exists || plan.kind != kind || !s.now().Before(plan.expiresAt) {
		delete(s.syncPlans, id)
		return nil, false
	}
	delete(s.syncPlans, id)
	return plan, true
}

func writeSyncError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeAPIError(w, http.StatusGatewayTimeout, "sync_timeout", "The sync operation did not finish before its request ended.")
	case errors.Is(err, skillsync.ErrUpdatePlanTooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "update_plan_too_large", err.Error())
	case errors.Is(err, skillsync.ErrStalePreview), errors.Is(err, skillsync.ErrNeedsChoice), errors.Is(err, skillsync.ErrStaleUpdatePlan), errors.Is(err, skillrepo.ErrConflict), errors.Is(err, skillrepo.ErrUnsafe), errors.Is(err, lifecycle.ErrPlanChanged), errors.Is(err, operation.ErrUnexpectedState), errors.Is(err, operation.ErrUnsafePath):
		writeAPIError(w, http.StatusConflict, "stale_plan", err.Error())
	case errors.Is(err, skillsync.ErrInvalidSelection), errors.Is(err, skillsync.ErrInvalidUpdateSelection), errors.Is(err, skillsync.ErrUnsafeSource), errors.Is(err, lifecycle.ErrUnsafePath):
		writeAPIError(w, http.StatusBadRequest, "invalid_sync_request", err.Error())
	default:
		writeAPIError(w, http.StatusInternalServerError, "sync_failed", err.Error())
	}
}

func validateGlobalJournalEntry(home string, entry operation.Entry) error {
	if len(entry.Before) == 0 || len(entry.Before) != len(entry.After) {
		return lifecycle.ErrUnsafePath
	}
	backupRoot := filepath.Join(home, ".agent-manager", "journal", ".skill-manager-journal")
	before, err := validateGlobalSnapshots(home, backupRoot, entry.Before)
	if err != nil {
		return err
	}
	after, err := validateGlobalSnapshots(home, backupRoot, entry.After)
	if err != nil || len(before) != len(after) {
		return lifecycle.ErrUnsafePath
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			return lifecycle.ErrUnsafePath
		}
	}
	return nil
}

func validateGlobalSnapshots(home, backupRoot string, snapshots []operation.Snapshot) (map[string]struct{}, error) {
	paths := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		if !globalSkillDestination(home, snapshot.Path) {
			return nil, lifecycle.ErrUnsafePath
		}
		if _, duplicate := paths[snapshot.Path]; duplicate {
			return nil, lifecycle.ErrUnsafePath
		}
		paths[snapshot.Path] = struct{}{}
		if err := validateGlobalSnapshotParents(home, snapshot.Path); err != nil {
			return nil, err
		}
		if !snapshot.Exists {
			if snapshot.Backup != "" {
				return nil, lifecycle.ErrUnsafePath
			}
			continue
		}
		backup := snapshot.Backup
		if !filepath.IsAbs(backup) || filepath.Clean(backup) != backup || filepath.Dir(backup) != backupRoot || filepath.Base(backup) == "." {
			return nil, lifecycle.ErrUnsafePath
		}
		if err := validateRealDirectoryChain(backupRoot); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(backup); err != nil {
			return nil, lifecycle.ErrUnsafePath
		}
	}
	return paths, nil
}

func validateRealDirectoryChain(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Clean(absolute) != absolute {
		return lifecycle.ErrUnsafePath
	}
	volume := filepath.VolumeName(absolute)
	current := string(filepath.Separator)
	if volume != "" {
		current = volume + string(filepath.Separator)
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(absolute, volume), string(filepath.Separator))
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return lifecycle.ErrUnsafePath
		}
	}
	return nil
}

func validateGlobalSnapshotParents(home, destination string) error {
	if !globalSkillDestination(home, destination) {
		return lifecycle.ErrUnsafePath
	}
	for _, target := range adapter.Supported() {
		agent, ok := adapter.ForAgent(target.Target())
		if !ok {
			continue
		}
		root := filepath.Dir(agent.GlobalSkillPath(home, "placeholder"))
		if filepath.Dir(destination) != root {
			continue
		}
		for current := root; current != filepath.Dir(current); current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return lifecycle.ErrUnsafePath
			}
		}
		return nil
	}
	return lifecycle.ErrUnsafePath
}

func globalSkillDestination(home, candidate string) bool {
	if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return false
	}
	for _, target := range adapter.Supported() {
		agent, ok := adapter.ForAgent(target.Target())
		if !ok {
			continue
		}
		root := filepath.Dir(agent.GlobalSkillPath(home, "placeholder"))
		if filepath.Dir(candidate) == root && adapter.ValidateIdentifier(filepath.Base(candidate)) == nil {
			return true
		}
	}
	return false
}
