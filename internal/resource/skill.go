package resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"gopkg.in/yaml.v3"
)

// SkillCatalogEntry is an eligible directory Skill discovered in a local catalog.
type SkillCatalogEntry struct {
	Identifier          string             `json:"identifier"`
	SourcePath          string             `json:"sourcePath"`
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	Body                string             `json:"body,omitempty"`
	Tags                []string           `json:"tags,omitempty"`
	Compatibility       []string           `json:"compatibility,omitempty"`
	SupportedLanguages  []string           `json:"supportedLanguages,omitempty"`
	SupportedFrameworks []string           `json:"supportedFrameworks,omitempty"`
	SupportedAgents     []string           `json:"supportedAgents,omitempty"`
	Provenance          string             `json:"provenance,omitempty"`
	Version             string             `json:"version,omitempty"`
	VersionKey          string             `json:"versionKey,omitempty"`
	Versions            []SkillVersionInfo `json:"versions,omitempty"`
	Source              string             `json:"source,omitempty"`
	SourceType          string             `json:"sourceType,omitempty"`
	Publisher           string             `json:"publisher,omitempty"`
	ImmutableRef        string             `json:"immutableRef,omitempty"`
	Checksum            string             `json:"checksum,omitempty"`
	TrustLevel          string             `json:"trustLevel,omitempty"`
	VerificationStatus  string             `json:"verificationStatus,omitempty"`
}

