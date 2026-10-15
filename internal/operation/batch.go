package operation

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrPendingBatch  = errors.New("operation is blocked by a pending batch")
	ErrBatchConflict = errors.New("batch ID already exists with different intent")
	batchIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
)

// BatchStatus is the durable state of a write-ahead operation.
type BatchStatus string

const (
	BatchPrepared      BatchStatus = "prepared"
	BatchPublishing    BatchStatus = "publishing"
	BatchCommitted     BatchStatus = "committed"
	BatchRolledBack    BatchStatus = "rolled-back"
	BatchNeedsRecovery BatchStatus = "needs-recovery"
)

// BatchStep is the durable intent and observed progress for one publication.
type BatchStep struct {
	ID            string `json:"id"`
	Resource      string `json:"resource"`
	Action        string `json:"action"`
	Status        string `json:"status,omitempty"`
	BeforeState   string `json:"beforeState,omitempty"`
	AfterState    string `json:"afterState,omitempty"`
	ExpectedAfter string `json:"expectedAfter,omitempty"`
	BackupPath    string `json:"backupPath,omitempty"`
	StagedPath    string `json:"stagedPath,omitempty"`
	StageRoot     string `json:"stageRoot,omitempty"`
	StagedState   string `json:"stagedState,omitempty"`
	RecoveryClass string `json:"recoveryClass,omitempty"`
}

