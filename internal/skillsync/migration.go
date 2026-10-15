package skillsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

const MigrationOperation = "migrate existing Skill references"

var ErrMigrationBlocked = errors.New("selected Skill reference cannot be migrated")

type MigrationEntry struct {
	SourceID      string    `json:"sourceId"`
	Scope         Scope     `json:"scope"`
	Location      string    `json:"location"`
	Target        string    `json:"target"`
	EntryPath     string    `json:"entryPath"`
	OriginalLink  string    `json:"originalLink"`
	LinkChain     []LinkHop `json:"linkChain"`
	RealPath      string    `json:"realPath"`
	Checksum      string    `json:"checksum"`
	Identifier    string    `json:"identifier"`
	VersionKey    string    `json:"versionKey"`
	VersionPath   string    `json:"versionPath"`
	Method        string    `json:"method"`
	ImpactWarning string    `json:"impactWarning,omitempty"`
}

// MigrationPreview contains a single-scope, exact-version replacement plan.
// Its private fields are server-owned and never accepted from a client.
type MigrationPreview struct {
	Scope             Scope            `json:"scope"`
	SyncMode          string           `json:"syncMode"`
	SelectedSourceIDs []string         `json:"selectedSourceIds"`
	Entries           []MigrationEntry `json:"entries"`
	Plan              operation.Plan   `json:"plan"`
	Warnings          []string         `json:"warnings"`
	ConfirmationToken string           `json:"confirmationToken"`
	lifecyclePlan     operation.Plan
	skills            []catalog.Skill
	targets           []adapter.Target
	options           lifecycle.Options
	root              string
}

type MigrationResult struct {
	BatchID   string           `json:"batchId"`
	Scope     Scope            `json:"scope"`
	Operation operation.Plan   `json:"operation"`
	Entries   []MigrationEntry `json:"entries"`
}

// Root returns the registered filesystem scope bound to this server-owned
// preview. It is not serialized and cannot be supplied by a client.
func (preview MigrationPreview) Root() string { return preview.root }