// SkillVersionInfo is an addressable version in the configured repository.
type SkillVersionInfo struct {
	Version             string   `json:"version,omitempty"`
	Key                 string   `json:"key"`
	Source              string   `json:"source,omitempty"`
	SourceType          string   `json:"sourceType,omitempty"`
	Publisher           string   `json:"publisher,omitempty"`
	ImmutableRef        string   `json:"immutableRef,omitempty"`
	Checksum            string   `json:"checksum,omitempty"`
	TrustLevel          string   `json:"trustLevel,omitempty"`
	VerificationStatus  string   `json:"verificationStatus,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	Compatibility       []string `json:"compatibility,omitempty"`
	SupportedLanguages  []string `json:"supportedLanguages,omitempty"`
	SupportedFrameworks []string `json:"supportedFrameworks,omitempty"`
	SupportedAgents     []string `json:"supportedAgents,omitempty"`
	LegacyProvenance    string   `json:"legacyProvenance,omitempty"`
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
	repository, err := skillrepo.Open(absRoot)
	if err != nil {
		return nil, nil, err
	}
	entries, repositoryDiagnostics, err := repository.Discover()
	if err != nil {
		return nil, nil, fmt.Errorf("read skill library: %w", err)
	}
	skills := make([]SkillCatalogEntry, 0, len(entries))
	diagnostics := make([]Diagnostic, 0, len(repositoryDiagnostics))
	for _, diagnostic := range repositoryDiagnostics {
		diagnostics = append(diagnostics, Diagnostic{Path: diagnostic.Path, Message: diagnostic.Message})
	}
	for _, entry := range entries {
		skill, err := readSkill(entry.Path, entry.Identifier)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: entry.Path, Message: err.Error()})
			continue
		}
		skill.Version = entry.Version
		skill.VersionKey = entry.Key
		skill.Source = entry.Metadata.Source
		skill.SourceType = entry.Metadata.SourceType
		skill.Publisher = entry.Metadata.Publisher
		skill.ImmutableRef = entry.Metadata.ImmutableRef
		skill.Checksum = entry.Metadata.Checksum
		skill.TrustLevel = entry.Metadata.TrustLevel
		skill.VerificationStatus = entry.Metadata.VerificationStatus
		if entry.Metadata.LegacyProvenance != "" {
			skill.Provenance = entry.Metadata.LegacyProvenance
		}
		applyVersionMetadata(&skill, entry.Metadata)
		versions, err := repository.VersionRecords(entry.Identifier)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: entry.Path, Message: fmt.Sprintf("read Skill versions: %v", err)})
			continue
		}
		skill.Versions = versionInfos(versions)
		skills = append(skills, skill)
	}
	return skills, diagnostics, nil
}

// ReadDirectory reads the Skill metadata from one exact real directory without
// enumerating its parent or any sibling entries. Callers that discover links
// must validate the link chain and complete tree before using this method.
func (h SkillHandler) ReadDirectory(path string) (SkillCatalogEntry, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return SkillCatalogEntry{}, fmt.Errorf("%s is not a real Skill directory", absPath)
	}
	return readSkill(absPath, filepath.Base(absPath))
}

// Resolve returns one addressable repository version without exposing other
// version directories as independent catalog identities.
func (h SkillHandler) Resolve(root, identifier, version string) (SkillCatalogEntry, error) {
	repository, err := skillrepo.Open(root)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	entry, err := repository.Resolve(identifier, version)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	skill, err := readSkill(entry.Path, entry.Identifier)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	applyVersion(&skill, entry)
	versions, err := repository.VersionRecords(identifier)
	if err != nil {
		return SkillCatalogEntry{}, err
	}
	skill.Versions = versionInfos(versions)
	return skill, nil
}

func applyVersion(skill *SkillCatalogEntry, entry skillrepo.Entry) {
	skill.Version = entry.Version
	skill.VersionKey = entry.Key
	skill.Source = entry.Metadata.Source
	skill.SourceType = entry.Metadata.SourceType
	skill.Publisher = entry.Metadata.Publisher
	skill.ImmutableRef = entry.Metadata.ImmutableRef
	skill.Checksum = entry.Metadata.Checksum
	skill.TrustLevel = entry.Metadata.TrustLevel
	skill.VerificationStatus = entry.Metadata.VerificationStatus
	if entry.Metadata.LegacyProvenance != "" {
		skill.Provenance = entry.Metadata.LegacyProvenance
	}
	applyVersionMetadata(skill, entry.Metadata)
}

func applyVersionMetadata(skill *SkillCatalogEntry, metadata skillrepo.VersionMetadata) {
	if len(metadata.Tags) > 0 {
		skill.Tags = append([]string(nil), metadata.Tags...)
	}
	if len(metadata.Compatibility) > 0 {
		skill.Compatibility = append([]string(nil), metadata.Compatibility...)
	}
	skill.SupportedLanguages = append([]string(nil), metadata.SupportedLanguages...)
	skill.SupportedFrameworks = append([]string(nil), metadata.SupportedFrameworks...)
	skill.SupportedAgents = append([]string(nil), metadata.SupportedAgents...)
}

func versionInfos(versions []skillrepo.Entry) []SkillVersionInfo {
	infos := make([]SkillVersionInfo, 0, len(versions))
	for _, version := range versions {
		infos = append(infos, SkillVersionInfo{
			Version: version.Version, Key: version.Key, Source: version.Metadata.Source,
			SourceType: version.Metadata.SourceType, Publisher: version.Metadata.Publisher,
			ImmutableRef: version.Metadata.ImmutableRef, Checksum: version.Metadata.Checksum,
			TrustLevel: version.Metadata.TrustLevel, VerificationStatus: version.Metadata.VerificationStatus,
			Tags: append([]string(nil), version.Metadata.Tags...), Compatibility: append([]string(nil), version.Metadata.Compatibility...),
			SupportedLanguages:  append([]string(nil), version.Metadata.SupportedLanguages...),
			SupportedFrameworks: append([]string(nil), version.Metadata.SupportedFrameworks...),
			SupportedAgents:     append([]string(nil), version.Metadata.SupportedAgents...),
			LegacyProvenance:    version.Metadata.LegacyProvenance,
		})
	}
	return infos
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
	repositoryRoot := filepath.Dir(source)
	versionDirectory := filepath.Dir(source)
	skillDirectory := filepath.Dir(versionDirectory)
	if filepath.Base(versionDirectory) == "versions" && filepath.Base(skillDirectory) == identifier {
		repositoryRoot = filepath.Dir(skillDirectory)
	}
	repository, err := skillrepo.Open(repositoryRoot)
	if err != nil {
		return false, fmt.Errorf("open skill repository for %s: %w", source, err)
	}
	versions, err := repository.VersionRecords(identifier)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) || os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("resolve Skill source %s: %w", source, err)
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return false, err
	}
	for _, version := range versions {
		if filepath.Clean(version.Path) == filepath.Clean(absSource) {
			selected, err := repository.Resolve(identifier, version.Key)
			if err != nil {
				if errors.Is(err, skillrepo.ErrNotFound) || os.IsNotExist(err) {
					return false, nil
				}
				return false, fmt.Errorf("resolve Skill source %s: %w", source, err)
			}
			if filepath.Clean(selected.Path) != filepath.Clean(absSource) {
				return false, nil
			}
			if _, err := readSkill(selected.Path, identifier); err != nil {
				return false, nil
			}
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
	skill, err := h.Resolve(absLibrary, identifier, "")
	if err != nil {
		if !errors.Is(err, skillrepo.ErrNotFound) {
			return ManagedResource{}, err
		}
		// Adoption plans need a stable destination before the new identifier
		// exists. Existing repository entries always resolve to their selected
		// content version above.
		skill = SkillCatalogEntry{Identifier: identifier, SourcePath: filepath.Join(absLibrary, identifier)}
	}
	return h.ManagedResource(skill)
}

// LibraryResourceVersion resolves an explicit repository version for a
// selected catalog entry.
func (h SkillHandler) LibraryResourceVersion(libraryPath, identifier, version string) (ManagedResource, error) {
	absLibrary, err := filepath.Abs(libraryPath)
	if err != nil {
		return ManagedResource{}, fmt.Errorf("resolve skill library path: %w", err)
	}
	skill, err := h.Resolve(absLibrary, identifier, version)
	if err != nil {
		return ManagedResource{}, err
	}
	return h.ManagedResource(skill)
}

// ManagesSource reports whether source is the canonical configured-library
// source for identifier.
func (h SkillHandler) ManagesSource(libraryPath, identifier, source string) (bool, error) {
	repository, err := skillrepo.Open(libraryPath)
	if err != nil {
		return false, err
	}
	return repository.OwnsPath(identifier, source)
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
	source, err := filepath.Abs(managed.Provenance.Source)
	if err != nil {
		return plan, err
	}
	if request.CatalogEntry != nil {
		version := request.CatalogEntry.VersionKey
		canonical, err := h.LibraryResourceVersion(request.LibraryPath, managed.ID, version)
		if err != nil {
			return plan, errors.Join(ErrUnsafeLifecycle, err)
		}
		if source != canonical.Provenance.Source {
			return plan, fmt.Errorf("%w: skill source is not the selected repository version", ErrUnsafeLifecycle)
		}
	} else {
		owned, err := h.ManagesSource(request.LibraryPath, managed.ID, source)
		if err != nil {
			return plan, errors.Join(ErrUnsafeLifecycle, err)
		}
		if !owned {
			return plan, fmt.Errorf("%w: skill source is not a configured repository version", ErrUnsafeLifecycle)
		}
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
	if request.RematerializeManaged {
		info, inspectErr := os.Lstat(request.PlacementPath)
		if inspectErr != nil {
			return plan, errors.Join(ErrUnsafeLifecycle, inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(request.PlacementPath)
			if readErr != nil {
				return plan, readErr
			}
			if !filepath.IsAbs(target) {
				target, readErr = filepath.Abs(filepath.Join(filepath.Dir(request.PlacementPath), target))
				if readErr != nil {
					return plan, readErr
				}
			}
			if filepath.Clean(target) != filepath.Clean(source) {
				return plan, ErrUnsafeLifecycle
			}
		} else if info.IsDir() {
			if !request.JournalCopyMatches || request.JournalSource != source {
				return plan, ErrUnsafeLifecycle
			}
		} else {
			return plan, ErrUnsafeLifecycle
		}
		conflict = true
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
	var managed ManagedResource
	var err error
	journalCopy := request.JournalCopyMatches && request.JournalMaterialization == "copy"
	if journalCopy {
		managed = ManagedResource{
			Version: "v1", ID: request.Identifier, Kind: Skill,
			Provenance: Provenance{Source: request.JournalSource},
		}
		if err := h.Validate(managed); err != nil {
			return plan, errors.Join(ErrUnsafeLifecycle, err)
		}
	} else if request.Action == LifecycleRemove {
		library, absErr := filepath.Abs(request.LibraryPath)
		if absErr != nil {
			return plan, absErr
		}
		managed, err = h.ManagedResource(SkillCatalogEntry{
			Identifier: request.Identifier,
			SourcePath: filepath.Join(library, request.Identifier),
		})
	} else {
		managed, err = h.LibraryResource(request.LibraryPath, request.Identifier)
	}
	if err != nil {
		return plan, errors.Join(ErrUnsafeLifecycle, err)
	}
	plan.Resource = managed
	if request.PlacementPath == "" {
		return plan, nil
	}
	source := ""
	if journalCopy {
		source, err = filepath.Abs(request.JournalSource)
		if err != nil {
			return plan, err
		}
		owned, err := h.ManagesSource(request.LibraryPath, request.Identifier, source)
		if err != nil {
			return plan, err
		}
		if !owned {
			return plan, ErrUnsafeLifecycle
		}
		eligible, err := h.Eligible(request.Identifier, source)
		if err != nil || !eligible {
			return plan, errors.Join(ErrUnsafeLifecycle, err)
		}
		if request.Action == LifecycleRemove {
			action = "remove managed copy"
		}
	} else {
		var owned bool
		source, owned, err = h.managedLink(request.LibraryPath, request.Identifier, request.PlacementPath)
		if err != nil {
			return plan, err
		}
		if !owned {
			return plan, ErrUnsafeLifecycle
		}
	}
	managed.Provenance.Source = source
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
	projectSkill, err := filepath.Abs(request.PlacementPath)
	if err != nil {
		return plan, fmt.Errorf("resolve project Skill path: %w", err)
	}
	libraryRoot, err := filepath.Abs(request.LibraryPath)
	if err != nil {
		return plan, fmt.Errorf("resolve Skill repository path: %w", err)
	}
	identityPath := filepath.Join(libraryRoot, request.Identifier)
	if _, err := os.Lstat(identityPath); err == nil {
		return plan, ErrLifecycleConflict
	} else if !os.IsNotExist(err) {
		return plan, err
	}
	checksum, err := skillrepo.ChecksumTree(projectSkill)
	if err != nil {
		return plan, fmt.Errorf("checksum project Skill: %w", err)
	}
	versionKey := "legacy-" + checksum[:12]
	libraryPath := filepath.Join(identityPath, "versions", versionKey)
	managed.Provenance.Source = libraryPath
	plan.Resource = managed
	plan.SourcePath = projectSkill
	plan.SourceChecksum = checksum
	plan.Applicable = true
	plan.Changes = []LifecycleChange{
		{Path: libraryPath, Action: "copy project skill into versioned library", Detail: projectSkill},
		{Path: projectSkill, Action: "replace directory with managed link", Detail: libraryPath},
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

func (h SkillHandler) managedLink(libraryPath, identifier, path string) (string, bool, error) {
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
	managed, err := h.ManagesSource(libraryPath, identifier, target)
	if err != nil {
		return "", false, err
	}
	return target, managed, nil
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
	contents, err := readSkillMarkdown(filepath.Join(path, "SKILL.md"))
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
	metadata, err := readRegularFile(filepath.Join(path, ".skill-manager.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return skill, nil
		}
		return SkillCatalogEntry{}, fmt.Errorf("read companion metadata: %w", err)
	}
	tags, compatibility, provenance, languages, frameworks, agents, err := parseCompanionMetadata(string(metadata))
	if err != nil {
		return SkillCatalogEntry{}, fmt.Errorf("parse companion metadata: %w", err)
	}
	skill.Tags = tags
	skill.Compatibility = compatibility
	skill.Provenance = provenance
	skill.SupportedLanguages = languages
	skill.SupportedFrameworks = frameworks
	skill.SupportedAgents = agents
	return skill, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func readSkillMarkdown(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular SKILL.md file", path)
	}
	return os.ReadFile(path)
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

func parseCompanionMetadata(contents string) (tags, compatibility []string, provenance string, languages, frameworks, agents []string, err error) {
	var metadata struct {
		Tags                []string `yaml:"tags"`
		Compatibility       []string `yaml:"compatibility"`
		Provenance          string   `yaml:"provenance"`
		SupportedLanguages  []string `yaml:"supportedLanguages"`
		SupportedFrameworks []string `yaml:"supportedFrameworks"`
		SupportedAgents     []string `yaml:"supportedAgents"`
	}
	if err := yaml.Unmarshal([]byte(contents), &metadata); err != nil {
		return nil, nil, "", nil, nil, nil, err
	}
	return metadata.Tags, metadata.Compatibility, metadata.Provenance, metadata.SupportedLanguages, metadata.SupportedFrameworks, metadata.SupportedAgents, nil
}
