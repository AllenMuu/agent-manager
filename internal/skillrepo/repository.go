// Package skillrepo stores reusable Skills outside agent auto-discovery paths.
package skillrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AllenMuu/skill-manager/internal/filelock"
	"gopkg.in/yaml.v3"
)

const (
	MetadataFilename      = "metadata.json"
	MetadataSchemaVersion = 1
)

var (
	ErrNotFound           = errors.New("skill repository entry not found")
	ErrConflict           = errors.New("skill repository version already exists")
	ErrUnsafe             = errors.New("unsafe skill repository path or content")
	ErrRollbackIncomplete = errors.New("Skill repository transaction rollback was incomplete")
)

// Metadata is the repository index for one stable Skill identity.
type Metadata struct {
	SchemaVersion  int               `json:"schemaVersion"`
	Identifier     string            `json:"id"`
	CurrentVersion string            `json:"currentVersion"`
	Versions       []VersionMetadata `json:"versions"`
}

// VersionMetadata records one immutable content version and its known origin.
// Empty provenance fields mean unknown; consumers must not infer them from
// Skill instructions or directory names.
type VersionMetadata struct {
	Key                 string         `json:"key"`
	Version             string         `json:"version"`
	Source              string         `json:"source,omitempty"`
	SourceType          string         `json:"sourceType,omitempty"`
	Publisher           string         `json:"publisher,omitempty"`
	ImmutableRef        string         `json:"immutableRef,omitempty"`
	Checksum            string         `json:"checksum,omitempty"`
	Tags                []string       `json:"tags,omitempty"`
	SupportedLanguages  []string       `json:"supportedLanguages,omitempty"`
	SupportedFrameworks []string       `json:"supportedFrameworks,omitempty"`
	SupportedAgents     []string       `json:"supportedAgents,omitempty"`
	Compatibility       []string       `json:"compatibility,omitempty"`
	InstalledAt         time.Time      `json:"installedAt,omitempty"`
	UpdatedAt           time.Time      `json:"updatedAt,omitempty"`
	TrustLevel          string         `json:"trustLevel,omitempty"`
	VerificationStatus  string         `json:"verificationStatus,omitempty"`
	LastUsedAt          time.Time      `json:"lastUsedAt,omitempty"`
	LegacyProvenance    string         `json:"legacyProvenance,omitempty"`
	SourceOrigins       []SourceOrigin `json:"sourceOrigins,omitempty"`
}

// SourceOrigin records a supported local import location without inferring
// provenance from Skill-authored files.
type SourceOrigin struct {
	Scope    string `json:"scope"`
	Location string `json:"location"`
	Path     string `json:"path"`
}

// Entry is one versioned or legacy Skill resolved to its content directory.
type Entry struct {
	Identifier string
	Path       string
	Version    string
	Key        string
	Metadata   VersionMetadata
}

// Diagnostic describes a repository child that cannot be cataloged safely.
type Diagnostic struct {
	Path    string
	Message string
}

// Repository resolves logical Skills to version directories under one root.
type Repository struct {
	root string
}

// VersionSource describes a new identity/version to add as one local batch.
type VersionSource struct {
	Identifier  string
	SourcePath  string
	Metadata    VersionMetadata
	MakeCurrent bool
}

type publishedIdentity struct {
	entry    Entry
	info     os.FileInfo
	checksum string
}

var repositoryLocks sync.Map

// Open constructs a repository handle without creating its root.
func Open(root string) (*Repository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("skill repository path is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve skill repository path: %w", err)
	}
	return &Repository{root: filepath.Clean(absRoot)}, nil
}

// Root returns the absolute repository path.
func (r *Repository) Root() string { return r.root }

// PathsOverlap resolves existing symlinked ancestors before comparing two
// paths, including paths that have not been created yet.
func PathsOverlap(first, second string) (bool, error) {
	physicalFirst, err := resolvePhysicalPath(first)
	if err != nil {
		return false, err
	}
	physicalSecond, err := resolvePhysicalPath(second)
	if err != nil {
		return false, err
	}
	return pathWithin(physicalFirst, physicalSecond) || pathWithin(physicalSecond, physicalFirst), nil
}

