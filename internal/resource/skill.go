package resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillCatalogEntry is an eligible directory Skill discovered in a local catalog.
type SkillCatalogEntry struct {
	Identifier    string
	SourcePath    string
	Name          string
	Description   string
	Body          string
	Tags          []string
	Compatibility []string
	Provenance    string
}

// Diagnostic explains why a catalog entry is not eligible for management.
type Diagnostic struct {
	Path    string
	Message string
}

// Discover reads immediate child directories of root that contain valid
// SKILL.md files. It only reads metadata files; it never executes Skill content.
func (h SkillHandler) Discover(root string) ([]SkillCatalogEntry, []Diagnostic, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve skill library path: %w", err)
	}
	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("read skill library: %w", err)
	}

	var skills []SkillCatalogEntry
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(absRoot, entry.Name())
		skill, err := readSkill(path, entry.Name())
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		skills = append(skills, skill)
	}
	return skills, diagnostics, nil
}

// Catalog returns the agent-neutral resource envelopes for eligible Skills.
func (h SkillHandler) Catalog(root string) ([]ManagedResource, []Diagnostic, error) {
	skills, diagnostics, err := h.Discover(root)
	if err != nil {
		return nil, nil, err
	}
	resources := make([]ManagedResource, 0, len(skills))
	for _, skill := range skills {
		managed, err := h.ManagedResource(skill)
		if err != nil {
			return nil, nil, err
		}
		resources = append(resources, managed)
	}
	return resources, diagnostics, nil
}

// Inspect reads and validates one directory Skill without executing its content.
func (h SkillHandler) Inspect(path string) (ManagedResource, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ManagedResource{}, fmt.Errorf("resolve skill path: %w", err)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return ManagedResource{}, fmt.Errorf("inspect skill path: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ManagedResource{}, fmt.Errorf("skill path is not an eligible directory Skill")
	}
	skill, err := readSkill(absPath, filepath.Base(absPath))
	if err != nil {
		return ManagedResource{}, err
	}
	return h.ManagedResource(skill)
}

// Eligible reports whether source is the named directory Skill in its catalog.
// Malformed entries are ineligible rather than fatal, matching catalog discovery.
func (h SkillHandler) Eligible(identifier, source string) (bool, error) {
	info, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return false, err
		}
		return false, fmt.Errorf("inspect skill source %s: %w", source, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	skills, _, err := h.Discover(filepath.Dir(source))
	if err != nil {
		return false, fmt.Errorf("discover skill source %s: %w", source, err)
	}
	for _, skill := range skills {
		if skill.Identifier == identifier && skill.SourcePath == source {
			return true, nil
		}
	}
	return false, nil
}

// ManagedResource converts Skill catalog metadata into its agent-neutral
// resource envelope. Runtime placement remains the target adapter's concern.
func (h SkillHandler) ManagedResource(skill SkillCatalogEntry) (ManagedResource, error) {
	managed := ManagedResource{
		Version:       "v1",
		ID:            skill.Identifier,
		Kind:          Skill,
		Provenance:    Provenance{Source: skill.SourcePath},
		Compatibility: Compatibility{Agents: skill.Compatibility},
	}
	if err := h.Validate(managed); err != nil {
		return ManagedResource{}, err
	}
	return managed, nil
}

// LibraryResource returns the managed identity and canonical source expected
// for one Skill in the configured library.
func (h SkillHandler) LibraryResource(libraryPath, identifier string) (ManagedResource, error) {
	absLibrary, err := filepath.Abs(libraryPath)
	if err != nil {
		return ManagedResource{}, fmt.Errorf("resolve skill library path: %w", err)
	}
	return h.ManagedResource(SkillCatalogEntry{
		Identifier: identifier,
		SourcePath: filepath.Join(absLibrary, identifier),
	})
}

// ManagesSource reports whether source is the canonical configured-library
// source for identifier.
func (h SkillHandler) ManagesSource(libraryPath, identifier, source string) (bool, error) {
	managed, err := h.LibraryResource(libraryPath, identifier)
	if err != nil {
		return false, err
	}
	return source == managed.Provenance.Source, nil
}

