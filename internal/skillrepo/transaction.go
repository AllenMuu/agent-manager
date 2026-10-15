package skillrepo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// identitySnapshot preserves a repository identity while a guarded batch is
// being published so a failed batch can roll back its exact prior state.
type identitySnapshot struct {
	Identifier string
	Exists     bool
	Checksum   string
	Backup     string
}

// ImportResult describes the versions published by one local import.
type ImportResult struct {
	Entries []Entry
}

// ImportSnapshot is durable before-state evidence for one repository identity.
type ImportSnapshot struct {
	Identifier string
	Exists     bool
	Checksum   string
	Backup     string
}

// ImportHooks expose the write-ahead boundaries around an atomic import.
type ImportHooks struct {
	BeforePublish func([]ImportSnapshot) error
	AfterPublish  func(int, Entry) error
}

// AddVersionsAtomic adds selected immutable versions as one repository
// transaction. It permits multiple versions for an identity, and rolls back
// only identities whose exact post-state is still owned by this batch.
func (r *Repository) AddVersionsAtomic(sources []VersionSource, backupRoot string, expectedBefore map[string]string, afterPublish func(int, Entry) error) (ImportResult, error) {
	return r.AddVersionsAtomicWithHooks(sources, backupRoot, expectedBefore, ImportHooks{AfterPublish: afterPublish})
}

// AddVersionsAtomicWithHooks publishes selected immutable versions as one
// repository transaction and exposes the durable backup boundary before the
// first version is published.
func (r *Repository) AddVersionsAtomicWithHooks(sources []VersionSource, backupRoot string, expectedBefore map[string]string, hooks ImportHooks) (ImportResult, error) {
	lock := repositoryLock(r.root)
	lock.Lock()
	defer lock.Unlock()
	if len(sources) == 0 {
		return ImportResult{}, fmt.Errorf("%w: batch has no new versions", ErrConflict)
	}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return ImportResult{}, fmt.Errorf("create Skill repository: %w", err)
	}
	unlock, err := lockRepositoryFile(r.root)
	if err != nil {
		return ImportResult{}, err
	}
	defer unlock()
	if err := requireRealDirectory(filepath.Dir(r.root), r.root, true); err != nil {
		return ImportResult{}, err
	}
	if err := ensureNoSymlinkAncestors(backupRoot); err != nil {
		return ImportResult{}, fmt.Errorf("validate import backup location: %w", err)
	}
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return ImportResult{}, fmt.Errorf("create import backup location: %w", err)
	}
	if err := requireRealDirectory(filepath.Dir(backupRoot), backupRoot, true); err != nil {
		return ImportResult{}, err
	}
	ids := make([]string, 0, len(sources))
	seenIDs := make(map[string]bool)
	seenVersions := make(map[string]bool)
	for _, source := range sources {
		if !validComponent(source.Identifier) || !validComponent(source.Metadata.Key) {
			return ImportResult{}, fmt.Errorf("%w: invalid Skill identity or version key", ErrUnsafe)
		}
		key := source.Identifier + "\x00" + source.Metadata.Key
		if seenVersions[key] {
			return ImportResult{}, fmt.Errorf("%w: duplicate version %s/%s", ErrConflict, source.Identifier, source.Metadata.Key)
		}
		seenVersions[key] = true
		if !seenIDs[source.Identifier] {
			seenIDs[source.Identifier] = true
			ids = append(ids, source.Identifier)
		}
		checksum, err := treeChecksum(source.SourcePath)
		if err != nil {
			return ImportResult{}, fmt.Errorf("inspect Skill source %s: %w", source.Identifier, err)
		}
		if source.Metadata.Checksum != "" && source.Metadata.Checksum != checksum {
			return ImportResult{}, fmt.Errorf("%w: Skill source %s changed after preview", ErrUnsafe, source.Identifier)
		}
	}
	sort.Strings(ids)
	before, err := captureIdentitySnapshots(r.root, ids, backupRoot)
	if err != nil {
		return ImportResult{}, err
	}
	if err := syncSnapshotBackups(before); err != nil {
		return ImportResult{}, err
	}
	if expectedBefore != nil {
		if len(expectedBefore) != len(ids) {
			return ImportResult{}, fmt.Errorf("%w: repository preview is incomplete", ErrConflict)
		}
		for _, snapshot := range before {
			if expectedBefore[snapshot.Identifier] != snapshot.Checksum {
				return ImportResult{}, fmt.Errorf("%w: repository identity %s changed after preview", ErrConflict, snapshot.Identifier)
			}
		}
	}
	for _, source := range sources {
		if records, err := r.VersionRecords(source.Identifier); err == nil {
			for _, existing := range records {
				if existing.Key == source.Metadata.Key || (existing.Metadata.Checksum != "" && existing.Metadata.Checksum == source.Metadata.Checksum) {
					return ImportResult{}, fmt.Errorf("%w: version already exists for %s", ErrConflict, source.Identifier)
				}
			}
		} else if !errors.Is(err, ErrNotFound) {
			return ImportResult{}, err
		}
	}
	if hooks.BeforePublish != nil {
		evidence := make([]ImportSnapshot, len(before))
		for i, snapshot := range before {
			evidence[i] = ImportSnapshot{Identifier: snapshot.Identifier, Exists: snapshot.Exists, Checksum: snapshot.Checksum, Backup: snapshot.Backup}
		}
		if err := hooks.BeforePublish(evidence); err != nil {
			return ImportResult{}, err
		}
	}
	entries := make([]Entry, 0, len(sources))
	for _, source := range sources {
		entry, err := r.addVersion(source.Identifier, source.SourcePath, source.Metadata, source.MakeCurrent)
		if err != nil {
			if len(entries) == 0 {
				return ImportResult{}, err
			}
			after := identityChecksums(r.root, ids)
			rollbackErr := restoreIdentitySnapshots(r.root, before, after)
			if rollbackErr != nil {
				rollbackErr = errors.Join(ErrRollbackIncomplete, fmt.Errorf("import rollback recovery snapshots retained at %s: %w", backupRoot, rollbackErr))
			}
			return ImportResult{}, errors.Join(err, rollbackErr)
		}
		entries = append(entries, entry)
		if hooks.AfterPublish != nil {
			if err := hooks.AfterPublish(len(entries)-1, entry); err != nil {
				after := identityChecksums(r.root, ids)
				rollbackErr := restoreIdentitySnapshots(r.root, before, after)
				if rollbackErr != nil {
					rollbackErr = errors.Join(ErrRollbackIncomplete, fmt.Errorf("import rollback recovery snapshots retained at %s: %w", backupRoot, rollbackErr))
				}
				return ImportResult{}, errors.Join(err, rollbackErr)
			}
		}
	}
	after := identityChecksums(r.root, ids)
	for _, id := range ids {
		if after[id] == "" {
			return ImportResult{}, fmt.Errorf("%w: missing post-import identity %s", ErrUnsafe, id)
		}
	}
	return ImportResult{Entries: entries}, nil
}