// Discover returns each versioned Skill's selected version and each eligible
// flat directory Skill found in an explicitly configured legacy library.
func (r *Repository) Discover() ([]Entry, []Diagnostic, error) {
	rootInfo, err := os.Lstat(r.root)
	if os.IsNotExist(err) {
		return []Entry{}, []Diagnostic{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("inspect skill repository: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%w: repository root is not a real directory", ErrUnsafe)
	}
	children, err := os.ReadDir(r.root)
	if err != nil {
		return nil, nil, fmt.Errorf("read skill repository: %w", err)
	}

	entries := make([]Entry, 0, len(children))
	diagnostics := make([]Diagnostic, 0)
	for _, child := range children {
		path := filepath.Join(r.root, child.Name())
		info, err := os.Lstat(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("inspect entry: %v", err)})
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entry, err := r.resolve(child.Name(), "")
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				if child.IsDir() {
					diagnostics = append(diagnostics, Diagnostic{Path: path, Message: "missing SKILL.md or repository metadata"})
				}
				continue
			}
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Identifier < entries[j].Identifier })
	return entries, diagnostics, nil
}

// Resolve selects the current version when version is empty. A version may be
// addressed by its declared version string or its repository storage key.
func (r *Repository) Resolve(identifier, version string) (Entry, error) {
	if !validComponent(identifier) || (version != "" && !validComponent(version)) {
		return Entry{}, fmt.Errorf("%w: invalid Skill identifier or version", ErrUnsafe)
	}
	return r.resolve(identifier, version)
}

// Versions returns every stored version for identifier in stable key order.
func (r *Repository) Versions(identifier string) ([]Entry, error) {
	if !validComponent(identifier) {
		return nil, fmt.Errorf("%w: invalid Skill identifier", ErrUnsafe)
	}
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, false); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, identifier)
		}
		return nil, err
	}
	skillRoot := filepath.Join(r.root, identifier)
	if err := requireRealDirectory(r.root, skillRoot, false); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, identifier)
		}
		return nil, err
	}
	metadata, err := readMetadata(skillRoot, identifier)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			entry, legacyErr := r.resolve(identifier, "")
			if legacyErr != nil {
				return nil, legacyErr
			}
			return []Entry{entry}, nil
		}
		return nil, err
	}

	versions := append([]VersionMetadata(nil), metadata.Versions...)
	sort.Slice(versions, func(i, j int) bool { return versions[i].Key < versions[j].Key })
	out := make([]Entry, 0, len(versions))
	for _, version := range versions {
		entry, err := entryForVersion(r.root, identifier, version)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// VersionRecords returns the indexed versions without inspecting their content
// directories. Call Resolve for the specific version whose content will be
// used; an unrelated damaged version must not block that resolution.
func (r *Repository) VersionRecords(identifier string) ([]Entry, error) {
	if !validComponent(identifier) {
		return nil, fmt.Errorf("%w: invalid Skill identifier", ErrUnsafe)
	}
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, false); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, identifier)
		}
		return nil, err
	}
	skillRoot := filepath.Join(r.root, identifier)
	if err := requireRealDirectory(r.root, skillRoot, false); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, identifier)
		}
		return nil, err
	}
	metadata, err := readMetadata(skillRoot, identifier)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			entry, legacyErr := r.resolve(identifier, "")
			if legacyErr != nil {
				return nil, legacyErr
			}
			return []Entry{entry}, nil
		}
		return nil, err
	}
	versions := append([]VersionMetadata(nil), metadata.Versions...)
	sort.Slice(versions, func(i, j int) bool { return versions[i].Key < versions[j].Key })
	out := make([]Entry, 0, len(versions))
	for _, version := range versions {
		if !validComponent(version.Key) {
			return nil, fmt.Errorf("%w: invalid version key %q", ErrUnsafe, version.Key)
		}
		path := filepath.Join(skillRoot, "versions", version.Key)
		if err := ensureWithin(r.root, path); err != nil {
			return nil, err
		}
		out = append(out, Entry{Identifier: identifier, Path: path, Version: version.Version, Key: version.Key, Metadata: version})
	}
	return out, nil
}

// IdentityPath returns the canonical root for a known versioned or legacy
// Skill identity. It validates the index/legacy marker and rejects unsafe
// descendants, but does not require every recorded version's checksum to
// match. This is intended for whole-identity operations such as confirmed
// deletion.
func (r *Repository) IdentityPath(identifier string) (string, error) {
	if !validComponent(identifier) {
		return "", fmt.Errorf("%w: invalid Skill identifier", ErrUnsafe)
	}
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, false); err != nil {
		return "", err
	}
	skillRoot := filepath.Join(r.root, identifier)
	if err := requireRealDirectory(r.root, skillRoot, false); err != nil {
		return "", err
	}
	if _, err := readMetadata(skillRoot, identifier); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := requireRegularFile(skillRoot, filepath.Join(skillRoot, "SKILL.md")); err != nil {
			return "", fmt.Errorf("legacy Skill identity is not eligible: %w", err)
		}
	}
	if _, err := treeChecksum(skillRoot); err != nil {
		return "", fmt.Errorf("inspect Skill identity: %w", err)
	}
	return skillRoot, nil
}