// PlanLifecycle validates Skill identity and filesystem semantics and returns
// a mutation-free plan. Runtime placement is supplied by the selected adapter.
func (h SkillHandler) PlanLifecycle(request LifecycleRequest) (LifecyclePlan, error) {
	skillRequest, ok := request.(SkillLifecycleRequest)
	if !ok {
		return nil, fmt.Errorf("Skill handler does not support lifecycle request %T", request)
	}
	switch skillRequest.Action {
	case LifecycleActivate:
		return h.planActivation(skillRequest)
	case LifecycleRemove:
		return h.planManagedLink(skillRequest, "remove managed link")
	case LifecycleAdopt:
		return h.planAdoption(skillRequest)
	case LifecycleFork:
		return h.planManagedLink(skillRequest, "replace managed link with independent copy")
	case LifecycleReconcile:
		return h.planReconciliation(skillRequest)
	default:
		return nil, fmt.Errorf("Skill handler does not support lifecycle action %q", skillRequest.Action)
	}
}

func (h SkillHandler) planActivation(request SkillLifecycleRequest) (SkillLifecyclePlan, error) {
	plan := SkillLifecyclePlan{Action: request.Action}
	managed := request.Resource
	if request.CatalogEntry != nil {
		var err error
		managed, err = h.ManagedResource(*request.CatalogEntry)
		if err != nil {
			return plan, errors.Join(ErrUnsafeLifecycle, err)
		}
	}
	if err := h.Validate(managed); err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	canonical, err := h.LibraryResource(request.LibraryPath, managed.ID)
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	source, err := filepath.Abs(managed.Provenance.Source)
	if err != nil {
		return plan, err
	}
	if source != canonical.Provenance.Source {
		return plan, fmt.Errorf("%w: skill source is not configured library entry", ErrUnsafeLifecycle)
	}
	eligible, err := h.Eligible(managed.ID, source)
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	if !eligible {
		return plan, fmt.Errorf("%w: library source is missing or not an eligible directory skill", ErrUnsafeLifecycle)
	}
	managed.Provenance.Source = source
	plan.Resource = managed
	plan.SourcePath = source
	plan.Applicable = true
	if request.PlacementPath == "" {
		return plan, nil
	}
	conflict, err := skillPlacementConflicts(request.PlacementPath, source)
	if err != nil {
		return plan, err
	}
	action := "create absolute link"
	if conflict {
		if request.Conflict != SkillConflictReplace {
			return plan, ErrUnsafeLifecycle
		}
		if !request.Force {
			return plan, ErrForceRequired
		}
		action = "replace conflicting path with absolute link"
	}
	plan.ReplaceExisting = conflict
	plan.Changes = []LifecycleChange{{Path: request.PlacementPath, Action: action, Detail: source}}
	if !containsString(managed.Compatibility.Agents, request.Target) {
		plan.Warnings = []string{fmt.Sprintf("%s is not declared compatible with %s", managed.ID, request.Target)}
	}
	return plan, nil
}

func (h SkillHandler) planManagedLink(request SkillLifecycleRequest, action string) (SkillLifecyclePlan, error) {
	plan := SkillLifecyclePlan{Action: request.Action}
	managed, err := h.LibraryResource(request.LibraryPath, request.Identifier)
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	plan.Resource = managed
	if request.PlacementPath == "" {
		return plan, nil
	}
	source, owned, err := h.managedLink(request.PlacementPath, managed)
	if err != nil {
		return plan, err
	}
	if !owned {
		return plan, ErrUnsafeLifecycle
	}
	plan.SourcePath = source
	plan.CurrentSource = source
	plan.Applicable = true
	plan.Changes = []LifecycleChange{{Path: request.PlacementPath, Action: action, Detail: source}}
	if request.Action == LifecycleRemove {
		plan.Changes[0].Detail = ""
	}
	return plan, nil
}

func (h SkillHandler) planAdoption(request SkillLifecycleRequest) (SkillLifecyclePlan, error) {
	plan := SkillLifecyclePlan{Action: request.Action}
	managed, err := h.LibraryResource(request.LibraryPath, request.Identifier)
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	plan.Resource = managed
	if request.PlacementPath == "" {
		return plan, nil
	}
	eligible, err := h.Eligible(request.Identifier, request.PlacementPath)
	if err != nil {
		return plan, err
	}
	if !eligible {
		return plan, fmt.Errorf("%w: project directory is not an eligible skill", ErrUnsafeLifecycle)
	}
	libraryPath := managed.Provenance.Source
	if _, err := os.Lstat(libraryPath); err == nil {
		return plan, ErrLifecycleConflict
	} else if !os.IsNotExist(err) {
		return plan, err
	}
	plan.SourcePath = request.PlacementPath
	plan.Applicable = true
	plan.Changes = []LifecycleChange{
		{Path: libraryPath, Action: "copy project skill into library", Detail: request.PlacementPath},
		{Path: request.PlacementPath, Action: "replace directory with managed link", Detail: libraryPath},
	}
	return plan, nil
}