func syncSnapshotBackups(snapshots []identitySnapshot) error {
	for _, snapshot := range snapshots {
		if !snapshot.Exists {
			continue
		}
		if err := syncTree(snapshot.Backup); err != nil {
			return fmt.Errorf("sync import backup for %s: %w", snapshot.Identifier, err)
		}
	}
	return nil
}

func syncTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: Skill version contains a symbolic link", ErrUnsafe)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := syncTree(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
		return syncDirectory(root)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: Skill version contains a special file", ErrUnsafe)
	}
	file, err := os.Open(root)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}

func captureIdentitySnapshots(repositoryRoot string, identifiers []string, backupRoot string) ([]identitySnapshot, error) {
	snapshots := make([]identitySnapshot, 0, len(identifiers))
	for _, identifier := range identifiers {
		if !validComponent(identifier) {
			return nil, fmt.Errorf("%w: invalid Skill identifier", ErrUnsafe)
		}
		path := filepath.Join(repositoryRoot, identifier)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			snapshots = append(snapshots, identitySnapshot{Identifier: identifier})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect repository identity %s: %w", identifier, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: repository identity %s is not a real directory", ErrUnsafe, identifier)
		}
		checksum, err := treeChecksum(path)
		if err != nil {
			return nil, fmt.Errorf("fingerprint repository identity %s: %w", identifier, err)
		}
		backup := filepath.Join(backupRoot, identifier)
		if _, err := os.Lstat(backup); err == nil {
			return nil, fmt.Errorf("%w: import backup already exists for %s", ErrConflict, identifier)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err := copyTree(path, backup); err != nil {
			return nil, fmt.Errorf("snapshot repository identity %s: %w", identifier, err)
		}
		copyChecksum, err := treeChecksum(backup)
		if err != nil || copyChecksum != checksum {
			return nil, errors.Join(fmt.Errorf("%w: repository snapshot did not verify for %s", ErrUnsafe, identifier), err)
		}
		snapshots = append(snapshots, identitySnapshot{Identifier: identifier, Exists: true, Checksum: checksum, Backup: backup})
	}
	return snapshots, nil
}

func identityChecksums(repositoryRoot string, identifiers []string) map[string]string {
	result := make(map[string]string, len(identifiers))
	for _, identifier := range identifiers {
		checksum, err := treeChecksum(filepath.Join(repositoryRoot, identifier))
		if err == nil {
			result[identifier] = checksum
		} else if os.IsNotExist(err) {
			result[identifier] = ""
		}
	}
	return result
}