// OwnsPath reports whether path is the canonical flat legacy directory or a
// recorded version directory for identifier. It does not require version
// content to remain present, which lets guarded removal recognize a dangling
// activation link without claiming arbitrary paths.
func (r *Repository) OwnsPath(identifier, path string) (bool, error) {
	if !validComponent(identifier) {
		return false, fmt.Errorf("%w: invalid Skill identifier", ErrUnsafe)
	}
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, false); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	skillRoot := filepath.Join(r.root, identifier)
	if _, err := os.Lstat(skillRoot); os.IsNotExist(err) {
		// A dangling activation may outlive a legacy library directory. Its
		// exact canonical path is still attributable to this repository.
		return filepath.Clean(absPath) == filepath.Clean(skillRoot), nil
	} else if err != nil {
		return false, err
	}
	if err := requireRealDirectory(r.root, skillRoot, false); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	metadata, err := readMetadata(skillRoot, identifier)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Clean(absPath) == filepath.Clean(skillRoot), nil
	}
	if err != nil {
		return false, err
	}
	for _, version := range metadata.Versions {
		if !validComponent(version.Key) {
			return false, fmt.Errorf("%w: invalid version key %q", ErrUnsafe, version.Key)
		}
		versionPath := filepath.Join(skillRoot, "versions", version.Key)
		if err := ensureWithin(r.root, versionPath); err != nil {
			return false, err
		}
		if filepath.Clean(absPath) == filepath.Clean(versionPath) {
			return true, nil
		}
	}
	return false, nil
}

// AddVersion copies sourcePath into a new immutable version directory. Existing
// content is never overwritten. makeCurrent selects the added version for
// subsequent default resolutions.
func (r *Repository) AddVersion(identifier, sourcePath string, version VersionMetadata, makeCurrent bool) (Entry, error) {
	if !validComponent(identifier) {
		return Entry{}, fmt.Errorf("%w: invalid Skill identifier %q", ErrUnsafe, identifier)
	}
	checksum, err := treeChecksum(sourcePath)
	if err != nil {
		return Entry{}, fmt.Errorf("inspect Skill source: %w", err)
	}
	if version.Checksum != "" && version.Checksum != checksum {
		return Entry{}, fmt.Errorf("%w: Skill source checksum changed after preview", ErrUnsafe)
	}
	lock := repositoryLock(r.root)
	lock.Lock()
	defer lock.Unlock()
	unlock, err := lockRepositoryFile(r.root)
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	return r.addVersion(identifier, sourcePath, version, makeCurrent)
}

// AddVersionsBatch atomically applies a batch of new Skill identities. The
// operation refuses every pre-existing identity and removes entries it created
// if a later write fails.
func (r *Repository) AddVersionsBatch(sources []VersionSource) ([]Entry, error) {
	lock := repositoryLock(r.root)
	lock.Lock()
	defer lock.Unlock()
	unlock, err := lockRepositoryFile(r.root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return r.addVersionsBatch(sources, nil)
}

func lockRepositoryFile(root string) (func() error, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create Skill repository for its operation lock: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: Skill repository root is not a real directory", ErrUnsafe)
	}
	return filelock.Acquire(filepath.Join(root, ".skill-manager-operation.lock"))
}

