// Package installation coordinates catalog selection, guarded previews, and
// Global or Project Skill activation for CLI and WebUI clients.
package installation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

var (
	ErrInvalidRequest = errors.New("invalid installation request")
	ErrPreviewExpired = errors.New("installation preview expired or unavailable")
)

const previewLifetime = 5 * time.Minute

// Request identifies Skills, activation scope, and target agents selected by
// the operator. Project holds the project root or, for Global, the user's home.
type Request struct {
	Project  string            `json:"project"`
	Global   bool              `json:"global,omitempty"`
	SkillIDs []string          `json:"skillIds"`
	Versions map[string]string `json:"versions,omitempty"`
	Targets  []adapter.Target  `json:"targets"`
	Options  lifecycle.Options `json:"options"`
}

// Preview is the immutable plan shown to an operator before installation.
type Preview struct {
	ID       string            `json:"id"`
	Project  string            `json:"project"`
	Global   bool              `json:"global,omitempty"`
	SkillIDs []string          `json:"skillIds"`
	Versions map[string]string `json:"versions,omitempty"`
	Targets  []adapter.Target  `json:"targets"`
	Plan     operation.Plan    `json:"plan"`
}

// ApplyResult distinguishes a completed operation from a stale preview.
type ApplyResult struct {
	Applied bool           `json:"applied"`
	Stale   bool           `json:"stale"`
	Plan    operation.Plan `json:"plan"`
}

type pendingPreview struct {
	session string
	request Request
	plan    operation.Plan
	state   [32]byte
	expires time.Time
}

// Service owns short-lived installation previews shared by interface adapters.
type Service struct {
	LibraryPath     string
	Materialization adapter.MaterializationMode

	mu      sync.Mutex
	pending map[string]pendingPreview
}

// New constructs a local installation application service.
func New(libraryPath string) *Service {
	return NewWithMaterialization(libraryPath, adapter.MaterializeAuto)
}

// NewWithMaterialization constructs an installation service using the
// machine-local Skill synchronization setting.
func NewWithMaterialization(libraryPath string, mode adapter.MaterializationMode) *Service {
	if mode == "" {
		mode = adapter.MaterializeAuto
	}
	return &Service{LibraryPath: libraryPath, Materialization: mode, pending: make(map[string]pendingPreview)}
}

// Preview validates a request and creates a session-scoped, short-lived plan.
// It performs no writes. A conflict may return a populated plan with an error
// so callers can display the blocked destination and request new options.
func (s *Service) Preview(session string, request Request) (Preview, error) {
	if session == "" {
		return Preview{}, fmt.Errorf("%w: missing session", ErrInvalidRequest)
	}
	normalized, skills, err := s.resolve(request)
	if err != nil {
		return Preview{}, err
	}
	life := lifecycle.New(s.LibraryPath, journal(normalized.Project, normalized.Global), nil)
	life.Materialization = s.Materialization
	plan, err := previewLifecycle(life, normalized, skills)
	preview := Preview{
		Project:  normalized.Project,
		Global:   normalized.Global,
		SkillIDs: append([]string{}, normalized.SkillIDs...),
		Versions: cloneVersions(normalized.Versions),
		Targets:  append([]adapter.Target{}, normalized.Targets...),
		Plan:     plan,
	}
	if err != nil {
		return preview, err
	}
	if len(plan.Changes) == 0 {
		return preview, fmt.Errorf("%w: no installation changes", ErrInvalidRequest)
	}
	state, err := filesystemState(normalized, skills)
	if err != nil {
		return Preview{}, fmt.Errorf("capture installation preview state: %w", err)
	}
	id, err := newPreviewID()
	if err != nil {
		return Preview{}, err
	}
	preview.ID = id
	s.mu.Lock()
	s.expireLocked(time.Now())
	s.pending[id] = pendingPreview{session: session, request: normalized, plan: plan, state: state, expires: time.Now().Add(previewLifetime)}
	s.mu.Unlock()
	return preview, nil
}

// Apply commits a session-owned preview if the current filesystem plan still
// matches the plan the operator confirmed.
func (s *Service) Apply(session, previewID string) (ApplyResult, error) {
	s.mu.Lock()
	s.expireLocked(time.Now())
	pending, ok := s.pending[previewID]
	if !ok || session == "" || pending.session != session {
		s.mu.Unlock()
		return ApplyResult{}, ErrPreviewExpired
	}
	delete(s.pending, previewID)
	s.mu.Unlock()

	operationJournal := journal(pending.request.Project, pending.request.Global)
	unlock, err := operationJournal.LockOperation()
	if err != nil {
		return ApplyResult{}, err
	}
	defer unlock()

	normalized, skills, err := s.resolve(pending.request)
	if err != nil {
		return ApplyResult{}, err
	}
	life := lifecycle.New(s.LibraryPath, operationJournal, nil)
	life.Materialization = s.Materialization
	state, err := filesystemState(normalized, skills)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("inspect installation preview state: %w", err)
	}
	if state != pending.state {
		plan, _ := previewLifecycle(life, normalized, skills)
		return ApplyResult{Stale: true, Plan: plan}, nil
	}
	plan, err := applyLifecycle(life, normalized, skills, pending.plan)
	if errors.Is(err, lifecycle.ErrPlanChanged) {
		return ApplyResult{Stale: true, Plan: plan}, nil
	}
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}
	return ApplyResult{Applied: true, Plan: plan}, nil
}