// PreviewMigration resolves selected server-discovered external symlinks to
// exact matching repository versions and prepares one atomic scope plan.
func (s *Service) PreviewMigration(sourceIDs []string, mode adapter.MaterializationMode) (MigrationPreview, error) {
	if mode == "" {
		mode = adapter.MaterializeAuto
	}
	if err := mode.Validate(); err != nil {
		return MigrationPreview{}, err
	}
	selected := uniqueDiscoveryIDs(sourceIDs)
	if len(selected) == 0 || len(selected) != len(sourceIDs) {
		return MigrationPreview{}, ErrInvalidSelection
	}
	inventory, err := Discover(s.home, s.project, s.repositoryRoot, s.globalJournal, s.projectJournal)
	if err != nil {
		return MigrationPreview{}, err
	}
	if !inventory.Complete {
		return MigrationPreview{}, ErrIncompleteInventory
	}
	byID := make(map[string]Source, len(inventory.Sources))
	for _, source := range inventory.Sources {
		byID[source.ID] = source
	}
	var scope Scope
	sources := make([]Source, 0, len(selected))
	for _, id := range selected {
		source, ok := byID[id]
		if !ok || !source.Eligible || !source.MigrationEligible || source.EntryType != "external-symlink" || source.IdentityConflict || source.Target == "" {
			return MigrationPreview{}, fmt.Errorf("%w: source %s is import-only, changed, or unsupported", ErrMigrationBlocked, id)
		}
		if scope != "" && scope != source.Scope {
			return MigrationPreview{}, fmt.Errorf("%w: migrate one scope at a time", ErrInvalidSelection)
		}
		scope = source.Scope
		sources = append(sources, source)
	}
	repository, err := skillrepo.Open(s.repositoryRoot)
	if err != nil {
		return MigrationPreview{}, err
	}
	preview := MigrationPreview{
		Scope: scope, SyncMode: string(mode), SelectedSourceIDs: selected,
		Entries: make([]MigrationEntry, 0, len(sources)), Warnings: []string{},
		skills: []catalog.Skill{}, targets: []adapter.Target{},
	}
	options := lifecycle.Options{
		Conflict: lifecycle.ConflictReplace, Force: true,
		ExpectedConflictFingerprints: map[string]string{}, ExpectedSourceFingerprints: map[string]string{},
		TargetBindings: []lifecycle.TargetBinding{}, LinkDependencies: []operation.LinkDependency{},
		JournalOperation: MigrationOperation,
	}
	targetSet, versionSet := map[adapter.Target]bool{}, map[string]bool{}
	var root string
	if scope == ScopeGlobal {
		root = s.home
	} else {
		root = s.project
	}
	for _, source := range sources {
		agent, ok := adapter.ForAgent(source.Target)
		if !ok {
			return MigrationPreview{}, fmt.Errorf("%w: source target %q is not supported", ErrMigrationBlocked, source.Target)
		}
		expectedPath := agent.ProjectSkillPath(root, source.Identifier)
		if scope == ScopeGlobal {
			expectedPath = agent.GlobalSkillPath(root, source.Identifier)
		}
		if filepath.Clean(expectedPath) != filepath.Clean(source.Path) {
			return MigrationPreview{}, fmt.Errorf("%w: source entry is outside its adapter-owned Skill location", ErrMigrationBlocked)
		}
		if err := operation.NewBatchStore(filepath.Join(s.syncRoot, "batches")).CheckAvailable([]string{filepath.Join(s.repositoryRoot, source.Identifier)}); err != nil {
			return MigrationPreview{}, fmt.Errorf("%w: unresolved import batch for %s: %w", ErrMigrationBlocked, source.Identifier, err)
		}
		versions, err := repository.VersionRecords(source.Identifier)
		if err != nil {
			return MigrationPreview{}, fmt.Errorf("%w: import %s before migrating its reference: %v", ErrMigrationBlocked, source.Identifier, err)
		}
		matching := make([]skillrepo.Entry, 0, 1)
		for _, version := range versions {
			checksum := version.Metadata.Checksum
			if checksum == "" {
				checksum, err = skillrepo.ChecksumTree(version.Path)
				if err != nil {
					continue
				}
			}
			if checksum == source.Checksum {
				matching = append(matching, version)
			}
		}
		if len(matching) == 0 {
			return MigrationPreview{}, fmt.Errorf("%w: no stored version matches %s at %s", ErrMigrationBlocked, source.Identifier, source.Path)
		}
		sort.Slice(matching, func(i, j int) bool { return matching[i].Key < matching[j].Key })
		version, err := repository.Resolve(source.Identifier, matching[0].Key)
		if err != nil {
			return MigrationPreview{}, err
		}
		skill, err := resource.NewSkillHandler().Resolve(s.repositoryRoot, source.Identifier, version.Key)
		if err != nil {
			return MigrationPreview{}, err
		}
		if skill.Identifier != source.Identifier {
			return MigrationPreview{}, fmt.Errorf("%w: repository version identity does not match the entry", ErrMigrationBlocked)
		}
		sourceFingerprint, err := operation.FingerprintPath(version.Path)
		if err != nil {
			return MigrationPreview{}, err
		}
		entryFingerprint, err := operation.FingerprintPath(source.Path)
		if err != nil {
			return MigrationPreview{}, err
		}
		options.ExpectedSourceFingerprints[version.Path] = sourceFingerprint
		options.ExpectedConflictFingerprints[source.Path] = entryFingerprint
		options.TargetBindings = append(options.TargetBindings, lifecycle.TargetBinding{Target: source.Target, Skill: skill})
		if !versionSet[version.Path] {
			versionSet[version.Path] = true
			preview.skills = append(preview.skills, skill)
		}
		if !targetSet[source.Target] {
			targetSet[source.Target] = true
			preview.targets = append(preview.targets, source.Target)
		}
		chain := make([]operation.LinkDependencyHop, len(source.LinkChain))
		for i, hop := range source.LinkChain {
			chain[i] = operation.LinkDependencyHop{Path: hop.Path, Target: hop.Target}
		}
		options.LinkDependencies = append(options.LinkDependencies, operation.LinkDependency{EntryPath: source.Path, LinkChain: chain, RealPath: source.RealPath, SourceChecksum: source.Checksum})
		entry := MigrationEntry{
			SourceID: source.ID, Scope: source.Scope, Location: source.Location, Target: string(source.Target),
			EntryPath: source.Path, OriginalLink: source.OriginalLink, LinkChain: append([]LinkHop(nil), source.LinkChain...),
			RealPath: source.RealPath, Checksum: source.Checksum, Identifier: source.Identifier,
			VersionKey: version.Key, VersionPath: version.Path, Method: string(mode),
		}
		if scope == ScopeGlobal {
			entry.ImpactWarning = "Other projects that reference this global entry may follow the new target. They are not registered or included in this scan."
		}
		preview.Entries = append(preview.Entries, entry)
	}
	sort.Slice(preview.skills, func(i, j int) bool {
		if preview.skills[i].Identifier != preview.skills[j].Identifier {
			return preview.skills[i].Identifier < preview.skills[j].Identifier
		}
		return preview.skills[i].SourcePath < preview.skills[j].SourcePath
	})
	sort.Slice(preview.targets, func(i, j int) bool { return preview.targets[i] < preview.targets[j] })
	sort.Slice(options.TargetBindings, func(i, j int) bool {
		if options.TargetBindings[i].Target != options.TargetBindings[j].Target {
			return options.TargetBindings[i].Target < options.TargetBindings[j].Target
		}
		return options.TargetBindings[i].Skill.SourcePath < options.TargetBindings[j].Skill.SourcePath
	})
	if scope == ScopeGlobal {
		preview.Warnings = append(preview.Warnings, "This scans and changes only the selected global entries. Other projects may reference them indirectly; other projects are not registered or verified.")
	} else {
		preview.Warnings = append(preview.Warnings, "This changes only the selected entries in the current registered project. Other projects are not scanned.")
	}
	life := s.migrationLifecycle(scope, nil)
	life.Materialization = mode
	if scope == ScopeGlobal {
		preview.lifecyclePlan, err = life.PreviewGlobalMany(root, preview.skills, preview.targets, options)
	} else {
		preview.lifecyclePlan, err = life.PreviewMany(root, preview.skills, preview.targets, options)
	}
	if err != nil {
		return MigrationPreview{}, err
	}
	preview.Plan = preview.lifecyclePlan
	preview.Plan.Operation = MigrationOperation
	preview.options, preview.root = options, root
	token, err := migrationPreviewToken(preview)
	if err != nil {
		return MigrationPreview{}, err
	}
	preview.ConfirmationToken = token
	return preview, nil
}