func (r *Repository) addVersionsBatch(sources []VersionSource, afterPublish func(int, Entry) error) ([]Entry, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: batch has no Skills", ErrConflict)
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if !validComponent(source.Identifier) {
			return nil, fmt.Errorf("%w: invalid Skill identifier %q", ErrUnsafe, source.Identifier)
		}
		if _, exists := seen[source.Identifier]; exists {
			return nil, fmt.Errorf("%w: duplicate Skill identifier %q", ErrConflict, source.Identifier)
		}
		seen[source.Identifier] = struct{}{}
		destination := filepath.Join(r.root, source.Identifier)
		if err := ensureWithin(r.root, destination); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(destination); err == nil {
			return nil, fmt.Errorf("%w: Skill %s already exists", ErrConflict, source.Identifier)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect Skill destination %s: %w", source.Identifier, err)
		}
	}

	created := make([]publishedIdentity, 0, len(sources))
	rollback := func() error {
		var rollbackErrs []error
		for i := len(created) - 1; i >= 0; i-- {
			if err := rollbackPublishedIdentity(r.root, created[i]); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback Skill %s: %w", created[i].entry.Identifier, err))
			}
		}
		return errors.Join(rollbackErrs...)
	}
	for i, source := range sources {
		entry, err := r.addVersion(source.Identifier, source.SourcePath, source.Metadata, source.MakeCurrent)
		if err != nil {
			return nil, errors.Join(err, rollback())
		}
		identityPath := filepath.Join(r.root, source.Identifier)
		identityInfo, err := os.Lstat(identityPath)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("capture rollback ownership for Skill %s: %w (published entry may remain)", source.Identifier, err), rollback())
		}
		identityChecksum, err := treeChecksum(identityPath)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("capture rollback ownership for Skill %s: %w (published entry may remain)", source.Identifier, err), rollback())
		}
		created = append(created, publishedIdentity{entry: entry, info: identityInfo, checksum: identityChecksum})
		if afterPublish != nil {
			if err := afterPublish(i, entry); err != nil {
				return nil, errors.Join(err, rollback())
			}
		}
	}
	entries := make([]Entry, len(created))
	for i := range created {
		entries[i] = created[i].entry
	}
	return entries, nil
}

func rollbackPublishedIdentity(root string, owned publishedIdentity) error {
	identifier := owned.entry.Identifier
	path := filepath.Join(root, identifier)
	if err := ensureWithin(root, path); err != nil {
		return fmt.Errorf("validate rollback path %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect rollback path %s: %w", path, err)
	}
	if !os.SameFile(owned.info, info) {
		return fmt.Errorf("%w: preserved replacement at %s during batch rollback", ErrConflict, path)
	}
	checksum, err := treeChecksum(path)
	if err != nil {
		return fmt.Errorf("verify rollback ownership for %s: %w", path, err)
	}
	if checksum != owned.checksum {
		return fmt.Errorf("%w: preserved changed Skill identity %s during batch rollback", ErrConflict, identifier)
	}

	stage, err := os.MkdirTemp(root, ".skill-manager-rollback-")
	if err != nil {
		return fmt.Errorf("stage rollback for %s: %w", identifier, err)
	}
	moved := filepath.Join(stage, identifier)
	if err := os.Rename(path, moved); err != nil {
		_ = os.Remove(stage)
		return fmt.Errorf("move rollback identity %s: %w", identifier, err)
	}
	restoreUnexpected := func(reason error) error {
		if _, pathErr := os.Lstat(path); os.IsNotExist(pathErr) {
			if restoreErr := os.Rename(moved, path); restoreErr == nil {
				_ = os.Remove(stage)
				return reason
			} else {
				return errors.Join(reason, fmt.Errorf("preserved rollback staging at %s: %w", stage, restoreErr))
			}
		}
		return fmt.Errorf("%w; preserved rollback staging at %s", reason, stage)
	}
	movedInfo, err := os.Lstat(moved)
	if err != nil {
		return fmt.Errorf("inspect staged rollback identity %s: %w; preserved at %s", identifier, err, stage)
	}
	if !os.SameFile(owned.info, movedInfo) {
		return restoreUnexpected(fmt.Errorf("%w: staged replacement for Skill %s", ErrConflict, identifier))
	}
	movedChecksum, err := treeChecksum(moved)
	if err != nil {
		return restoreUnexpected(fmt.Errorf("verify staged rollback ownership for %s: %w", identifier, err))
	}
	if movedChecksum != owned.checksum {
		return restoreUnexpected(fmt.Errorf("%w: staged Skill %s changed during rollback", ErrConflict, identifier))
	}
	if err := removeTreeWritable(moved); err != nil {
		return fmt.Errorf("remove owned rollback identity %s: %w; preserved staging at %s", identifier, err, stage)
	}
	if err := os.Remove(stage); err != nil {
		return fmt.Errorf("remove rollback staging %s: %w", stage, err)
	}
	return nil
}