func filesystemState(request Request, skills []catalog.Skill) ([32]byte, error) {
	digest := sha256.New()
	paths := make([]string, 0, len(skills)*2*len(request.Targets))
	fullTree := make(map[string]bool, len(skills)+len(skills)*len(request.Targets))
	parentPaths := make(map[string]bool, len(skills)*len(request.Targets))
	for _, skill := range skills {
		paths = append(paths, skill.SourcePath)
		fullTree[skill.SourcePath] = true
		for _, target := range request.Targets {
			a, _ := adapter.For(target)
			destination := a.ProjectSkillPath(request.Project, skill.Identifier)
			if request.Global {
				destination = a.GlobalSkillPath(request.Project, skill.Identifier)
			}
			paths = append(paths, destination)
			fullTree[destination] = true
			for parent := filepath.Dir(destination); parent != request.Project && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
				paths = append(paths, parent)
				parentPaths[parent] = true
			}
		}
	}
	sort.Strings(paths)
	previous := ""
	for _, path := range paths {
		if path == previous {
			continue
		}
		previous = path
		writeHashString(digest, path)
		var err error
		if parentPaths[path] && !fullTree[path] {
			err = hashParentPath(digest, path)
		} else {
			err = hashPath(digest, path, fullTree[path])
		}
		if err != nil {
			return [32]byte{}, err
		}
	}
	var result [32]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func previewLifecycle(life *lifecycle.Service, request Request, skills []catalog.Skill) (operation.Plan, error) {
	if request.Global {
		return life.PreviewGlobalMany(request.Project, skills, request.Targets, request.Options)
	}
	return life.PreviewMany(request.Project, skills, request.Targets, request.Options)
}

func applyLifecycle(life *lifecycle.Service, request Request, skills []catalog.Skill, expected operation.Plan) (operation.Plan, error) {
	if request.Global {
		return life.ApplyGlobalMany(request.Project, skills, request.Targets, expected, true, request.Options)
	}
	return life.ApplyMany(request.Project, skills, request.Targets, expected, true, request.Options)
}

func hashParentPath(digest hash.Hash, path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		// A missing parent and a directory created by another installation are
		// equivalent for the selected destination.
		writeHashString(digest, "parent-directory")
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		writeHashString(digest, "parent-symlink:"+target)
		return nil
	}
	if info.IsDir() {
		writeHashString(digest, "parent-directory")
		return nil
	}
	writeHashString(digest, fmt.Sprintf("parent-other:%o:%d", info.Mode(), info.Size()))
	return nil
}

func hashPath(digest hash.Hash, path string, descend bool) error {
	return hashPathActive(digest, path, descend, make(map[string]bool))
}

func hashPathActive(digest hash.Hash, path string, descend bool, active map[string]bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		writeHashString(digest, "missing")
		return nil
	}
	if err != nil {
		return err
	}
	size := info.Size()
	if info.IsDir() {
		// Directory sizes can change when unrelated siblings are installed.
		size = 0
	}
	writeHashString(digest, fmt.Sprintf("mode:%o:size:%d", info.Mode(), size))
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		writeHashString(digest, "symlink:"+target)
		if descend {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve skill symlink %s: %w", path, err)
			}
			return hashPathActive(digest, resolved, true, active)
		}
	case info.IsDir() && descend:
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve skill directory %s: %w", path, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return err
		}
		if active[resolved] {
			writeHashString(digest, "directory-cycle:"+resolved)
			return nil
		}
		active[resolved] = true
		defer delete(active, resolved)
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			writeHashString(digest, entry.Name())
			if err := hashPathActive(digest, filepath.Join(path, entry.Name()), true, active); err != nil {
				return err
			}
		}
	case info.Mode().IsRegular() && descend:
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func writeHashString(digest hash.Hash, value string) {
	_, _ = io.WriteString(digest, value)
	_, _ = digest.Write([]byte{0})
}

