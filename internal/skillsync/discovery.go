package skillsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

type DiscoveryStatus string

const (
	DiscoveryStaged    DiscoveryStatus = "staged"
	DiscoveryCommitted DiscoveryStatus = "committed"
)

// DiscoveryRecord is a durable observation of a source at one confirmed
// import. It is separate from immutable Skill version provenance.
type DiscoveryRecord struct {
	SchemaVersion   int                       `json:"schemaVersion"`
	ID              string                    `json:"id"`
	BatchID         string                    `json:"batchId"`
	Status          DiscoveryStatus           `json:"status"`
	Scope           Scope                     `json:"scope"`
	Location        string                    `json:"location"`
	EntryPath       string                    `json:"entryPath"`
	EntryType       string                    `json:"entryType,omitempty"`
	OriginalLink    string                    `json:"originalLink,omitempty"`
	LinkChain       []LinkHop                 `json:"linkChain,omitempty"`
	RealPath        string                    `json:"realPath"`
	Checksum        string                    `json:"checksum"`
	Identifier      string                    `json:"identifier"`
	VersionKey      string                    `json:"versionKey"`
	VersionMetadata skillrepo.VersionMetadata `json:"versionMetadata,omitempty"`
	MakeCurrent     bool                      `json:"makeCurrent,omitempty"`
	ReusedVersion   bool                      `json:"reusedVersion"`
	ObservedAt      time.Time                 `json:"observedAt"`
	ConfirmedAt     time.Time                 `json:"confirmedAt,omitempty"`
	Identity        string                    `json:"identity,omitempty"`
}

// DiscoveryStore persists source observations independently of repository
// version metadata.
type DiscoveryStore struct{ Root string }

func NewDiscoveryStore(root string) *DiscoveryStore { return &DiscoveryStore{Root: root} }

// Prepare durably stages one source observation. Retrying the same ID and
// evidence is idempotent; changed evidence under the same ID is rejected.
func (s *DiscoveryStore) Prepare(record DiscoveryRecord) (DiscoveryRecord, error) {
	if err := s.ensureRoot(); err != nil {
		return DiscoveryRecord{}, err
	}
	record, err := normalizeDiscoveryRecord(record)
	if err != nil {
		return DiscoveryRecord{}, err
	}
	unlock, err := operationLock(filepath.Join(s.Root, ".discoveries.lock"))
	if err != nil {
		return DiscoveryRecord{}, err
	}
	defer unlock()
	if existing, ok, err := s.getUnlocked(record.ID); err != nil {
		return DiscoveryRecord{}, err
	} else if ok {
		if !sameDiscoveryEvidence(existing, record) {
			return DiscoveryRecord{}, fmt.Errorf("discovery record ID conflicts with changed evidence")
		}
		return existing, nil
	}
	record.SchemaVersion = 1
	record.Status = DiscoveryStaged
	if record.ObservedAt.IsZero() {
		record.ObservedAt = time.Now().UTC()
	}
	if err := s.write(record); err != nil {
		return DiscoveryRecord{}, err
	}
	return record, nil
}

