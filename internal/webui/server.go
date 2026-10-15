// Package webui provides the local Skill installation API module.
package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/recommend"
	"github.com/AllenMuu/skill-manager/internal/search"
	"github.com/AllenMuu/skill-manager/internal/stack"
)

type server struct {
	project  string
	library  string
	session  string
	installs *installation.Service
}

// Module exposes Skill installation APIs inside an authenticated local Web
// server, sharing that server's session token.
type Module struct {
	server *server
	api    http.Handler
}

// NewModule prepares the installation APIs to be mounted by the main Web
// console. The console authenticates requests before dispatching to its API.
func NewModule(project, library, session string) (*Module, error) {
	return NewModuleWithMaterialization(project, library, session, nil)
}

// NewModuleWithMaterialization reads the parent console's synchronized setting
// for each guarded preview and apply without mutating shared service options.
func NewModuleWithMaterialization(project, library, session string, mode func() adapter.MaterializationMode) (*Module, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(project)
	if err != nil {
		return nil, fmt.Errorf("inspect project %s: %w", project, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path %s is not a directory", project)
	}
	if session == "" {
		session, err = randomToken()
		if err != nil {
			return nil, err
		}
	}
	installs := installation.NewWithMaterializationProvider(library, mode)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		installs.BatchStore = operation.NewBatchStore(filepath.Join(home, ".agent-manager", "skill-sync", "batches"))
	}
	module := &Module{server: &server{
		project:  project,
		library:  library,
		session:  session,
		installs: installs,
	}}
	module.api = module.server.apiHandler()
	return module, nil
}

// APIHandler exposes only installation API routes for a parent server to mount
// after it has authenticated and validated the request origin.
func (m *Module) APIHandler() http.Handler {
	return m.api
}

func (s *server) apiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog", s.catalog)
	mux.HandleFunc("/api/recommend", s.recommend)
	mux.HandleFunc("/api/install/preview", s.preview)
	mux.HandleFunc("/api/install/apply", s.apply)
	return mux
}

func (s *server) catalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	skills, _, err := catalog.Discover(s.library)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) != "" {
		skills = search.Match(query, skills)
	}
	items := make([]catalogItem, 0, len(skills))
	for _, skill := range skills {
		items = append(items, catalogItem{
			Identifier: skill.Identifier, Name: skill.Name, Description: skill.Description,
			Tags: nonNil(skill.Tags), Compatibility: nonNil(skill.Compatibility),
			Source: skill.Source, SourceType: skill.SourceType, Publisher: skill.Publisher, ImmutableRef: skill.ImmutableRef,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items, "targets": availableTargets()})
}

type catalogItem struct {
	Identifier    string   `json:"identifier"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Compatibility []string `json:"compatibility"`
	Source        string   `json:"source,omitempty"`
	SourceType    string   `json:"sourceType,omitempty"`
	Publisher     string   `json:"publisher,omitempty"`
	ImmutableRef  string   `json:"immutableRef,omitempty"`
}

type targetOption struct {
	ID    adapter.Target `json:"id"`
	Label string         `json:"label"`
}

func availableTargets() []targetOption {
	labels := map[adapter.Target]string{
		adapter.ClaudeCode: "Claude Code",
		adapter.Codex:      "Codex",
		adapter.Pi:         "Pi",
	}
	targets := make([]targetOption, 0, len(adapter.Supported()))
	for _, target := range adapter.Supported() {
		targets = append(targets, targetOption{ID: target.Target(), Label: labels[target.Target()]})
	}
	return targets
}

type recommendationResult struct {
	Project      string                `json:"project"`
	ScanComplete bool                  `json:"scanComplete"`
	Diagnostics  []stack.Diagnostic    `json:"diagnostics"`
	Scopes       []recommendationScope `json:"scopes"`
}

type recommendationScope struct {
	Path            string               `json:"path"`
	Status          recommend.Status     `json:"status"`
	Technologies    []stack.Technology   `json:"technologies"`
	Evidence        []stack.Evidence     `json:"evidence"`
	Diagnostics     []stack.Diagnostic   `json:"diagnostics"`
	Recommendations []recommendationItem `json:"recommendations"`
	NextAction      recommend.Action     `json:"nextAction"`
}

type recommendationItem struct {
	Identifier       string               `json:"identifier"`
	Description      string               `json:"description"`
	MatchReason      string               `json:"matchReason"`
	MatchingEvidence []recommend.Match    `json:"matchingEvidence"`
	Confidence       recommend.Confidence `json:"confidence"`
}

func (s *server) recommend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	result, err := recommend.Recommend(s.project, s.library)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	out := recommendationResult{Project: result.Project, ScanComplete: result.ScanComplete, Diagnostics: result.Diagnostics, Scopes: make([]recommendationScope, 0, len(result.Scopes))}
	for _, scope := range result.Scopes {
		item := recommendationScope{Path: scope.Path, Status: scope.Status, Technologies: scope.Technologies, Evidence: scope.Evidence, Diagnostics: scope.Diagnostics, Recommendations: make([]recommendationItem, 0, len(scope.Recommendations)), NextAction: scope.NextAction}
		for _, rec := range scope.Recommendations {
			item.Recommendations = append(item.Recommendations, recommendationItem{Identifier: rec.Identifier, Description: rec.Description, MatchReason: rec.MatchReason, MatchingEvidence: rec.MatchingEvidence, Confidence: rec.Confidence})
		}
		out.Scopes = append(out.Scopes, item)
	}
	writeJSON(w, http.StatusOK, out)
}

type previewRequest struct {
	SkillIDs         []string         `json:"skillIds"`
	Targets          []adapter.Target `json:"targets"`
	ReplaceConflicts bool             `json:"replaceConflicts"`
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input previewRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request := installation.Request{Project: s.project, SkillIDs: input.SkillIDs, Targets: input.Targets}
	if input.ReplaceConflicts {
		request.Options = lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	}
	preview, err := s.installs.Preview(s.session, request)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, lifecycle.ErrForceRequired) || errors.Is(err, lifecycle.ErrUnsafePath) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error(), "preview": preview})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preview": preview})
}

type applyRequest struct {
	PreviewID string `json:"previewId"`
}

func (s *server) apply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input applyRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.installs.Apply(s.session, input.PreviewID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if result.Stale {
		writeJSON(w, http.StatusConflict, map[string]any{"stale": true, "result": result})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create WebUI session token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
