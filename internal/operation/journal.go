package operation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/AllenMuu/skill-manager/internal/resource"
)

var (
	ErrNotConfirmed     = fmt.Errorf("operation was not confirmed")
	ErrUnexpectedState  = fmt.Errorf("operation paths changed since confirmation")
	ErrJournalCommitted = fmt.Errorf("operation journal was published but post-commit sync failed")
	ErrUnsafePath       = fmt.Errorf("refusing unsafe operation path")
)

// Snapshot records a path's state in an operation backup directory.
type Snapshot struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Backup string `json:"backup,omitempty"`
}

// Entry is a confirmed reversible operation.
type Entry struct {
	Version      string     `json:"version,omitempty"`
	ResourceKind string     `json:"resourceKind,omitempty"`
	Operation    string     `json:"operation"`
	At           time.Time  `json:"at"`
	Before       []Snapshot `json:"before"`
	After        []Snapshot `json:"after"`
}

// RecordMetadata describes the versioned resource envelope for a journal
// record. It is optional so existing Skill callers retain their source API.
type RecordMetadata struct {
	Version      string
	ResourceKind string
}

// Journal persists reversible operation entries at Path.
type Journal struct {
	Path string
	// ValidateEntry rejects entries that violate the caller's journal trust
	// boundary. It runs whenever entries are read, before any operation uses
	// their snapshots.
	ValidateEntry func(Entry) error
	// ValidateUndoEntry applies an additional caller-specific policy before an
	// entry can be restored. It runs before confirmation and again immediately
	// before snapshot validation and restore.
	ValidateUndoEntry func(Entry) error
	// BeforeRestorePublish is an optional fault-injection seam for restore tests.
	BeforeRestorePublish func(path string) error
	// BeforeJournalDirectorySync is an optional fault-injection seam that runs
	// after the journal file is published, modeling a post-commit sync error.
	BeforeJournalDirectorySync func() error
}

// New creates a journal that persists entries at path.
func New(path string) *Journal { return &Journal{Path: path} }

// Capture snapshots each explicit path without following soft links.
func (j *Journal) Capture(paths []string) ([]Snapshot, error) {
	result := make([]Snapshot, 0, len(paths))
	for i, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve snapshot path: %w", err)
		}
		s := Snapshot{Path: abs}
		if _, err := os.Lstat(abs); err != nil {
			if os.IsNotExist(err) {
				result = append(result, s)
				continue
			}
			return nil, fmt.Errorf("inspect %s: %w", abs, err)
		}
		s.Exists = true
		backup := filepath.Join(filepath.Dir(j.Path), ".skill-manager-journal", fmt.Sprintf("%d-%d", time.Now().UnixNano(), i))
		if err := copyPath(abs, backup); err != nil {
			return nil, err
		}
		s.Backup = backup
		result = append(result, s)
	}
	return result, nil
}

// Record appends a confirmed operation with both pre- and post-operation state.
func (j *Journal) Record(operation string, before, after []Snapshot, metadata ...RecordMetadata) error {
	plan := Plan{Operation: operation}
	if len(metadata) > 0 {
		plan.Version = metadata[0].Version
		plan.ResourceKind = metadata[0].ResourceKind
	}
	return j.RecordPlan(plan, before, after)
}

// RecordPlan appends a confirmed operation using the plan's versioned
// resource metadata. Empty metadata retains the legacy Skill defaults.
func (j *Journal) RecordPlan(plan Plan, before, after []Snapshot) error {
	version, kind := normalizeMetadata(plan.Version, plan.ResourceKind)
	if err := validateMetadata(version, kind); err != nil {
		return err
	}
	entries, err := j.entries()
	if err != nil {
		return err
	}
	entries = append(entries, Entry{Version: version, ResourceKind: kind, Operation: plan.Operation, At: time.Now().UTC(), Before: before, After: after})
	return j.write(entries)
}

// Latest returns the latest journal entry after normalizing legacy fields in
// memory. It never rewrites an existing journal merely by reading it.
func (j *Journal) Latest() (Entry, bool, error) {
	entries, err := j.entries()
	if err != nil {
		return Entry{}, false, err
	}
	if len(entries) == 0 {
		return Entry{}, false, nil
	}
	return entries[len(entries)-1], true, nil
}