// ApplyMigrationUntil recomputes the source inventory and plan immediately
// before applying a confirmed single-scope migration.
func (s *Service) ApplyMigrationUntil(reviewed MigrationPreview, expiresAt time.Time) (MigrationResult, error) {
	if reviewed.ConfirmationToken == "" || reviewed.ConfirmationToken != migrationMustToken(reviewed) {
		return MigrationResult{}, ErrStalePreview
	}
	unlockImport, err := s.lockImport()
	if err != nil {
		return MigrationResult{}, err
	}
	defer unlockImport()
	journal := s.projectJournal
	global := reviewed.Scope == ScopeGlobal
	if global {
		journal = s.globalJournal
	} else if reviewed.Scope != ScopeProject {
		return MigrationResult{}, ErrStalePreview
	}
	if journal == nil {
		return MigrationResult{}, fmt.Errorf("operation journal is unavailable for migration scope %q", reviewed.Scope)
	}
	unlockScope, err := journal.LockOperation()
	if err != nil {
		return MigrationResult{}, err
	}
	defer unlockScope()
	mode := adapter.MaterializationMode(reviewed.SyncMode)
	fresh, err := s.PreviewMigration(reviewed.SelectedSourceIDs, mode)
	if err != nil || !reflect.DeepEqual(fresh, reviewed) {
		return MigrationResult{}, errors.Join(ErrStalePreview, err)
	}
	if !expiresAt.IsZero() && !s.currentTime().Before(expiresAt) {
		return MigrationResult{}, ErrStalePreview
	}
	life := s.migrationLifecycle(reviewed.Scope, func(plan operation.Plan) bool { return reflect.DeepEqual(plan, reviewed.lifecyclePlan) })
	life.OperationLockHeld = true
	life.Materialization = mode
	selectedEntryPaths := make(map[string]struct{}, len(reviewed.Entries))
	for _, entry := range reviewed.Entries {
		path, err := filepath.Abs(entry.EntryPath)
		if err != nil {
			return MigrationResult{}, err
		}
		selectedEntryPaths[filepath.Clean(path)] = struct{}{}
	}
	if !expiresAt.IsZero() {
		life.BeforeFinalPublish = func() error {
			if !s.currentTime().Before(expiresAt) {
				return ErrStalePreview
			}
			return verifyMigrationLinkEvidence(reviewed.options.LinkDependencies, selectedEntryPaths)
		}
	} else {
		life.BeforeFinalPublish = func() error {
			return verifyMigrationLinkEvidence(reviewed.options.LinkDependencies, selectedEntryPaths)
		}
	}
	var applied operation.Plan
	if global {
		applied, err = life.ApplyGlobalMany(reviewed.root, reviewed.skills, reviewed.targets, reviewed.lifecyclePlan, true, reviewed.options)
	} else {
		applied, err = life.ApplyMany(reviewed.root, reviewed.skills, reviewed.targets, reviewed.lifecyclePlan, true, reviewed.options)
	}
	if err != nil {
		return MigrationResult{}, err
	}
	if life.LastBatchID == "" {
		return MigrationResult{}, fmt.Errorf("migration completed without a durable batch ID")
	}
	resultPlan := reviewed.Plan
	if len(applied.Changes) != len(resultPlan.Changes) {
		resultPlan.Changes = applied.Changes
	}
	return MigrationResult{BatchID: life.LastBatchID, Scope: reviewed.Scope, Operation: resultPlan, Entries: append([]MigrationEntry(nil), reviewed.Entries...)}, nil
}

