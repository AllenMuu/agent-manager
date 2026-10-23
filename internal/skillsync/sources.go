// Package skillsync discovers local Skills and coordinates guarded imports.
package skillsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

var ErrUnsafeSource = errors.New("unsafe Skill source")

const (
	maxSourceEntriesPerScope = 4096
	maxSourceLinkHops        = 16
	maxSourceSkillFiles      = 2048
	maxSourceSkillBytes      = 64 << 20
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// Source describes one Skill candidate and the supported location that owns it.
type Source struct {
	ID                 string         `json:"id"`
	Identifier         string         `json:"identifier"`
	ResolvedIdentifier string         `json:"resolvedIdentifier,omitempty"`
	Scope              Scope          `json:"scope"`
	Location           string         `json:"location"`
	Target             adapter.Target `json:"target,omitempty"`
	Path               string         `json:"path"`
	EntryType          string         `json:"entryType,omitempty"`
	RealPath           string         `json:"realPath,omitempty"`
	OriginalLink       string         `json:"originalLink,omitempty"`
	LinkChain          []LinkHop      `json:"linkChain,omitempty"`
	MigrationEligible  bool           `json:"migrationEligible"`
	IdentityConflict   bool           `json:"identityConflict,omitempty"`
	Checksum           string         `json:"checksum,omitempty"`
	ModifiedAt         *time.Time     `json:"modifiedAt,omitempty"`
	Origin             string         `json:"origin,omitempty"`
	OriginType         string         `json:"originType,omitempty"`
	OriginRef          string         `json:"originRef,omitempty"`
	OriginSkillPath    string         `json:"originSkillPath,omitempty"`
	InstalledAt        *time.Time     `json:"installedAt,omitempty"`
	UpdatedAt          *time.Time     `json:"updatedAt,omitempty"`
	StoredHash         string         `json:"-"`
	HashKind           string         `json:"-"`
	LockPath           string         `json:"-"`
	LockKey            string         `json:"-"`
	Eligible           bool           `json:"eligible"`
	Problem            string         `json:"problem,omitempty"`
	AlreadyManaged     bool           `json:"alreadyManaged,omitempty"`
	ManagedMaterial    string         `json:"managedMaterial,omitempty"`
}

type SourceLocation struct {
	Scope Scope          `json:"scope"`
	Label string         `json:"label"`
	Path  string         `json:"path"`
	Agent adapter.Target `json:"agent,omitempty"`
}

// LinkHop records the exact path and unmodified target text for one link.
type LinkHop struct {
	Path   string `json:"path"`
	Target string `json:"target"`
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
	Complete    bool             `json:"complete"`
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
	result := Inventory{Locations: locations, Sources: []Source{}, Diagnostics: []Diagnostic{}, Complete: true}
	entriesByScope := map[Scope]int{}
	limitReached := map[Scope]bool{}
	trees := map[string]sourceTreeResult{}
	for _, location := range locations {
		if limitReached[location.Scope] {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: fmt.Sprintf("source scan stopped because the %d-entry scope limit was reached", maxSourceEntriesPerScope)})
			continue
		}
		boundary := absHome
		if location.Scope == ScopeProject {
			boundary = absProject
		}
		if err := validateBelowBoundary(boundary, location.Path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: err.Error()})
			result.Complete = false
			continue
		}
		info, err := os.Lstat(location.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: fmt.Sprintf("inspect source directory: %v", err)})
			result.Complete = false
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: "source location must be a real directory; symbolic links are not followed"})
			result.Complete = false
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
		remaining := maxSourceEntriesPerScope - entriesByScope[location.Scope]
		found, diagnostics, observed, overflow, err := discoverLocation(location, boundary, remaining, globalJournal, projectJournal, repo, trees)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Scope: location.Scope, Path: location.Path, Message: err.Error()})
			result.Complete = false
			continue
		}
		result.Sources = append(result.Sources, found...)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		entriesByScope[location.Scope] += observed
		if overflow {
			limitReached[location.Scope] = true
			result.Complete = false
		}
	}
	attachSourceProvenance(result.Sources, absHome, absProject)
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
		locations = append(locations, SourceLocation{Scope: ScopeGlobal, Label: label, Path: filepath.Dir(agent.GlobalSkillPath(home, "placeholder")), Agent: target.Target()})
		if project != "" {
			locations = append(locations, SourceLocation{Scope: ScopeProject, Label: label, Path: filepath.Dir(agent.ProjectSkillPath(project, "placeholder")), Agent: target.Target()})
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

func discoverLocation(location SourceLocation, boundary string, limit int, globalJournal, projectJournal *operation.Journal, repo *skillrepo.Repository, trees map[string]sourceTreeResult) ([]Source, []Diagnostic, int, bool, error) {
	children, overflow, err := readDirectoryEntries(location.Path, limit)
	if err != nil {
		return nil, nil, 0, false, fmt.Errorf("read supported Skill source %s: %w", location.Path, err)
	}
	diagnostics := make([]Diagnostic, 0)
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
		candidate := Source{ID: sourceID(location, child.Name(), path), Identifier: child.Name(), Scope: location.Scope, Location: location.Label, Target: location.Agent, Path: path}
		if info.Mode()&os.ModeSymlink != 0 {
			candidate.EntryType = "external-symlink"
			realPath, chain, original, resolveErr := resolveEntryLink(path)
			candidate.LinkChain = chain
			candidate.OriginalLink = original
			if resolveErr != nil {
				candidate.Problem = resolveErr.Error()
				sources = append(sources, candidate)
				diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
				continue
			}
			candidate.RealPath = realPath
			candidate.ResolvedIdentifier = filepath.Base(realPath)
			candidate.MigrationEligible = location.Agent != ""
		} else {
			candidate.EntryType = "directory"
			candidate.RealPath = path
			candidate.ResolvedIdentifier = child.Name()
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if err := validateBelowBoundary(boundary, path); err != nil {
				candidate.Problem = err.Error()
				sources = append(sources, candidate)
				diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
				continue
			}
		}
		if journal := journalFor(location.Scope, globalJournal, projectJournal); journal != nil {
			materialization, managed, journalErr := journal.RecordedMaterialization(path)
			if journalErr != nil {
				candidate.Problem = fmt.Sprintf("validate managed Skill journal: %v", journalErr)
				sources = append(sources, candidate)
				diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
				continue
			}
			if managed && materialization.Source != "" {
				if owned, ownErr := repo.OwnsPath(child.Name(), materialization.Source); ownErr == nil && owned {
					candidate.AlreadyManaged = true
					candidate.ManagedMaterial = materialization.Method
					candidate.EntryType = "managed-copy"
					if materialization.Method == "symlink" {
						candidate.EntryType = "managed-link"
					}
					candidate.Eligible = true
					candidate.MigrationEligible = false
					sources = append(sources, candidate)
					continue
				}
			}
		}
		overlaps, err := skillrepo.PathsOverlap(repo.Root(), candidate.RealPath)
		if err != nil {
			candidate.Problem = fmt.Sprintf("validate repository exclusion: %v", err)
			sources = append(sources, candidate)
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
			continue
		}
		if overlaps {
			if owned, ownErr := repo.OwnsPath(child.Name(), candidate.RealPath); ownErr == nil && owned {
				candidate.ResolvedIdentifier = child.Name()
				candidate.AlreadyManaged = true
				candidate.ManagedMaterial = "symlink"
				if len(candidate.LinkChain) == 1 {
					candidate.MigrationEligible = false
				}
			} else {
				candidate.Problem = "resolved Skill source overlaps the configured repository"
				sources = append(sources, candidate)
				diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
				continue
			}
		}
		tree, inspected := trees[candidate.RealPath]
		if !inspected {
			tree.checksum, tree.err = skillrepo.ChecksumTreeWithLimits(candidate.RealPath, maxSourceSkillFiles, maxSourceSkillBytes)
			if tree.err == nil {
				tree.skill, tree.err = catalog.ReadDirectory(candidate.RealPath)
			}
			if tree.err == nil {
				modifiedAt, modifiedErr := newestRegularFileMTime(candidate.RealPath)
				tree.err = modifiedErr
				if modifiedErr == nil {
					tree.modifiedAt = &modifiedAt
				}
			}
			trees[candidate.RealPath] = tree
		}
		if tree.err != nil {
			candidate.Problem = fmt.Sprintf("unsafe Skill tree: %v", tree.err)
		} else if candidate.ResolvedIdentifier != child.Name() {
			candidate.IdentityConflict = true
			candidate.MigrationEligible = false
			candidate.Problem = fmt.Sprintf("source entry %q does not match resolved Skill identity %q; choose which identity to use before importing", child.Name(), candidate.ResolvedIdentifier)
			candidate.Checksum = tree.checksum
			candidate.Eligible = tree.checksum != ""
			if tree.modifiedAt != nil && !tree.modifiedAt.IsZero() {
				modifiedAt := *tree.modifiedAt
				candidate.ModifiedAt = &modifiedAt
			}
		} else if tree.checksum == "" {
			candidate.Problem = "directory does not contain a valid SKILL.md"
		} else {
			candidate.Checksum = tree.checksum
			candidate.Eligible = true
			if tree.modifiedAt != nil && !tree.modifiedAt.IsZero() {
				modifiedAt := *tree.modifiedAt
				candidate.ModifiedAt = &modifiedAt
			}
		}
		if !candidate.Eligible {
			diagnostics = append(diagnostics, Diagnostic{Scope: location.Scope, Path: path, Message: candidate.Problem})
		}
		sources = append(sources, candidate)
	}
	return sources, diagnostics, len(children), overflow, nil
}