func restoreIdentitySnapshots(repositoryRoot string, before []identitySnapshot, expectedAfter map[string]string) error {
	if len(before) == 0 || len(expectedAfter) != len(before) {
		return fmt.Errorf("%w: incomplete import rollback state", ErrUnsafe)
	}
	ids := make([]string, 0, len(before))
	seen := make(map[string]bool, len(before))
	for _, snapshot := range before {
		if !validComponent(snapshot.Identifier) || seen[snapshot.Identifier] {
			return fmt.Errorf("%w: invalid import rollback identity", ErrUnsafe)
		}
		seen[snapshot.Identifier] = true
		ids = append(ids, snapshot.Identifier)
		current := filepath.Join(repositoryRoot, snapshot.Identifier)
		if expectedAfter[snapshot.Identifier] == "" {
			if _, err := os.Lstat(current); !os.IsNotExist(err) {
				return fmt.Errorf("%w: repository identity %s appeared during transaction", ErrConflict, snapshot.Identifier)
			}
			if snapshot.Exists {
				return fmt.Errorf("%w: prior repository identity %s disappeared", ErrConflict, snapshot.Identifier)
			}
			continue
		}
		checksum, err := treeChecksum(current)
		if err != nil || checksum != expectedAfter[snapshot.Identifier] {
			return errors.Join(fmt.Errorf("%w: repository identity %s changed after import", ErrConflict, snapshot.Identifier), err)
		}
		if snapshot.Exists {
			if err := ensureNoSymlinkAncestors(snapshot.Backup); err != nil {
				return fmt.Errorf("validate import backup for %s: %w", snapshot.Identifier, err)
			}
			backupChecksum, err := treeChecksum(snapshot.Backup)
			if err != nil || backupChecksum != snapshot.Checksum {
				return errors.Join(fmt.Errorf("%w: import backup changed for %s", ErrConflict, snapshot.Identifier), err)
			}
		}
	}
	sort.Strings(ids)
	stage, err := os.MkdirTemp(repositoryRoot, ".skill-manager-import-rollback-")
	if err != nil {
		return fmt.Errorf("stage import rollback: %w", err)
	}
	cleanup := false
	defer func() {
		if cleanup {
			_ = removeTreeWritable(stage)
		}
	}()
	moved := make(map[string]string, len(ids))
	restored := make(map[string]string, len(ids))
	restoreMoved := func(cause error) error {
		var errs []error
		for i := len(ids) - 1; i >= 0; i-- {
			id := ids[i]
			current := filepath.Join(repositoryRoot, id)
			original := moved[id]
			if original == "" {
				continue
			}
			if restoredChecksum := restored[id]; restoredChecksum != "" {
				if checksum, checksumErr := treeChecksum(current); checksumErr != nil || checksum != restoredChecksum {
					errs = append(errs, errors.Join(fmt.Errorf("preserved concurrent identity %s during rollback", id), checksumErr))
					continue
				}
				if err := removeTreeWritable(current); err != nil {
					errs = append(errs, err)
					continue
				}
			}
			if _, err := os.Lstat(current); os.IsNotExist(err) {
				if err := os.Rename(original, current); err != nil {
					errs = append(errs, fmt.Errorf("restore identity %s after rollback failure: %w", id, err))
				}
			} else {
				errs = append(errs, fmt.Errorf("preserved unexpected path %s during rollback", current))
			}
		}
		return errors.Join(cause, errors.Join(errs...))
	}
	for _, id := range ids {
		current := filepath.Join(repositoryRoot, id)
		if expectedAfter[id] == "" {
			continue
		}
		checksum, err := treeChecksum(current)
		if err != nil || checksum != expectedAfter[id] {
			return restoreMoved(errors.Join(fmt.Errorf("%w: repository identity %s changed while staging rollback", ErrConflict, id), err))
		}
		movedPath := filepath.Join(stage, "current-"+id)
		if err := os.Rename(current, movedPath); err != nil {
			return restoreMoved(fmt.Errorf("stage imported identity %s: %w", id, err))
		}
		moved[id] = movedPath
	}
	for _, snapshot := range before {
		if !snapshot.Exists {
			continue
		}
		if err := ensureNoSymlinkAncestors(snapshot.Backup); err != nil {
			return restoreMoved(fmt.Errorf("validate import backup for %s: %w", snapshot.Identifier, err))
		}
		staged := filepath.Join(stage, "restore-"+snapshot.Identifier)
		if err := copyTree(snapshot.Backup, staged); err != nil {
			return restoreMoved(fmt.Errorf("stage prior identity %s: %w", snapshot.Identifier, err))
		}
		checksum, err := treeChecksum(staged)
		if err != nil || checksum != snapshot.Checksum {
			return restoreMoved(errors.Join(fmt.Errorf("%w: staged prior identity changed for %s", ErrUnsafe, snapshot.Identifier), err))
		}
		if err := os.Rename(staged, filepath.Join(repositoryRoot, snapshot.Identifier)); err != nil {
			return restoreMoved(fmt.Errorf("restore prior identity %s: %w", snapshot.Identifier, err))
		}
		restored[snapshot.Identifier] = snapshot.Checksum
	}
	cleanup = true
	return nil
}

func ensureNoSymlinkAncestors(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	current := string(filepath.Separator)
	if volume != "" {
		current = volume + string(filepath.Separator)
	}
	relative := strings.TrimPrefix(absolute, current)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic-link backup ancestor %s", ErrUnsafe, current)
		}
	}
	return nil
}