func verifyMigrationLinkEvidence(dependencies []operation.LinkDependency, selectedEntryPaths map[string]struct{}) error {
	for _, dependency := range dependencies {
		for _, hop := range dependency.LinkChain {
			path, err := filepath.Abs(hop.Path)
			if err != nil {
				return errors.Join(ErrStalePreview, err)
			}
			path = filepath.Clean(path)
			if _, selected := selectedEntryPaths[path]; selected {
				continue
			}
			target, err := os.Readlink(path)
			if err != nil || target != hop.Target {
				return errors.Join(ErrStalePreview, fmt.Errorf("migration link dependency changed at %s", path), err)
			}
		}
		realPath, err := filepath.Abs(dependency.RealPath)
		if err != nil {
			return errors.Join(ErrStalePreview, err)
		}
		checksum, err := skillrepo.ChecksumTree(realPath)
		if err != nil || checksum != dependency.SourceChecksum {
			return errors.Join(ErrStalePreview, fmt.Errorf("migration source content changed at %s", realPath), err)
		}
	}
	return nil
}

func (s *Service) migrationLifecycle(scope Scope, confirm lifecycle.ConfirmFunc) *lifecycle.Service {
	journal := s.projectJournal
	if scope == ScopeGlobal {
		journal = s.globalJournal
	}
	life := lifecycle.New(s.repositoryRoot, journal, confirm)
	life.BatchStore = operation.NewBatchStore(filepath.Join(s.syncRoot, "batches"))
	return life
}

func migrationPreviewToken(preview MigrationPreview) (string, error) {
	copy := preview
	copy.ConfirmationToken = ""
	copy.lifecyclePlan = operation.Plan{}
	copy.skills, copy.targets, copy.options, copy.root = nil, nil, lifecycle.Options{}, ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func migrationMustToken(preview MigrationPreview) string {
	token, _ := migrationPreviewToken(preview)
	return token
}