// RecordedLinkTarget reports whether a confirmed operation recorded path as a
// soft link, returning its recorded destination without following it.
func (j *Journal) RecordedLinkTarget(path string) (string, bool, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	entries, err := j.entries()
	if err != nil {
		return "", false, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		for _, snapshot := range entries[i].After {
			if snapshot.Path != path {
				continue
			}
			// A path's newest post-operation state owns the answer. Never fall
			// through to an older managed-link snapshot after a fork/remove.
			if !snapshot.Exists {
				return "", false, nil
			}
			info, err := os.Lstat(snapshot.Backup)
			if err != nil {
				return "", false, fmt.Errorf("inspect journal backup: %w", err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				return "", false, nil
			}
			target, err := os.Readlink(snapshot.Backup)
			if err != nil {
				return "", false, err
			}
			return target, true, nil
		}
	}
	return "", false, nil
}

// PreviewUndo returns the restoration plan for the latest confirmed operation
// without changing paths or journal state.
func (j *Journal) PreviewUndo() (Plan, error) {
	plan, _, err := j.previewUndo()
	return plan, err
}

// UndoAvailable reports whether the latest journal entry still owns every
// recorded post-operation path. It never changes the journal or project tree.
func (j *Journal) UndoAvailable() (bool, error) {
	entries, err := j.entries()
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	for _, snapshot := range entries[len(entries)-1].After {
		matches, err := matchesSnapshot(snapshot)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

func (j *Journal) previewUndo() (Plan, []Entry, error) {
	entries, err := j.entries()
	if err != nil {
		return Plan{}, nil, err
	}
	if len(entries) == 0 {
		return Plan{}, nil, fmt.Errorf("operation journal is empty")
	}
	entry := entries[len(entries)-1]
	if j.ValidateUndoEntry != nil {
		if err := j.ValidateUndoEntry(entry); err != nil {
			return Plan{}, nil, err
		}
	}
	plan := Plan{Version: entry.Version, ResourceKind: entry.ResourceKind, Operation: "undo " + entry.Operation}
	for _, snapshot := range entry.Before {
		plan.Changes = append(plan.Changes, Change{Path: snapshot.Path, Action: "restore pre-operation state"})
	}
	return plan, entries, nil
}

// UndoLatest restores the pre-operation state of the latest journal entry.
func (j *Journal) UndoLatest(confirm func(Plan) bool) error {
	return j.UndoLatestWithFingerprints(confirm, nil)
}

// UndoLatestWithFingerprints also verifies reviewed pre-operation backups before restore.
func (j *Journal) UndoLatestWithFingerprints(confirm func(Plan) bool, expected map[string]string) error {
	plan, entries, err := j.previewUndo()
	if err != nil {
		return err
	}
	entry := entries[len(entries)-1]
	if confirm == nil || !confirm(plan) {
		return ErrNotConfirmed
	}
	if j.ValidateUndoEntry != nil {
		if err := j.ValidateUndoEntry(entry); err != nil {
			return err
		}
	}
	for _, snapshot := range entry.After {
		matches, err := matchesSnapshot(snapshot)
		if err != nil {
			return err
		}
		if !matches {
			return ErrUnexpectedState
		}
	}
	for _, snapshot := range entry.Before {
		if !snapshot.Exists || expected == nil {
			continue
		}
		digest, ok := expected[snapshot.Backup]
		if !ok {
			return ErrUnexpectedState
		}
		actual, err := FingerprintPath(snapshot.Backup)
		if err != nil || actual != digest {
			return errors.Join(ErrUnexpectedState, err)
		}
	}
	if err := j.Restore(entry.Before); err != nil {
		return err
	}
	if err := j.write(entries[:len(entries)-1]); err != nil {
		if errors.Is(err, ErrJournalCommitted) {
			return err
		}
		return errors.Join(err, j.Restore(entry.After))
	}
	return nil
}

func normalizeMetadata(version, kind string) (string, string) {
	if version == "" {
		version = "v1"
	}
	if kind == "" {
		kind = "skill"
	}
	return version, kind
}

func validateMetadata(version, kind string) error {
	if version != "v1" {
		return fmt.Errorf("unsupported operation journal version %q", version)
	}
	switch resource.Kind(kind) {
	case resource.Skill, resource.SubAgent, resource.Memory:
		return nil
	default:
		return fmt.Errorf("unsupported operation journal resource kind %q", kind)
	}
}

// Restore replaces the supplied explicit paths with their captured state.
// It is used to roll back an incomplete staged operation.
func (j *Journal) Restore(snapshots []Snapshot) error {
	// Validate each snapshot independently before touching any current path. An
	// unsafe parent must remain fail-closed, but must not prevent unrelated
	// snapshots from being restored (for example, a transaction whose second
	// project parent was swapped after its first publication).
	parentIdentities := make([][]restoreParentIdentity, len(snapshots))
	eligible := make([]bool, len(snapshots))
	var preflightErrs []error
	for i, snapshot := range snapshots {
		identities, err := captureRestoreParentIdentities(snapshot.Path)
		if err != nil {
			preflightErrs = append(preflightErrs, fmt.Errorf("skip restore %s: %w", snapshot.Path, err))
			continue
		}
		parentIdentities[i] = identities
		eligible[i] = true
	}
	for i, snapshot := range snapshots {
		if !eligible[i] {
			continue
		}
		if !snapshot.Exists {
			continue
		}
		if _, err := os.Lstat(snapshot.Backup); err != nil {
			preflightErrs = append(preflightErrs, fmt.Errorf("skip restore %s: preflight backup %s: %w", snapshot.Path, snapshot.Backup, err))
			eligible[i] = false
		}
	}
	// Build every replacement before removing an existing target.
	type stage struct{ root, candidate, previous string }
	staged := make([]stage, len(snapshots))
	for i, snapshot := range snapshots {
		if !eligible[i] {
			continue
		}
		// Keep recovery staging beside the journal, not beneath the target
		// parent. If a target parent is replaced after staging, cleanup must not
		// follow that replacement into an external directory.
		root, err := os.MkdirTemp(filepath.Dir(j.Path), ".skill-manager-restore-")
		if err != nil {
			return fmt.Errorf("stage restore %s: %w", snapshot.Path, err)
		}
		staged[i] = stage{root: root, candidate: filepath.Join(root, "candidate"), previous: filepath.Join(root, "previous")}
		if !snapshot.Exists {
			continue
		}
		if err := copyPath(snapshot.Backup, staged[i].candidate); err != nil {
			return fmt.Errorf("stage restore %s: %w", snapshot.Path, err)
		}
	}
	moved := make([]bool, len(snapshots))
	published := make([]bool, len(snapshots))
	removed := make([]bool, len(snapshots))
	retained := make([]bool, len(snapshots))
	defer func() {
		for i := range staged {
			if staged[i].root == "" || retained[i] {
				continue
			}
			_ = os.RemoveAll(staged[i].root)
		}
	}()
	rollback := func() error {
		var errs []error
		for i := len(snapshots) - 1; i >= 0; i-- {
			if !moved[i] && !published[i] && !removed[i] {
				continue
			}
			if err := restoreParentsMatch(parentIdentities[i]); err != nil {
				retained[i] = true
				errs = append(errs, fmt.Errorf("preserved restore staging at %s: %w", staged[i].root, err))
				continue
			}
			if published[i] {
				owned, matchErr := pathsEqual(snapshots[i].Path, snapshots[i].Backup)
				if matchErr != nil {
					if !os.IsNotExist(matchErr) {
						errs = append(errs, fmt.Errorf("preserve unexpected restore target %s: %w", snapshots[i].Path, matchErr))
					}
				} else if owned {
					if err := os.Remove(snapshots[i].Path); err != nil && !os.IsNotExist(err) {
						errs = append(errs, err)
					}
				} else {
					errs = append(errs, fmt.Errorf("preserved unexpected restore target %s", snapshots[i].Path))
				}
			}
			if removed[i] {
				if _, err := os.Lstat(snapshots[i].Path); err != nil && !os.IsNotExist(err) {
					errs = append(errs, err)
				} else if err == nil {
					errs = append(errs, fmt.Errorf("preserved unexpected late restore target %s", snapshots[i].Path))
				}
			}
			if _, err := os.Lstat(snapshots[i].Path); err == nil {
				retained[i] = true
				errs = append(errs, fmt.Errorf("preserved restore staging at %s: unexpected late restore target %s", staged[i].root, snapshots[i].Path))
			} else if !os.IsNotExist(err) {
				errs = append(errs, err)
			} else if _, err := os.Lstat(staged[i].previous); err == nil {
				if err := os.Rename(staged[i].previous, snapshots[i].Path); err != nil {
					errs = append(errs, err)
				}
			}
		}
		return errors.Join(errs...)
	}
	for i, snapshot := range snapshots {
		if !eligible[i] {
			continue
		}
		if err := restoreParentsMatch(parentIdentities[i]); err != nil {
			retained[i] = true
			return errors.Join(err, rollback())
		}
		if _, err := os.Lstat(snapshot.Path); err == nil {
			if err := os.Rename(snapshot.Path, staged[i].previous); err != nil {
				return errors.Join(fmt.Errorf("stage current %s: %w", snapshot.Path, err), rollback())
			}
			moved[i] = true
		} else if !os.IsNotExist(err) {
			return errors.Join(err, rollback())
		}
		if j.BeforeRestorePublish != nil {
			if err := j.BeforeRestorePublish(snapshot.Path); err != nil {
				return errors.Join(err, rollback())
			}
		}
		if err := restoreParentsMatch(parentIdentities[i]); err != nil {
			retained[i] = true
			return errors.Join(err, rollback())
		}
		if snapshot.Exists {
			if err := os.Rename(staged[i].candidate, snapshot.Path); err != nil {
				return errors.Join(fmt.Errorf("publish restore %s: %w", snapshot.Path, err), rollback())
			}
			published[i] = true
		} else {
			removed[i] = true
		}
	}
	return errors.Join(preflightErrs...)
}

// restoreParentIdentity tracks every ancestor so Restore can refuse a replaced
// parent rather than risking a path-based rename into an external tree.
type restoreParentIdentity struct {
	path string
	info os.FileInfo
}

func captureRestoreParentIdentities(path string) ([]restoreParentIdentity, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve restore path: %v", ErrUnsafePath, err)
	}
	parent := filepath.Dir(abs)
	if info, inspectErr := os.Lstat(parent); inspectErr != nil {
		return nil, fmt.Errorf("%w: inspect restore parent %s: %v", ErrUnsafePath, parent, inspectErr)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: restore parent %s is a symlink", ErrUnsafePath, parent)
	}
	identities := make([]restoreParentIdentity, 0, 4)
	for {
		rawInfo, inspectErr := os.Lstat(parent)
		if inspectErr != nil {
			return nil, fmt.Errorf("%w: inspect restore parent %s: %v", ErrUnsafePath, parent, inspectErr)
		}
		if rawInfo.Mode()&os.ModeSymlink != 0 && !trustedSystemParentSymlink(parent) {
			return nil, fmt.Errorf("%w: restore ancestor %s is a symlink", ErrUnsafePath, parent)
		}
		info, inspectErr := os.Stat(parent)
		if inspectErr != nil {
			return nil, fmt.Errorf("%w: inspect restore parent %s: %v", ErrUnsafePath, parent, inspectErr)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: restore parent %s is not a directory", ErrUnsafePath, parent)
		}
		identities = append(identities, restoreParentIdentity{path: parent, info: info})
		next := filepath.Dir(parent)
		if next == parent {
			return identities, nil
		}
		parent = next
	}
}

func trustedSystemParentSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return (path == "/var" && resolved == "/private/var") || (path == "/tmp" && resolved == "/private/tmp")
}

func restoreParentsMatch(identities []restoreParentIdentity) error {
	for _, expected := range identities {
		current, err := os.Stat(expected.path)
		if err != nil {
			return fmt.Errorf("%w: restore parent %s changed: %v", ErrUnsafePath, expected.path, err)
		}
		if !os.SameFile(expected.info, current) {
			return fmt.Errorf("%w: restore parent %s changed", ErrUnsafePath, expected.path)
		}
	}
	return nil
}

func (j *Journal) entries() ([]Entry, error) {
	b, err := os.ReadFile(j.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read operation journal: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("parse operation journal: %w", err)
	}
	for i := range entries {
		if entries[i].Version == "" {
			entries[i].Version = "v1"
		}
		if entries[i].ResourceKind == "" {
			entries[i].ResourceKind = "skill"
		}
		if err := validateMetadata(entries[i].Version, entries[i].ResourceKind); err != nil {
			return nil, err
		}
		if j.ValidateEntry != nil {
			if err := j.ValidateEntry(entries[i]); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

func (j *Journal) write(entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o755); err != nil {
		return fmt.Errorf("create operation journal directory: %w", err)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.Path), ".skill-manager-journal-")
	if err != nil {
		return fmt.Errorf("create journal temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write journal temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync journal temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close journal temp file: %w", err)
	}
	if err := os.Rename(tmpName, j.Path); err != nil {
		return fmt.Errorf("publish operation journal: %w", err)
	}
	if j.BeforeJournalDirectorySync != nil {
		if err := j.BeforeJournalDirectorySync(); err != nil {
			return errors.Join(ErrJournalCommitted, err)
		}
	}
	dir, err := os.Open(filepath.Dir(j.Path))
	if err != nil {
		return errors.Join(ErrJournalCommitted, fmt.Errorf("open operation journal directory after publish: %w", err))
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.Join(ErrJournalCommitted, fmt.Errorf("sync operation journal directory: %w", err))
	}
	return nil
}

func copyPath(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported snapshot file type at %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func matchesSnapshot(snapshot Snapshot) (bool, error) {
	_, err := os.Lstat(snapshot.Path)
	if !snapshot.Exists {
		if err == nil {
			return false, nil
		}
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("inspect expected absent path %s: %w", snapshot.Path, err)
	}
	if err != nil {
		return false, fmt.Errorf("inspect expected path %s: %w", snapshot.Path, err)
	}
	return pathsEqual(snapshot.Path, snapshot.Backup)
}

func pathsEqual(left, right string) (bool, error) {
	li, err := os.Lstat(left)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", left, err)
	}
	ri, err := os.Lstat(right)
	if err != nil || li.Mode() != ri.Mode() {
		if err != nil {
			return false, fmt.Errorf("inspect backup %s: %w", right, err)
		}
		return false, nil
	}
	if li.Mode()&os.ModeSymlink != 0 {
		l, le := os.Readlink(left)
		r, re := os.Readlink(right)
		if le != nil {
			return false, fmt.Errorf("read link %s: %w", left, le)
		}
		if re != nil {
			return false, fmt.Errorf("read backup link %s: %w", right, re)
		}
		return l == r, nil
	}
	if li.IsDir() {
		le, leErr := os.ReadDir(left)
		re, reErr := os.ReadDir(right)
		if leErr != nil || reErr != nil || len(le) != len(re) {
			if leErr != nil {
				return false, fmt.Errorf("read directory %s: %w", left, leErr)
			}
			if reErr != nil {
				return false, fmt.Errorf("read backup directory %s: %w", right, reErr)
			}
			return false, nil
		}
		for i := range le {
			if le[i].Name() != re[i].Name() {
				return false, nil
			}
			equal, err := pathsEqual(filepath.Join(left, le[i].Name()), filepath.Join(right, re[i].Name()))
			if err != nil {
				return false, err
			}
			if !equal {
				return false, nil
			}
		}
		return true, nil
	}
	if !li.Mode().IsRegular() {
		return false, nil
	}
	l, le := os.ReadFile(left)
	r, re := os.ReadFile(right)
	if le != nil {
		return false, fmt.Errorf("read %s: %w", left, le)
	}
	if re != nil {
		return false, fmt.Errorf("read backup %s: %w", right, re)
	}
	return bytes.Equal(l, r), nil
}