// Batch is a write-ahead record shared by imports and lifecycle operations.
// Resources are absolute paths whose overlap is protected while the batch is
// prepared, publishing, or awaiting recovery.
type Batch struct {
	SchemaVersion    int               `json:"schemaVersion"`
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Scope            string            `json:"scope"`
	Status           BatchStatus       `json:"status"`
	Resources        []string          `json:"resources"`
	SourceRecordIDs  []string          `json:"sourceRecordIds,omitempty"`
	LinkDependencies []LinkDependency  `json:"linkDependencies,omitempty"`
	Plan             Plan              `json:"plan,omitempty"`
	Materializations []Materialization `json:"materializations,omitempty"`
	UndoEntry        *Entry            `json:"undoEntry,omitempty"`
	Steps            []BatchStep       `json:"steps,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	Error            string            `json:"error,omitempty"`
}

// BatchStore persists independent atomic JSON records under Root.
type BatchStore struct{ Root string }

// NewBatchStore returns a store rooted at path.
func NewBatchStore(path string) *BatchStore { return &BatchStore{Root: path} }

// NewBatchID returns an opaque identifier suitable for a durable batch file.
func NewBatchID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// Prepare writes the complete recovery intent before any resource is changed.
// Retrying the same ID and intent is idempotent; reusing an ID for changed
// intent is rejected.
func (s *BatchStore) Prepare(batch Batch) (Batch, error) {
	if err := s.ensureRoot(); err != nil {
		return Batch{}, err
	}
	batch, err := normalizeBatch(batch)
	if err != nil {
		return Batch{}, err
	}
	unlock, err := acquireFileLock(filepath.Join(s.Root, ".batches.lock"))
	if err != nil {
		return Batch{}, err
	}
	defer unlock()

	if existing, ok, err := s.getUnlocked(batch.ID); err != nil {
		return Batch{}, err
	} else if ok {
		if !sameBatchIntent(existing, batch) {
			return Batch{}, ErrBatchConflict
		}
		return existing, nil
	}
	all, err := s.listUnlocked()
	if err != nil {
		return Batch{}, err
	}
	for _, other := range all {
		if !batchPending(other.Status) {
			continue
		}
		if resourcesOverlap(batch.Resources, other.Resources) {
			return Batch{}, fmt.Errorf("%w: batch %s owns an overlapping resource", ErrPendingBatch, other.ID)
		}
	}
	now := time.Now().UTC()
	batch.SchemaVersion = 1
	batch.Status = BatchPrepared
	batch.CreatedAt, batch.UpdatedAt = now, now
	if err := s.write(batch); err != nil {
		return Batch{}, err
	}
	return batch, nil
}

// Get returns one durable batch by ID.
func (s *BatchStore) Get(id string) (Batch, bool, error) {
	if !batchIDPattern.MatchString(id) {
		return Batch{}, false, fmt.Errorf("invalid batch ID")
	}
	if err := s.ensureRoot(); err != nil {
		return Batch{}, false, err
	}
	return s.getUnlocked(id)
}

// List returns batches ordered by creation time and ID.
func (s *BatchStore) List() ([]Batch, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	return s.listUnlocked()
}

// Pending returns all batches that may still own or need recovery of resources.
func (s *BatchStore) Pending() ([]Batch, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	pending := all[:0]
	for _, batch := range all {
		if batchPending(batch.Status) {
			pending = append(pending, batch)
		}
	}
	return pending, nil
}

// CheckAvailable rejects a write whose path is equal to, an ancestor of, or a
// descendant of a resource owned by a pending batch.
func (s *BatchStore) CheckAvailable(resources []string) error {
	requested, err := normalizeResources(resources)
	if err != nil {
		return err
	}
	if s == nil || s.Root == "" {
		return nil
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	pending, err := s.Pending()
	if err != nil {
		return err
	}
	for _, batch := range pending {
		if resourcesOverlap(requested, batch.Resources) {
			return fmt.Errorf("%w: batch %s owns an overlapping resource", ErrPendingBatch, batch.ID)
		}
	}
	return nil
}

// Update persists progress or a terminal recovery classification. Intent
// fields are immutable after Prepare.
func (s *BatchStore) Update(batch Batch) error {
	if err := s.ensureRoot(); err != nil {
		return err
	}
	if !batchIDPattern.MatchString(batch.ID) {
		return fmt.Errorf("invalid batch ID")
	}
	unlock, err := acquireFileLock(filepath.Join(s.Root, ".batches.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	existing, ok, err := s.getUnlocked(batch.ID)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrNotExist
	}
	if !sameBatchIntent(existing, batch) {
		return ErrBatchConflict
	}
	if !validBatchTransition(existing.Status, batch.Status) {
		return fmt.Errorf("invalid batch state transition %q -> %q", existing.Status, batch.Status)
	}
	batch.SchemaVersion = 1
	batch.CreatedAt = existing.CreatedAt
	batch.UpdatedAt = time.Now().UTC()
	batch.Resources = existing.Resources
	batch.SourceRecordIDs = existing.SourceRecordIDs
	batch.LinkDependencies = existing.LinkDependencies
	batch.Plan = existing.Plan
	batch.UndoEntry = existing.UndoEntry
	batch.Kind, batch.Scope = existing.Kind, existing.Scope
	if err := s.write(batch); err != nil {
		return err
	}
	return nil
}

func (s *BatchStore) ensureRoot() error {
	if s == nil || s.Root == "" {
		return fmt.Errorf("batch store root is required")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0700); err != nil {
			return fmt.Errorf("create batch store: %w", err)
		}
	} else if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: batch store is not a real directory", ErrUnsafePath)
	}
	// Sync the chain on every entry. A prior attempt may have created the
	// directory tree and then failed partway through syncing its ancestors.
	if err := syncDirectoryChain(root); err != nil {
		return fmt.Errorf("sync batch store directory chain: %w", err)
	}
	s.Root = root
	return nil
}

func (s *BatchStore) getUnlocked(id string) (Batch, bool, error) {
	path := filepath.Join(s.Root, id+".json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Batch{}, false, nil
	}
	if err != nil {
		return Batch{}, false, err
	}
	if !info.Mode().IsRegular() {
		return Batch{}, false, fmt.Errorf("%w: batch record is not a regular file", ErrUnsafePath)
	}
	file, err := os.Open(path)
	if err != nil {
		return Batch{}, false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var batch Batch
	if err := decoder.Decode(&batch); err != nil {
		return Batch{}, false, fmt.Errorf("decode batch %s: %w", id, err)
	}
	if batch.ID != id || batch.SchemaVersion != 1 {
		return Batch{}, false, fmt.Errorf("invalid batch record %s", id)
	}
	return batch, true, nil
}

func (s *BatchStore) listUnlocked() ([]Batch, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	batches := make([]Batch, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		batch, ok, err := s.getUnlocked(id)
		if err != nil {
			return nil, err
		}
		if ok {
			batches = append(batches, batch)
		}
	}
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].CreatedAt.Equal(batches[j].CreatedAt) {
			return batches[i].ID < batches[j].ID
		}
		return batches[i].CreatedAt.Before(batches[j].CreatedAt)
	})
	return batches, nil
}

func (s *BatchStore) write(batch Batch) error {
	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(s.Root, ".batch-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, filepath.Join(s.Root, batch.ID+".json")); err != nil {
		return err
	}
	if err := syncDirectory(s.Root); err != nil {
		return err
	}
	ok = true
	return nil
}

func normalizeBatch(batch Batch) (Batch, error) {
	if !batchIDPattern.MatchString(batch.ID) || batch.Kind == "" || batch.Scope == "" {
		return Batch{}, fmt.Errorf("batch ID, kind, and scope are required")
	}
	resources, err := normalizeResources(batch.Resources)
	if err != nil || len(resources) == 0 {
		return Batch{}, fmt.Errorf("batch requires at least one valid resource")
	}
	batch.Resources = resources
	batch.SourceRecordIDs = uniqueSorted(batch.SourceRecordIDs)
	for i := range batch.LinkDependencies {
		path, err := filepath.Abs(batch.LinkDependencies[i].EntryPath)
		if err != nil {
			return Batch{}, err
		}
		batch.LinkDependencies[i].EntryPath = filepath.Clean(path)
		path, err = filepath.Abs(batch.LinkDependencies[i].RealPath)
		if err != nil {
			return Batch{}, err
		}
		batch.LinkDependencies[i].RealPath = filepath.Clean(path)
		batch.LinkDependencies[i].LinkChain = append([]LinkDependencyHop(nil), batch.LinkDependencies[i].LinkChain...)
		for j := range batch.LinkDependencies[i].LinkChain {
			path, err = filepath.Abs(batch.LinkDependencies[i].LinkChain[j].Path)
			if err != nil {
				return Batch{}, err
			}
			batch.LinkDependencies[i].LinkChain[j].Path = filepath.Clean(path)
		}
	}
	for i := range batch.Steps {
		if batch.Steps[i].ID == "" || batch.Steps[i].Action == "" || batch.Steps[i].Resource == "" {
			return Batch{}, fmt.Errorf("batch step ID, action, and resource are required")
		}
		path, err := filepath.Abs(batch.Steps[i].Resource)
		if err != nil {
			return Batch{}, err
		}
		batch.Steps[i].Resource = filepath.Clean(path)
	}
	if batch.Status != "" && batch.Status != BatchPrepared {
		return Batch{}, fmt.Errorf("new batch must start prepared")
	}
	return batch, nil
}

func normalizeResources(resources []string) ([]string, error) {
	result := make([]string, 0, len(resources))
	seen := map[string]bool{}
	for _, resource := range resources {
		if strings.TrimSpace(resource) == "" {
			return nil, fmt.Errorf("batch resource path is required")
		}
		path, err := filepath.Abs(resource)
		if err != nil {
			return nil, err
		}
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !set[value] {
			set[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func sameBatchIntent(a, b Batch) bool {
	if len(a.Steps) != len(b.Steps) {
		return false
	}
	a.Steps = append([]BatchStep(nil), a.Steps...)
	b.Steps = append([]BatchStep(nil), b.Steps...)
	a.Status, b.Status = "", ""
	a.SchemaVersion, b.SchemaVersion = 0, 0
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	a.Error, b.Error = "", ""
	for i := range a.Steps {
		a.Steps[i].Status, b.Steps[i].Status = "", ""
		a.Steps[i].BeforeState, b.Steps[i].BeforeState = "", ""
		a.Steps[i].AfterState, b.Steps[i].AfterState = "", ""
		a.Steps[i].BackupPath, b.Steps[i].BackupPath = "", ""
		a.Steps[i].StagedPath, b.Steps[i].StagedPath = "", ""
		a.Steps[i].StageRoot, b.Steps[i].StageRoot = "", ""
		a.Steps[i].StagedState, b.Steps[i].StagedState = "", ""
		a.Steps[i].RecoveryClass, b.Steps[i].RecoveryClass = "", ""
	}
	a.Materializations, b.Materializations = nil, nil
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func batchPending(status BatchStatus) bool {
	return status == BatchPrepared || status == BatchPublishing || status == BatchNeedsRecovery
}

func validBatchTransition(from, to BatchStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case BatchPrepared:
		return to == BatchPublishing || to == BatchCommitted || to == BatchRolledBack || to == BatchNeedsRecovery
	case BatchPublishing:
		return to == BatchCommitted || to == BatchRolledBack || to == BatchNeedsRecovery
	case BatchNeedsRecovery:
		return to == BatchCommitted || to == BatchRolledBack || to == BatchNeedsRecovery
	default:
		return false
	}
}

func resourcesOverlap(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if pathContains(left, right) || pathContains(right, left) {
				return true
			}
		}
	}
	return false
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