// Commit marks the selected staged records committed. It is safe to retry
// after a partial write; already committed records remain committed.
func (s *DiscoveryStore) Commit(ids []string) error {
	if err := s.ensureRoot(); err != nil {
		return err
	}
	unlock, err := operationLock(filepath.Join(s.Root, ".discoveries.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	for _, id := range uniqueDiscoveryIDs(ids) {
		if !discoveryIDValid(id) {
			return fmt.Errorf("invalid discovery record ID")
		}
		record, ok, err := s.getUnlocked(id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("discovery record %s: %w", id, os.ErrNotExist)
		}
		if record.Status == DiscoveryCommitted {
			continue
		}
		if record.Status != DiscoveryStaged {
			return fmt.Errorf("discovery record %s has invalid state %q", id, record.Status)
		}
		record.Status = DiscoveryCommitted
		record.ConfirmedAt = time.Now().UTC()
		if err := s.write(record); err != nil {
			return err
		}
	}
	return nil
}

// Abort removes only staged records owned by batchID. Committed discovery
// evidence is never deleted by rollback cleanup.
func (s *DiscoveryStore) Abort(batchID string) error {
	return s.abortRecords(batchID, nil)
}

// AbortIDs removes only the selected staged records. Committed discovery
// evidence is retained, including when a partially published import is
// recovered entry by entry.
func (s *DiscoveryStore) AbortIDs(ids []string) error {
	return s.abortRecords("", ids)
}

func (s *DiscoveryStore) abortRecords(batchID string, ids []string) error {
	if err := s.ensureRoot(); err != nil {
		return err
	}
	unlock, err := operationLock(filepath.Join(s.Root, ".discoveries.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	selected := map[string]bool{}
	for _, id := range uniqueDiscoveryIDs(ids) {
		if !discoveryIDValid(id) {
			return fmt.Errorf("invalid discovery record ID")
		}
		selected[id] = true
	}
	var candidates []string
	if batchID != "" {
		entries, err := os.ReadDir(s.Root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				candidates = append(candidates, strings.TrimSuffix(entry.Name(), ".json"))
			}
		}
	} else {
		for id := range selected {
			candidates = append(candidates, id)
		}
	}
	changed := false
	for _, id := range candidates {
		record, ok, err := s.getUnlocked(id)
		if err != nil {
			return err
		}
		if ok && (batchID == "" || record.BatchID == batchID) && record.Status == DiscoveryStaged {
			if err := os.Remove(filepath.Join(s.Root, id+".json")); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed {
		dir, err := os.Open(s.Root)
		if err != nil {
			return err
		}
		err = dir.Sync()
		_ = dir.Close()
		return err
	}
	return nil
}

func (s *DiscoveryStore) Get(id string) (DiscoveryRecord, bool, error) {
	if !discoveryIDValid(id) {
		return DiscoveryRecord{}, false, fmt.Errorf("invalid discovery record ID")
	}
	if err := s.ensureRoot(); err != nil {
		return DiscoveryRecord{}, false, err
	}
	return s.getUnlocked(id)
}

func (s *DiscoveryStore) ForBatch(batchID string) ([]DiscoveryRecord, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	result := []DiscoveryRecord{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, ok, err := s.getUnlocked(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if ok && record.BatchID == batchID {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *DiscoveryStore) ensureRoot() error {
	if s == nil || s.Root == "" {
		return fmt.Errorf("discovery store root is required")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: discovery store is not a real directory", operation.ErrUnsafePath)
	}
	s.Root = root
	return nil
}

func (s *DiscoveryStore) getUnlocked(id string) (DiscoveryRecord, bool, error) {
	path := filepath.Join(s.Root, id+".json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return DiscoveryRecord{}, false, nil
	}
	if err != nil {
		return DiscoveryRecord{}, false, err
	}
	if !info.Mode().IsRegular() {
		return DiscoveryRecord{}, false, fmt.Errorf("%w: discovery record is not a regular file", operation.ErrUnsafePath)
	}
	file, err := os.Open(path)
	if err != nil {
		return DiscoveryRecord{}, false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	var record DiscoveryRecord
	if err := decoder.Decode(&record); err != nil {
		return DiscoveryRecord{}, false, fmt.Errorf("decode discovery record %s: %w", id, err)
	}
	if record.ID != id || record.SchemaVersion != 1 {
		return DiscoveryRecord{}, false, fmt.Errorf("invalid discovery record %s", id)
	}
	return record, true, nil
}

func (s *DiscoveryStore) write(record DiscoveryRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(s.Root, ".discovery-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(name)
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
	if err := os.Rename(name, filepath.Join(s.Root, record.ID+".json")); err != nil {
		return err
	}
	dir, err := os.Open(s.Root)
	if err != nil {
		return err
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return err
	}
	ok = true
	return nil
}

func normalizeDiscoveryRecord(record DiscoveryRecord) (DiscoveryRecord, error) {
	if !discoveryIDValid(record.ID) || record.BatchID == "" || record.Scope == "" || record.EntryPath == "" || record.RealPath == "" || record.Checksum == "" || record.Identifier == "" || record.VersionKey == "" {
		return DiscoveryRecord{}, fmt.Errorf("discovery record requires IDs, source identity, checksum, and version")
	}
	entry, err := filepath.Abs(record.EntryPath)
	if err != nil {
		return DiscoveryRecord{}, err
	}
	real, err := filepath.Abs(record.RealPath)
	if err != nil {
		return DiscoveryRecord{}, err
	}
	record.EntryPath, record.RealPath = filepath.Clean(entry), filepath.Clean(real)
	for i := range record.LinkChain {
		path, err := filepath.Abs(record.LinkChain[i].Path)
		if err != nil {
			return DiscoveryRecord{}, err
		}
		record.LinkChain[i].Path = filepath.Clean(path)
	}
	return record, nil
}

func sameDiscoveryEvidence(a, b DiscoveryRecord) bool {
	a.SchemaVersion, b.SchemaVersion = 0, 0
	a.Status, b.Status = "", ""
	a.ConfirmedAt, b.ConfirmedAt = time.Time{}, time.Time{}
	a.ObservedAt, b.ObservedAt = time.Time{}, time.Time{}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func discoveryIDValid(id string) bool {
	if !strings.HasPrefix(id, "src-") || len(id) != 68 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "src-"))
	return err == nil
}

func discoveryRecordID(batchID, sourceID string) string {
	digest := sha256.Sum256([]byte(batchID + "\x00" + sourceID))
	return "src-" + hex.EncodeToString(digest[:])
}

func uniqueDiscoveryIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

func operationLock(path string) (func() error, error) {
	return operation.New(path).LockOperation()
}