func (s *Service) resolve(request Request) (Request, []catalog.Skill, error) {
	if len(request.SkillIDs) == 0 {
		return Request{}, nil, fmt.Errorf("%w: select at least one Skill", ErrInvalidRequest)
	}
	if len(request.Targets) == 0 {
		return Request{}, nil, fmt.Errorf("%w: select at least one target agent", ErrInvalidRequest)
	}
	projectRoot := request.Project
	if request.Global {
		home, err := os.UserHomeDir()
		if err != nil {
			return Request{}, nil, fmt.Errorf("resolve current user's home directory: %w", err)
		}
		home, err = filepath.Abs(home)
		if err != nil {
			return Request{}, nil, fmt.Errorf("resolve current user's home directory: %w", err)
		}
		if projectRoot != "" {
			requestedRoot, err := filepath.Abs(projectRoot)
			if err != nil || filepath.Clean(requestedRoot) != filepath.Clean(home) {
				return Request{}, nil, fmt.Errorf("%w: global scope must use the current user's home directory", ErrInvalidRequest)
			}
		}
		projectRoot = home
	}
	project, err := filepath.Abs(projectRoot)
	if err != nil {
		return Request{}, nil, fmt.Errorf("resolve installation root: %w", err)
	}
	info, err := os.Stat(project)
	if err != nil {
		return Request{}, nil, fmt.Errorf("inspect installation root %s: %w", project, err)
	}
	if !info.IsDir() {
		return Request{}, nil, fmt.Errorf("installation root %s is not a directory", project)
	}
	if _, err := os.ReadDir(project); err != nil {
		return Request{}, nil, fmt.Errorf("read installation root %s: %w", project, err)
	}
	if request.Options.Conflict != "" && request.Options.Conflict != lifecycle.ConflictReplace {
		return Request{}, nil, fmt.Errorf("%w: unsupported conflict strategy %q", ErrInvalidRequest, request.Options.Conflict)
	}
	seenTargets := make(map[adapter.Target]bool, len(request.Targets))
	for _, target := range request.Targets {
		if _, ok := adapter.For(target); !ok {
			return Request{}, nil, fmt.Errorf("unsupported target %q", target)
		}
		if seenTargets[target] {
			return Request{}, nil, fmt.Errorf("%w: duplicate target %q", ErrInvalidRequest, target)
		}
		seenTargets[target] = true
	}
	seenIDs := make(map[string]bool, len(request.SkillIDs))
	for _, id := range request.SkillIDs {
		if id == "" || seenIDs[id] {
			return Request{}, nil, fmt.Errorf("%w: empty or duplicate Skill identifier %q", ErrInvalidRequest, id)
		}
		seenIDs[id] = true
	}
	for id, version := range request.Versions {
		if !seenIDs[id] || strings.TrimSpace(version) != version || version == "" {
			return Request{}, nil, fmt.Errorf("%w: version selection must name a selected Skill and a non-empty version", ErrInvalidRequest)
		}
	}
	skills, _, err := catalog.Discover(s.LibraryPath)
	if err != nil {
		return Request{}, nil, err
	}
	byID := make(map[string]catalog.Skill, len(skills))
	for _, skill := range skills {
		byID[skill.Identifier] = skill
	}
	selected := make([]catalog.Skill, 0, len(request.SkillIDs))
	selectedVersions := make(map[string]string, len(request.SkillIDs))
	handler := resource.NewSkillHandler()
	for _, id := range request.SkillIDs {
		version := request.Versions[id]
		var skill catalog.Skill
		if version != "" {
			resolved, resolveErr := handler.Resolve(s.LibraryPath, id, version)
			if resolveErr != nil {
				return Request{}, nil, fmt.Errorf("resolve Skill %q version %q: %w", id, version, resolveErr)
			}
			skill = resolved
		} else {
			var ok bool
			skill, ok = byID[id]
			if !ok {
				return Request{}, nil, fmt.Errorf("Skill %q not found in configured library", id)
			}
		}
		selectedVersions[id] = skill.VersionKey
		selected = append(selected, skill)
	}
	request.Project = project
	request.SkillIDs = append([]string{}, request.SkillIDs...)
	request.Versions = selectedVersions
	request.Targets = append([]adapter.Target{}, request.Targets...)
	return request, selected, nil
}

func cloneVersions(versions map[string]string) map[string]string {
	if len(versions) == 0 {
		return nil
	}
	copy := make(map[string]string, len(versions))
	for identifier, version := range versions {
		copy[identifier] = version
	}
	return copy
}

func (s *Service) expireLocked(now time.Time) {
	for id, pending := range s.pending {
		if !pending.expires.After(now) {
			delete(s.pending, id)
		}
	}
}

func journal(project string, global bool) *operation.Journal {
	if global {
		return operation.GlobalJournal(project)
	}
	return operation.ProjectJournal(project)
}

func newPreviewID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create installation preview id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