func readDirectoryEntries(path string, limit int) ([]os.DirEntry, bool, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer directory.Close()
	readLimit := limit + 1
	if readLimit < 1 {
		readLimit = 1
	}
	entries, err := directory.ReadDir(readLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	overflow := len(entries) > limit
	if overflow {
		entries = entries[:limit]
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, overflow, nil
}

type sourceTreeResult struct {
	skill      catalog.Skill
	checksum   string
	modifiedAt *time.Time
	err        error
}

func resolveEntryLink(entry string) (string, []LinkHop, string, error) {
	current, err := filepath.Abs(entry)
	if err != nil {
		return "", nil, "", err
	}
	seen := map[string]bool{}
	chain := make([]LinkHop, 0, 2)
	original := ""
	for hop := 0; hop <= maxSourceLinkHops; hop++ {
		current = filepath.Clean(current)
		if seen[current] {
			return "", chain, original, fmt.Errorf("symbolic-link cycle detected")
		}
		seen[current] = true
		if err := validateRealPathParents(current); err != nil {
			return "", chain, original, err
		}
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return "", chain, original, fmt.Errorf("symbolic-link target is missing")
			}
			return "", chain, original, fmt.Errorf("inspect symbolic-link target: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.IsDir() {
				return "", chain, original, fmt.Errorf("symbolic-link target is not a Skill directory")
			}
			return current, chain, original, nil
		}
		if hop == maxSourceLinkHops {
			return "", chain, original, fmt.Errorf("symbolic-link chain exceeds %d hops", maxSourceLinkHops)
		}
		target, err := os.Readlink(current)
		if err != nil {
			return "", chain, original, fmt.Errorf("read symbolic link: %w", err)
		}
		if hop == 0 {
			original = target
		}
		chain = append(chain, LinkHop{Path: current, Target: target})
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		current, err = filepath.Abs(target)
		if err != nil {
			return "", chain, original, err
		}
	}
	return "", chain, original, fmt.Errorf("symbolic-link chain exceeds %d hops", maxSourceLinkHops)
}

