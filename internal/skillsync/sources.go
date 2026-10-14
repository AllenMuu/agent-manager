// Package skillsync discovers local Skills and coordinates guarded imports.
package skillsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

var ErrUnsafeSource = errors.New("unsafe Skill source")

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// Source describes one Skill candidate and the supported location that owns it.
type Source struct {
	ID              string `json:"id"`
	Identifier      string `json:"identifier"`
	Scope           Scope  `json:"scope"`
	Location        string `json:"location"`
	Path            string `json:"path"`
	Checksum        string `json:"checksum,omitempty"`
	Eligible        bool   `json:"eligible"`
	Problem         string `json:"problem,omitempty"`
	AlreadyManaged  bool   `json:"alreadyManaged,omitempty"`
	ManagedMaterial string `json:"managedMaterial,omitempty"`
}

type SourceLocation struct {
	Scope Scope  `json:"scope"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

type Diagnostic struct {
	Scope   Scope  `json:"scope"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Inventory struct {
	Locations   []SourceLocation `json:"locations"`
	Sources     []Source         `json:"sources"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
}

// Discover inspects only adapter-owned and neutral Skills directories below
// the supplied home and registered project. It never accepts a source path.
func Discover(home, project, repository string, globalJournal, projectJournal *operation.Journal) (Inventory, error) {
	if home == "" {
		return Inventory{}, fmt.Errorf("home directory is required")
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return Inventory{}, err
	}
	var absProject string
	if project != "" {
		absProject, err = filepath.Abs(project)
		if err != nil {
			return Inventory{}, err
		}
	}
	repo, err := skillrepo.Open(repository)
	if err != nil {
		return Inventory{}, err
	}
	locations := sourceLocations(absHome, absProject)
	result := Inventory{Locations: locations, Sources: []Source{}, Diagnostics: []Diagnostic{}}
	for _, location := range locations {
		boundary := absHome
		if location.Scope == ScopeProject {
			boundary = absProject
		}
		if err := validateBelowBoundary(boundary, location.Path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: err.Error()})
			continue
		}
		info, err := os.Lstat(location.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: fmt.Sprintf("inspect source directory: %v", err)})
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: "source location must be a real directory; symbolic links are not followed"})
			continue
		}
		overlaps, err := skillrepo.PathsOverlap(repo.Root(), location.Path)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: fmt.Sprintf("validate repository exclusion: %v", err)})
			continue
		}
		if overlaps {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: "configured repository is excluded from Skill sources"})
			continue
		}
		found, diagnostics, err := discoverLocation(location, boundary, globalJournal, projectJournal, repo)
		if err != nil {
			return Inventory{}, err
		}
		result.Sources = append(result.Sources, found...)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}
	sort.Slice(result.Sources, func(i, j int) bool {
		left, right := result.Sources[i], result.Sources[j]
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		if left.Location != right.Location {
			return left.Location < right.Location
		}
		if left.Identifier != right.Identifier {
			return left.Identifier < right.Identifier
		}
		return left.Path < right.Path
	})
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].Scope != result.Diagnostics[j].Scope {
			return result.Diagnostics[i].Scope < result.Diagnostics[j].Scope
		}
		if result.Diagnostics[i].Path != result.Diagnostics[j].Path {
			return result.Diagnostics[i].Path < result.Diagnostics[j].Path
		}
		return result.Diagnostics[i].Message < result.Diagnostics[j].Message
	})
	return result, nil
}

func sourceLocations(home, project string) []SourceLocation {
	locations := make([]SourceLocation, 0, 10)
	for _, target := range adapter.Supported() {
		agent, ok := adapter.ForAgent(target.Target())
		if !ok {
			continue
		}
		label := map[adapter.Target]string{adapter.ClaudeCode: "Claude Code", adapter.Codex: "Codex", adapter.Pi: "Pi"}[target.Target()]
		locations = append(locations, SourceLocation{Scope: ScopeGlobal, Label: label, Path: filepath.Dir(agent.GlobalSkillPath(home, "placeholder"))})
		if project != "" {
			locations = append(locations, SourceLocation{Scope: ScopeProject, Label: label, Path: filepath.Dir(agent.ProjectSkillPath(project, "placeholder"))})
		}
	}
	locations = append(locations,
		SourceLocation{Scope: ScopeGlobal, Label: ".agent", Path: filepath.Join(home, ".agent", "skills")},
		SourceLocation{Scope: ScopeGlobal, Label: ".agents", Path: filepath.Join(home, ".agents", "skills")},
	)
	if project != "" {
		locations = append(locations,
			SourceLocation{Scope: ScopeProject, Label: ".agent", Path: filepath.Join(project, ".agent", "skills")},
			SourceLocation{Scope: ScopeProject, Label: ".agents", Path: filepath.Join(project, ".agents", "skills")},
		)
	}
	return locations
}

func discoverLocation(location SourceLocation, boundary string, globalJournal, projectJournal *operation.Journal, repo *skillrepo.Repository) ([]Source, []Diagnostic, error) {
	children, err := os.ReadDir(location.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("read supported Skill source %s: %w", location.Path, err)
	}
	parsed, catalogDiagnostics, err := catalog.Discover(location.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("parse supported Skill source %s: %w", location.Path, err)
	}
	byID := make(map[string]catalog.Skill, len(parsed))
	for _, skill := range parsed {
		byID[skill.Identifier] = skill
	}
	diagnostics := make([]Diagnostic, 0, len(catalogDiagnostics))
	for _, item := range catalogDiagnostics {
		diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: item.Path, Message: item.Message})
	}
	sources := make([]Source, 0, len(children))
	for _, child := range children {
		path := filepath.Join(location.Path, child.Name())
		info, err := os.Lstat(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: fmt.Sprintf("inspect Skill entry: %v", err)})
			continue
		}
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		candidate := Source{ID: sourceID(location, child.Name(), path), Identifier: child.Name(), Scope: location.Scope, Location: location.Label, Path: path}
		if info.Mode()&os.ModeSymlink != 0 {
			if source, err := os.Readlink(path); err == nil {
				if !filepath.IsAbs(source) {
					source = filepath.Join(filepath.Dir(path), source)
				}
				if owned, _ := repo.OwnsPath(child.Name(), source); owned {
					candidate.AlreadyManaged = true
					candidate.ManagedMaterial = "symlink"
					candidate.Eligible = true
					sources = append(sources, candidate)
					continue
				}
			}
			candidate.Problem = "symbolic-link Skills are not followed"
			sources = append(sources, candidate)
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
			continue
		}
		if err := validateBelowBoundary(boundary, path); err != nil {
			candidate.Problem = err.Error()
			sources = append(sources, candidate)
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
			continue
		}
		if journal := journalFor(location.Scope, globalJournal, projectJournal); journal != nil {
			if materialization, managed, err := journal.RecordedMaterialization(path); err != nil {
				candidate.Problem = fmt.Sprintf("validate managed Skill journal: %v", err)
			} else if managed && materialization.Method == "copy" && materialization.Source != "" {
				if owned, ownErr := repo.OwnsPath(child.Name(), materialization.Source); ownErr == nil && owned {
					candidate.AlreadyManaged = true
					candidate.ManagedMaterial = "copy"
					candidate.Eligible = true
					sources = append(sources, candidate)
					continue
				}
			}
		}
		checksum, err := skillrepo.ChecksumTree(path)
		if err != nil {
			candidate.Problem = fmt.Sprintf("unsafe Skill tree: %v", err)
		} else if _, ok := byID[child.Name()]; !ok {
			candidate.Problem = "directory does not contain a valid SKILL.md"
		} else {
			candidate.Checksum = checksum
			candidate.Eligible = true
		}
		if !candidate.Eligible {
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
		}
		sources = append(sources, candidate)
	}
	return sources, diagnostics, nil
}

func journalFor(scope Scope, global, project *operation.Journal) *operation.Journal {
	if scope == ScopeGlobal {
		return global
	}
	return project
}

func sourceID(location SourceLocation, identifier, path string) string {
	return string(location.Scope) + ":" + location.Label + ":" + identifier + ":" + filepath.Clean(path)
}

func validateBelowBoundary(boundary, path string) error {
	absoluteBoundary, err := filepath.Abs(boundary)
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteBoundary, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: supported path escapes its registered root", ErrUnsafeSource)
	}
	boundaryInfo, err := os.Lstat(absoluteBoundary)
	if err != nil {
		return err
	}
	if !boundaryInfo.IsDir() || boundaryInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: source boundary is not a real directory", ErrUnsafeSource)
	}
	current := absoluteBoundary
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return err
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic-link ancestor %s is not followed", ErrUnsafeSource, current)
		}
		if current != absolutePath && !info.IsDir() {
			return fmt.Errorf("%w: source ancestor %s is not a directory", ErrUnsafeSource, current)
		}
	}
	return nil
}