func repositoryLock(root string) *sync.Mutex {
	lock, _ := repositoryLocks.LoadOrStore(filepath.Clean(root), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (r *Repository) addVersion(identifier, sourcePath string, version VersionMetadata, makeCurrent bool) (result Entry, resultErr error) {
	if !validComponent(identifier) {
		return Entry{}, fmt.Errorf("%w: invalid Skill identifier %q", ErrUnsafe, identifier)
	}
	overlaps, err := PathsOverlap(sourcePath, filepath.Join(r.root, identifier))
	if err != nil {
		return Entry{}, fmt.Errorf("resolve Skill source and destination paths: %w", err)
	}
	if overlaps {
		return Entry{}, fmt.Errorf("%w: Skill source and repository destination overlap", ErrUnsafe)
	}
	checksum, err := treeChecksum(sourcePath)
	if err != nil {
		return Entry{}, fmt.Errorf("inspect Skill source: %w", err)
	}
	if version.Key == "" {
		if version.Version != "" {
			version.Key = version.Version
		} else {
			version.Key = "legacy-" + checksum[:12]
		}
	}
	if version.Version == "" {
		version.Version = "unknown"
	}
	if !validComponent(version.Key) {
		return Entry{}, fmt.Errorf("%w: invalid Skill version key %q", ErrUnsafe, version.Key)
	}
	if version.Checksum == "" {
		version.Checksum = checksum
	} else if version.Checksum != checksum {
		return Entry{}, fmt.Errorf("%w: Skill source checksum changed after preview", ErrUnsafe)
	}
	if version.Source == "" {
		absSource, absErr := filepath.Abs(sourcePath)
		if absErr != nil {
			return Entry{}, fmt.Errorf("resolve Skill source path: %w", absErr)
		}
		version.Source = absSource
	}
	if version.SourceType == "" {
		version.SourceType = "local"
	}
	if version.TrustLevel == "" {
		version.TrustLevel = "unknown"
	}
	if version.VerificationStatus == "" {
		version.VerificationStatus = "unknown"
	}
	now := time.Now().UTC()
	if version.InstalledAt.IsZero() {
		version.InstalledAt = now
	}
	if version.UpdatedAt.IsZero() {
		version.UpdatedAt = now
	}
	if err := mergeCompanionMetadata(sourcePath, &version); err != nil {
		return Entry{}, err
	}

	skillRoot := filepath.Join(r.root, identifier)
	if err := ensureWithin(r.root, skillRoot); err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return Entry{}, fmt.Errorf("create skill repository: %w", err)
	}
	rootInfo, err := os.Lstat(r.root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Entry{}, fmt.Errorf("%w: repository root is not a real directory", ErrUnsafe)
	}

	skillInfo, skillInfoErr := os.Lstat(skillRoot)
	newSkill := os.IsNotExist(skillInfoErr)
	if skillInfoErr != nil && !newSkill {
		return Entry{}, fmt.Errorf("inspect Skill destination: %w", skillInfoErr)
	}
	if !newSkill && (!skillInfo.IsDir() || skillInfo.Mode()&os.ModeSymlink != 0) {
		return Entry{}, fmt.Errorf("%w: Skill destination is not a real directory", ErrUnsafe)
	}
	metadata := Metadata{}
	var metadataErr error
	if !newSkill {
		metadata, metadataErr = readMetadata(skillRoot, identifier)
	}
	if metadataErr != nil && !errors.Is(metadataErr, os.ErrNotExist) {
		return Entry{}, metadataErr
	}
	if newSkill {
		metadata = Metadata{SchemaVersion: MetadataSchemaVersion, Identifier: identifier, Versions: []VersionMetadata{}}
	} else if errors.Is(metadataErr, os.ErrNotExist) {
		return Entry{}, fmt.Errorf("%w: existing flat Skill must be imported into a fresh repository", ErrConflict)
	}
	for _, existing := range metadata.Versions {
		if existing.Key == version.Key || (version.Version != "unknown" && existing.Version == version.Version) {
			return Entry{}, fmt.Errorf("%w: %s version %s", ErrConflict, identifier, version.Key)
		}
	}

	versionsRoot := filepath.Join(skillRoot, "versions")
	versionRoot := filepath.Join(versionsRoot, version.Key)
	if err := ensureWithin(r.root, versionRoot); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(versionRoot); err == nil {
		return Entry{}, fmt.Errorf("%w: %s version %s", ErrConflict, identifier, version.Key)
	} else if !os.IsNotExist(err) {
		return Entry{}, fmt.Errorf("inspect version destination: %w", err)
	}
	createdSkill := false
	committed := false
	if newSkill {
		if err := os.Mkdir(skillRoot, 0o755); err != nil {
			if os.IsExist(err) {
				return Entry{}, fmt.Errorf("%w: Skill %s was created concurrently", ErrConflict, identifier)
			}
			return Entry{}, fmt.Errorf("create Skill identity directory: %w", err)
		}
		createdSkill = true
		if err := os.Mkdir(versionsRoot, 0o755); err != nil {
			cleanupErr := removeTreeWritable(skillRoot)
			return Entry{}, errors.Join(fmt.Errorf("create version directory: %w", err), cleanupErr)
		}
	} else {
		if err := os.Mkdir(versionsRoot, 0o755); err != nil && !os.IsExist(err) {
			return Entry{}, fmt.Errorf("create version directory: %w", err)
		}
	}
	if createdSkill {
		defer func() {
			if !committed {
				if err := removeTreeWritable(skillRoot); err != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("rollback Skill identity %s: %w", identifier, err))
				}
			}
		}()
	}
	if err := requireRealDirectory(r.root, skillRoot, true); err != nil {
		return Entry{}, err
	}
	if err := requireRealDirectory(skillRoot, versionsRoot, false); err != nil {
		return Entry{}, err
	}
	stage, err := os.MkdirTemp(versionsRoot, ".stage-")
	if err != nil {
		return Entry{}, fmt.Errorf("create version staging directory: %w", err)
	}
	defer func() {
		if err := removeTreeWritable(stage); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("clean version staging directory: %w", err))
		}
	}()
	if err := copyTree(sourcePath, stage); err != nil {
		return Entry{}, fmt.Errorf("copy Skill version: %w", err)
	}
	stagedChecksum, err := treeChecksum(stage)
	if err != nil {
		return Entry{}, fmt.Errorf("verify staged Skill version: %w", err)
	}
	if stagedChecksum != checksum {
		return Entry{}, fmt.Errorf("%w: Skill source changed while being copied", ErrUnsafe)
	}
	if err := os.Rename(stage, versionRoot); err != nil {
		return Entry{}, fmt.Errorf("publish Skill version: %w", err)
	}

	metadata.Versions = append(metadata.Versions, version)
	if metadata.CurrentVersion == "" || makeCurrent {
		metadata.CurrentVersion = version.Key
	}
	if err := writeMetadata(skillRoot, metadata); err != nil {
		return Entry{}, errors.Join(err, removeTreeWritable(versionRoot))
	}
	committed = true
	return entryForVersion(r.root, identifier, version)
}