func (h SkillHandler) planReconciliation(request SkillLifecycleRequest) (SkillLifecyclePlan, error) {
	plan := SkillLifecyclePlan{Action: request.Action}
	managed, err := h.LibraryResource(request.LibraryPath, request.Identifier)
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	plan.Resource = managed
	if request.PlacementPath == "" {
		return plan, nil
	}
	info, err := os.Lstat(request.PlacementPath)
	if err != nil {
		return plan, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return plan, nil
	}
	old, err := os.Readlink(request.PlacementPath)
	if err != nil {
		return plan, err
	}
	if _, err := os.Stat(old); err == nil {
		return plan, nil
	} else if !os.IsNotExist(err) {
		return plan, fmt.Errorf("inspect link target %s: %w", old, err)
	}
	managedSource, err := h.ManagesSource(request.LibraryPath, request.Identifier, old)
	if err != nil {
		return plan, err
	}
	if !managedSource && (!request.JournalOwned || request.JournalSource != old) {
		return plan, nil
	}
	eligible, err := h.Eligible(request.Identifier, managed.Provenance.Source)
	if err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return plan, err
	}
	if !eligible {
		return plan, nil
	}
	plan.SourcePath = managed.Provenance.Source
	plan.CurrentSource = old
	plan.Applicable = true
	plan.Changes = []LifecycleChange{{Path: request.PlacementPath, Action: "repoint orphaned managed link", Detail: managed.Provenance.Source}}
	return plan, nil
}

func (h SkillHandler) managedLink(path string, managed ManagedResource) (string, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect managed link %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", false, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", false, fmt.Errorf("read managed link %s: %w", path, err)
	}
	return target, target == managed.Provenance.Source, nil
}

func skillPlacementConflicts(path, source string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil && target == source {
			return false, nil
		}
	}
	return true, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readSkill(path, identifier string) (SkillCatalogEntry, error) {
	contents, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return SkillCatalogEntry{}, fmt.Errorf("missing SKILL.md")
		}
		return SkillCatalogEntry{}, fmt.Errorf("read SKILL.md: %w", err)
	}

	name, description, body, err := parseSkillMarkdown(string(contents))
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	skill := SkillCatalogEntry{
		Identifier:  identifier,
		SourcePath:  path,
		Name:        name,
		Description: description,
		Body:        body,
	}
	metadata, err := os.ReadFile(filepath.Join(path, ".skill-manager.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return skill, nil
		}
		return SkillCatalogEntry{}, fmt.Errorf("read companion metadata: %w", err)
	}
	tags, compatibility, provenance, err := parseCompanionMetadata(string(metadata))
	if err != nil {
		return SkillCatalogEntry{}, fmt.Errorf("parse companion metadata: %w", err)
	}
	skill.Tags = tags
	skill.Compatibility = compatibility
	skill.Provenance = provenance
	return skill, nil
}

func parseSkillMarkdown(contents string) (name, description, body string, err error) {
	lines := strings.SplitAfter(contents, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", "", fmt.Errorf("SKILL.md must begin with frontmatter")
	}
	frontmatterEnd := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			frontmatterEnd = i
			break
		}
	}
	if frontmatterEnd < 0 {
		return "", "", "", fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	var frontmatter struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:frontmatterEnd], "")), &frontmatter); err != nil {
		return "", "", "", fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	if frontmatter.Name == "" || frontmatter.Description == "" {
		return "", "", "", fmt.Errorf("SKILL.md frontmatter requires name and description")
	}
	return frontmatter.Name, frontmatter.Description, strings.Join(lines[frontmatterEnd+1:], ""), nil
}

func parseCompanionMetadata(contents string) (tags, compatibility []string, provenance string, err error) {
	var metadata struct {
		Tags          []string `yaml:"tags"`
		Compatibility []string `yaml:"compatibility"`
		Provenance    string   `yaml:"provenance"`
	}
	if err := yaml.Unmarshal([]byte(contents), &metadata); err != nil {
		return nil, nil, "", err
	}
	return metadata.Tags, metadata.Compatibility, metadata.Provenance, nil
}