func validateRealPathParents(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absPath)
	root := volume + string(filepath.Separator)
	relative, err := filepath.Rel(root, absPath)
	if err != nil {
		return err
	}
	components := strings.Split(relative, string(filepath.Separator))
	current := root
	for _, component := range components[:max(0, len(components)-1)] {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect link-path ancestor %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !trustedSystemPathSymlink(current) {
				return fmt.Errorf("%w: link-path ancestor %s is not a real directory", ErrUnsafeSource, current)
			}
			resolved, err := os.Stat(current)
			if err != nil || !resolved.IsDir() {
				return fmt.Errorf("%w: link-path ancestor %s is not a real directory", ErrUnsafeSource, current)
			}
		} else if !info.IsDir() {
			return fmt.Errorf("%w: link-path ancestor %s is not a real directory", ErrUnsafeSource, current)
		}
	}
	return nil
}

func trustedSystemPathSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return (path == "/var" && resolved == "/private/var") || (path == "/tmp" && resolved == "/private/tmp")
}

func journalFor(scope Scope, global, project *operation.Journal) *operation.Journal {
	if scope == ScopeGlobal {
		return global
	}
	return project
}

func sourceID(location SourceLocation, identifier, path string) string {
	identity := string(location.Scope) + "\x00" + location.Label + "\x00" + identifier + "\x00" + filepath.Clean(path)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
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