func (r *Repository) resolve(identifier, version string) (Entry, error) {
	if !validComponent(identifier) {
		return Entry{}, fmt.Errorf("%w: invalid Skill identifier %q", ErrUnsafe, identifier)
	}
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, false); err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s: %w", ErrNotFound, identifier, err)
		}
		return Entry{}, err
	}
	skillRoot := filepath.Join(r.root, identifier)
	if err := requireRealDirectory(r.root, skillRoot, false); err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s: %w", ErrNotFound, identifier, err)
		}
		return Entry{}, err
	}
	metadata, err := readMetadata(skillRoot, identifier)
	if err == nil {
		key := version
		if key == "" {
			key = metadata.CurrentVersion
		}
		var selected *VersionMetadata
		for i := range metadata.Versions {
			item := &metadata.Versions[i]
			if item.Key == key || (version != "" && item.Version == version) {
				if selected != nil && selected.Key != item.Key {
					return Entry{}, fmt.Errorf("ambiguous Skill version %q for %s", version, identifier)
				}
				selected = item
			}
		}
		if selected == nil {
			return Entry{}, fmt.Errorf("%w: %s version %q", ErrNotFound, identifier, version)
		}
		return entryForVersion(r.root, identifier, *selected)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Entry{}, err
	}
	if version != "" && version != "legacy" {
		return Entry{}, fmt.Errorf("%w: %s version %q", ErrNotFound, identifier, version)
	}
	if err := requireSkillMarkdown(skillRoot, filepath.Join(skillRoot, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s: %w", ErrNotFound, identifier, err)
		}
		return Entry{}, err
	}
	legacy := VersionMetadata{Key: "legacy", Version: "unknown", TrustLevel: "unknown", VerificationStatus: "unknown"}
	if err := mergeCompanionMetadata(skillRoot, &legacy); err != nil {
		return Entry{}, err
	}
	return Entry{Identifier: identifier, Path: skillRoot, Version: legacy.Version, Key: legacy.Key, Metadata: legacy}, nil
}

