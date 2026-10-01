// Package webui serves the embedded local installation interface.
package webui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/recommend"
	"github.com/AllenMuu/skill-manager/internal/search"
	"github.com/AllenMuu/skill-manager/internal/stack"
)

//go:embed dist
var embedded embed.FS

type server struct {
	project   string
	library   string
	authority string
	session   string
	installs  *installation.Service
	assets    fs.FS
}

// NewLocalServer binds to IPv4 loopback and returns its URL and HTTP server.
// The listener is returned separately so callers control serving and shutdown.
func NewLocalServer(project, library string) (string, *http.Server, net.Listener, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return "", nil, nil, err
	}
	info, err := os.Stat(project)
	if err != nil {
		return "", nil, nil, fmt.Errorf("inspect project %s: %w", project, err)
	}
	if !info.IsDir() {
		return "", nil, nil, fmt.Errorf("project path %s is not a directory", project)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, fmt.Errorf("listen on loopback: %w", err)
	}
	session, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return "", nil, nil, err
	}
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		_ = listener.Close()
		return "", nil, nil, err
	}
	state := &server{
		project:   project,
		library:   library,
		authority: listener.Addr().String(),
		session:   session,
		installs:  installation.New(library),
		assets:    assets,
	}
	return "http://" + state.authority, &http.Server{Handler: state.handler()}, listener, nil
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog", s.catalog)
	mux.HandleFunc("/api/recommend", s.recommend)
	mux.HandleFunc("/api/install/preview", s.preview)
	mux.HandleFunc("/api/install/apply", s.apply)
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		if r.Host != s.authority || !remoteIsLoopback(r.RemoteAddr) {
			http.Error(w, "local requests only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.authority {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("X-Agent-Manager-Session") != s.session {
			http.Error(w, "session is not active", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
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
		items = append(items, catalogItem{Identifier: skill.Identifier, Name: skill.Name, Description: skill.Description, Tags: nonNil(skill.Tags), Compatibility: nonNil(skill.Compatibility)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items, "targets": availableTargets()})
}

type catalogItem struct {
	Identifier    string   `json:"identifier"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Compatibility []string `json:"compatibility"`
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

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	name := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
	if name == "." || name == "" || name == "index.html" {
		contents, err := fs.ReadFile(s.assets, "index.html")
		if err != nil {
			http.Error(w, "embedded WebUI is unavailable", http.StatusInternalServerError)
			return
		}
		contents = []byte(strings.ReplaceAll(string(contents), "__SESSION_TOKEN__", s.session))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(contents)
		return
	}
	http.FileServer(http.FS(s.assets)).ServeHTTP(w, r)
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

func remoteIsLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
