package stack

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxEntries is the traversal budget for one scan.
const maxEntries = 10000

// Evidence links one detected technology to its scope-relative marker path.
type Evidence struct {
	Path       string `json:"path"`
	Technology string `json:"technology"`
}

// Diagnostic reports a localized, non-fatal observation from a scan.
type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Scope is one independently evaluated project scope with its detected
// technology evidence.
type Scope struct {
	Path         string
	Technologies []string
	Evidence     []Evidence
	Diagnostics  []Diagnostic
}

// Result is the outcome of a static project scan.
type Result struct {
	Project      string
	Scopes       []Scope
	ScanComplete bool
	Diagnostics  []Diagnostic
}

type scanState struct {
	project      string
	boundaries   map[string]bool
	evidence     map[string][]Evidence
	diagnostics  map[string][]Diagnostic
	entries      int
	limitReached bool
	complete     bool
}

// Scan statically discovers technology scopes beneath root. It never executes
// content, follows directory soft links, or reads more than maxEntries
// directory entries and maxMarkerBytes bytes per marker.
func Scan(root string) (Result, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect project %s: %w", root, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("project %s is not a directory", root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	state := &scanState{
		project:     abs,
		boundaries:  map[string]bool{},
		evidence:    map[string][]Evidence{},
		diagnostics: map[string][]Diagnostic{},
		complete:    true,
	}
	scanDirectory(state, abs)
	result := Result{Project: abs, ScanComplete: state.complete}
	if state.limitReached {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Path:    ".",
			Message: fmt.Sprintf("traversal stopped after inspecting %d directory entries", maxEntries),
		})
		result.ScanComplete = false
	}
	scopeDirs := []string{abs}
	for dir := range state.boundaries {
		if dir != abs {
			scopeDirs = append(scopeDirs, dir)
		}
	}
	sort.Slice(scopeDirs, func(i, j int) bool {
		return scopeRelative(state.project, scopeDirs[i]) < scopeRelative(state.project, scopeDirs[j])
	})
	scopeSet := map[string]bool{}
	for _, dir := range scopeDirs {
		scopeSet[dir] = true
	}
	// Non-boundary markers belong to their nearest enclosing project scope.
	for dir, evidence := range state.evidence {
		if scopeSet[dir] {
			continue
		}
		scopeDir := closestScope(dir, scopeSet)
		for _, item := range evidence {
			item.Path = scopeRelative(scopeDir, filepath.Join(dir, item.Path))
			state.evidence[scopeDir] = append(state.evidence[scopeDir], item)
		}
	}
	// Bubbles every diagnostic up to its closest enclosing scope so skipped
	// links and unreadable markers in non-scope directories stay visible.
	for dir, diagnostics := range state.diagnostics {
		if scopeSet[dir] {
			continue
		}
		for _, d := range diagnostics {
			scopeDir := closestScope(dir, scopeSet)
			if scopeDir == "" {
				result.Diagnostics = append(result.Diagnostics, d)
				continue
			}
			state.diagnostics[scopeDir] = append(state.diagnostics[scopeDir], d)
		}
	}
	for _, dir := range scopeDirs {
		result.Scopes = append(result.Scopes, buildScope(state, dir))
	}
	return result, nil
}

// closestScope walks up from dir to the nearest enclosing scope directory.
func closestScope(dir string, scopes map[string]bool) string {
	for {
		if scopes[dir] {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func scanDirectory(state *scanState, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		state.complete = false
		state.recordDiagnostic(dir, ".", fmt.Sprintf("unreadable directory: %v", err))
		return
	}
	for _, entry := range entries {
		state.entries++
		if state.entries > maxEntries {
			state.limitReached = true
			return
		}
		name := entry.Name()
		if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 && excludedDirectories[name] {
			continue
		}
		if m, ok := markerByName(name); ok {
			if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
				// Directory markers (.claude and friends) are recorded without
				// traversal; a linked marker also counts as its presence.
				state.recordMarker(dir, name, m, nil)
				continue
			}
			if m.scopeBoundary {
				state.boundaries[dir] = true
			}
			state.readFileMarker(dir, name, m)
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			state.complete = false
			state.recordDiagnostic(dir, name, "skipped directory soft link")
			continue
		}
		if entry.IsDir() {
			scanDirectory(state, filepath.Join(dir, name))
		}
	}
}

func (s *scanState) readFileMarker(dir, name string, m marker) {
	path := filepath.Join(dir, name)
	file, err := os.Open(path)
	if err != nil {
		s.complete = false
		s.recordDiagnostic(dir, name, fmt.Sprintf("unreadable marker: %v", err))
		return
	}
	defer file.Close()
	contents := make([]byte, maxMarkerBytes+1)
	n, err := io.ReadFull(file, contents)
	if err != nil && err != io.ErrUnexpectedEOF {
		s.complete = false
		s.recordDiagnostic(dir, name, fmt.Sprintf("unreadable marker: %v", err))
		return
	}
	if n > maxMarkerBytes {
		s.complete = false
		s.recordDiagnostic(dir, name, "marker exceeds the 1 MiB static read limit and was not parsed")
		return
	}
	s.recordMarker(dir, name, m, contents[:n])
}

func (s *scanState) recordMarker(dir, name string, m marker, contents []byte) {
	for _, id := range m.parse(contents) {
		s.evidence[dir] = append(s.evidence[dir], Evidence{Path: name, Technology: id})
	}
}

func (s *scanState) recordDiagnostic(dir, name, message string) {
	s.diagnostics[dir] = append(s.diagnostics[dir], Diagnostic{Path: name, Message: message})
}

func buildScope(state *scanState, dir string) Scope {
	rel := scopeRelative(state.project, dir)
	scope := Scope{Path: rel}
	seen := map[string]bool{}
	for _, e := range state.evidence[dir] {
		scope.Evidence = append(scope.Evidence, e)
		if !seen[e.Technology] {
			seen[e.Technology] = true
			scope.Technologies = append(scope.Technologies, e.Technology)
		}
	}
	sort.Slice(scope.Evidence, func(i, j int) bool {
		if scope.Evidence[i].Path != scope.Evidence[j].Path {
			return scope.Evidence[i].Path < scope.Evidence[j].Path
		}
		return scope.Evidence[i].Technology < scope.Evidence[j].Technology
	})
	sort.Strings(scope.Technologies)
	scope.Diagnostics = append(scope.Diagnostics, state.diagnostics[dir]...)
	sort.Slice(scope.Diagnostics, func(i, j int) bool {
		if scope.Diagnostics[i].Path != scope.Diagnostics[j].Path {
			return scope.Diagnostics[i].Path < scope.Diagnostics[j].Path
		}
		return scope.Diagnostics[i].Message < scope.Diagnostics[j].Message
	})
	return scope
}

func scopeRelative(project, dir string) string {
	rel, err := filepath.Rel(project, dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

// NormalizeToken lowercases and trims one free-text token for matching.
func NormalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