func entryForVersion(root, identifier string, metadata VersionMetadata) (Entry, error) {
	if !validComponent(metadata.Key) {
		return Entry{}, fmt.Errorf("%w: invalid version key %q", ErrUnsafe, metadata.Key)
	}
	skillRoot := filepath.Join(root, identifier)
	if err := requireRealDirectory(filepath.Dir(root), root, false); err != nil {
		return Entry{}, err
	}
	versionsRoot := filepath.Join(skillRoot, "versions")
	versionPath := filepath.Join(versionsRoot, metadata.Key)
	if err := ensureWithin(root, versionPath); err != nil {
		return Entry{}, err
	}
	if err := requireRealDirectory(root, skillRoot, false); err != nil {
		return Entry{}, err
	}
	if err := requireRealDirectory(skillRoot, versionsRoot, false); err != nil {
		return Entry{}, err
	}
	if err := requireRealDirectory(versionsRoot, versionPath, false); err != nil {
		return Entry{}, err
	}
	if err := requireRegularFile(versionPath, filepath.Join(versionPath, "SKILL.md")); err != nil {
		return Entry{}, fmt.Errorf("version %s/%s is not an eligible Skill: %w", identifier, metadata.Key, err)
	}
	if metadata.Checksum != "" {
		checksum, err := treeChecksum(versionPath)
		if err != nil {
			return Entry{}, fmt.Errorf("verify version %s/%s checksum: %w", identifier, metadata.Key, err)
		}
		if checksum != metadata.Checksum {
			return Entry{}, fmt.Errorf("%w: version %s/%s content checksum does not match metadata", ErrUnsafe, identifier, metadata.Key)
		}
	}
	return Entry{Identifier: identifier, Path: versionPath, Version: metadata.Version, Key: metadata.Key, Metadata: metadata}, nil
}

func readMetadata(skillRoot, identifier string) (Metadata, error) {
	metadataPath := filepath.Join(skillRoot, MetadataFilename)
	if err := requireRegularFile(skillRoot, metadataPath); err != nil {
		return Metadata{}, err
	}
	contents, err := os.ReadFile(metadataPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("read Skill repository metadata: %w", err)
	}
	var metadata Metadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse Skill repository metadata: %w", err)
	}
	if metadata.SchemaVersion != MetadataSchemaVersion {
		return Metadata{}, fmt.Errorf("unsupported Skill repository metadata schema %d", metadata.SchemaVersion)
	}
	if metadata.Identifier != identifier {
		return Metadata{}, fmt.Errorf("Skill repository metadata id %q does not match directory %q", metadata.Identifier, identifier)
	}
	if !validComponent(metadata.CurrentVersion) {
		return Metadata{}, fmt.Errorf("%w: invalid current version key %q", ErrUnsafe, metadata.CurrentVersion)
	}
	if len(metadata.Versions) == 0 {
		return Metadata{}, fmt.Errorf("Skill repository metadata has no versions")
	}
	seenKeys := make(map[string]struct{}, len(metadata.Versions))
	currentFound := false
	for _, version := range metadata.Versions {
		if !validComponent(version.Key) {
			return Metadata{}, fmt.Errorf("%w: invalid version key %q", ErrUnsafe, version.Key)
		}
		if _, exists := seenKeys[version.Key]; exists {
			return Metadata{}, fmt.Errorf("duplicate Skill version key %q", version.Key)
		}
		seenKeys[version.Key] = struct{}{}
		if version.Key == metadata.CurrentVersion {
			currentFound = true
		}
	}
	if !currentFound {
		return Metadata{}, fmt.Errorf("current Skill version %q is not declared", metadata.CurrentVersion)
	}
	return metadata, nil
}

func writeMetadata(skillRoot string, metadata Metadata) error {
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Skill repository metadata: %w", err)
	}
	contents = append(contents, '\n')
	file, err := os.CreateTemp(skillRoot, ".metadata-*.tmp")
	if err != nil {
		return fmt.Errorf("stage Skill repository metadata: %w", err)
	}
	stage := file.Name()
	defer os.Remove(stage)
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write Skill repository metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync Skill repository metadata: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Skill repository metadata: %w", err)
	}
	if err := os.Rename(stage, filepath.Join(skillRoot, MetadataFilename)); err != nil {
		return fmt.Errorf("publish Skill repository metadata: %w", err)
	}
	return nil
}

func mergeCompanionMetadata(skillPath string, metadata *VersionMetadata) error {
	path := filepath.Join(skillPath, ".skill-manager.yaml")
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect companion metadata: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: companion metadata is not a regular file", ErrUnsafe)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read companion metadata: %w", err)
	}
	var legacy struct {
		Tags                []string `yaml:"tags"`
		Compatibility       []string `yaml:"compatibility"`
		SupportedLanguages  []string `yaml:"supportedLanguages"`
		SupportedFrameworks []string `yaml:"supportedFrameworks"`
		SupportedAgents     []string `yaml:"supportedAgents"`
		Provenance          string   `yaml:"provenance"`
	}
	if err := yaml.Unmarshal(contents, &legacy); err != nil {
		return fmt.Errorf("parse companion metadata: %w", err)
	}
	if len(metadata.Tags) == 0 {
		metadata.Tags = legacy.Tags
	}
	if len(metadata.Compatibility) == 0 {
		metadata.Compatibility = legacy.Compatibility
	}
	if len(metadata.SupportedLanguages) == 0 {
		metadata.SupportedLanguages = legacy.SupportedLanguages
	}
	if len(metadata.SupportedFrameworks) == 0 {
		metadata.SupportedFrameworks = legacy.SupportedFrameworks
	}
	if len(metadata.SupportedAgents) == 0 {
		metadata.SupportedAgents = legacy.SupportedAgents
	}
	if metadata.LegacyProvenance == "" {
		metadata.LegacyProvenance = legacy.Provenance
	}
	return nil
}

func validComponent(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00")
}

func ensureWithin(root, path string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absRoot, absPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: path escapes repository root", ErrUnsafe)
	}
	return nil
}

func resolvePhysicalPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)
	current := absPath
	missing := make([]string, 0)
	for {
		_, statErr := os.Lstat(current)
		if statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absPath, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func requireRealDirectory(root, path string, allowMissing bool) error {
	if err := ensureWithin(root, path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafe, path)
	}
	return nil
}

func requireRegularFile(parent, path string) error {
	if err := ensureWithin(parent, path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
	}
	return nil
}

func requireSkillMarkdown(parent, path string) error {
	if err := ensureWithin(parent, path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular SKILL.md file", ErrUnsafe, path)
	}
	return nil
}

func treeChecksum(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := requireRealDirectory(filepath.Dir(absRoot), absRoot, false); err != nil {
		return "", err
	}
	hash := sha256.New()
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%w: Skill contains a symlink or special file at %s", ErrUnsafe, path)
		}
		relative, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		_, _ = hash.Write([]byte{0})
		if info.IsDir() {
			_, _ = hash.Write([]byte{'d'})
			return nil
		}
		_, _ = hash.Write([]byte{'f'})
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ChecksumTree returns the deterministic SHA-256 checksum for a safe regular
// file/directory tree. Symlinks and special files are rejected.
func ChecksumTree(root string) (string, error) { return treeChecksum(root) }

func copyTree(source, destination string) error {
	if _, err := treeChecksum(source); err != nil {
		return err
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, sourceInfo.Mode().Perm()|0o700); err != nil {
		return err
	}
	type copiedDirectory struct {
		path string
		mode os.FileMode
	}
	var directories []copiedDirectory
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(target, info.Mode().Perm()|0o700); err != nil {
				return err
			}
			directories = append(directories, copiedDirectory{path: target, mode: info.Mode().Perm()})
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: cannot copy non-regular Skill path %s", ErrUnsafe, path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputErr := input.Close()
		outputErr := output.Close()
		return errors.Join(copyErr, inputErr, outputErr)
	}); err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return fmt.Errorf("preserve directory mode for %s: %w", directories[i].path, err)
		}
	}
	if err := os.Chmod(destination, sourceInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve root directory mode for %s: %w", destination, err)
	}
	return nil
}

func removeTreeWritable(path string) error {
	firstErr := os.RemoveAll(path)
	if firstErr == nil {
		return nil
	}
	var permissionErrs []error
	walkErr := filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		permissions := os.FileMode(0o600)
		if info.IsDir() {
			permissions = 0o700
		}
		if err := os.Chmod(current, info.Mode().Perm()|permissions); err != nil {
			permissionErrs = append(permissionErrs, fmt.Errorf("make %s removable: %w", current, err))
		}
		return nil
	})
	if retryErr := os.RemoveAll(path); retryErr == nil {
		return nil
	} else {
		return errors.Join(fmt.Errorf("remove %s: %w", path, firstErr), walkErr, errors.Join(permissionErrs...), fmt.Errorf("retry removing %s: %w", path, retryErr))
	}
}
