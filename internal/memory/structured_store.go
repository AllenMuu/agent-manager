package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// StructuredStore is an explicitly selected local canonical store. Opening it
// never reads or imports legacy text. The root must be an existing directory.
type StructuredStore struct {
	root        string
	identity    os.FileInfo
	persistence StorePersistence
}
type operationReceipt struct {
	Intent  string   `json:"intent"`
	Results []Record `json:"results"`
}
type storeState struct {
	Operations map[string]operationReceipt `json:"operations"`
	Format     int                         `json:"format"`
	Records    map[RecordID]Record         `json:"records"`
	History    map[RecordID][]Record       `json:"history"`
}

func OpenStructuredStore(root string, options ...StoreOption) (*StructuredStore, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: existing direct directory required", ErrUnavailable)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	root = filepath.Join(parent, filepath.Base(absolute))
	if err != nil {
		return nil, err
	}
	dir, err := openStoreDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer dir.Close()
	openedInfo, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("%w: store directory changed during open", ErrUnavailable)
	}
	s := &StructuredStore{root: root, identity: openedInfo, persistence: AtomicFilePersistence{}}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidInput
		}
		if err := option(s); err != nil {
			return nil, err
		}
	}
	_, err = s.read(context.Background())
	return s, err
}
func (s *StructuredStore) directory() (*os.File, error) {
	if s == nil || s.identity == nil || s.persistence == nil {
		return nil, ErrUnavailable
	}
	dir, err := openStoreDirectory(s.root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	info, err := dir.Stat()
	if err != nil || !os.SameFile(info, s.identity) {
		dir.Close()
		return nil, fmt.Errorf("%w: store directory replaced", ErrUnavailable)
	}
	return dir, nil
}
func loadStore(dir *os.File) (storeState, error) {
	state := storeState{Format: 1, Operations: map[string]operationReceipt{}, Records: map[RecordID]Record{}, History: map[RecordID][]Record{}}
	data, err := readStoreFile(dir, "memory.json")
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !utf8.Valid(data) {
		return state, fmt.Errorf("%w: structured state must be valid UTF-8", ErrUnavailable)
	}
	if err := validateJSONUnicode(data); err != nil {
		return state, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	state = storeState{}
	if err = json.Unmarshal(data, &state); err != nil || state.Format != 1 || state.Records == nil || state.History == nil || state.Operations == nil {
		return state, fmt.Errorf("%w: invalid structured Memory format", ErrUnavailable)
	}
	if err := validateStoreState(state); err != nil {
		return storeState{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return state, nil
}
func (s *StructuredStore) read(ctx context.Context) (storeState, error) {
	if err := operationContext(ctx); err != nil {
		return storeState{}, err
	}
	dir, err := s.directory()
	if err != nil {
		return storeState{}, err
	}
	defer dir.Close()
	state, err := loadStore(dir)
	if err != nil {
		return state, err
	}
	return state, operationContext(ctx)
}
func neutralID() (RecordID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return RecordID("memory-" + hex.EncodeToString(b[:])), nil
}
func canonical(input NewRecord) (Record, error) {
	id, err := neutralID()
	layer := input.Layer
	if layer == "" {
		layer = LayerRaw
	}
	return Record{ID: id, Version: 1, Owner: input.Owner, Type: input.Type, Content: input.Content, Source: input.Source, Evidence: append([]string(nil), input.Evidence...), State: RecordActive, Layer: layer}, err
}
func (s *StructuredStore) transaction(ctx context.Context, operationID string, intent any, change func(*storeState) (Record, error)) (Record, error) {
	records, err := s.transactionBatch(ctx, operationID, intent, func(state *storeState) ([]Record, error) { r, e := change(state); return []Record{r}, e })
	if err != nil {
		return Record{}, err
	}
	return records[0], nil
}
func (s *StructuredStore) transactionBatch(ctx context.Context, operationID string, intent any, change func(*storeState) ([]Record, error)) ([]Record, error) {
	if err := operationContext(ctx); err != nil {
		return nil, err
	}
	if operationID == "" {
		id, err := neutralID()
		if err != nil {
			return nil, err
		}
		operationID = string(id)
	}
	if err := validateStructuredToken("operation ID", operationID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	dir, err := s.directory()
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	unlock, err := lockStore(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	state, err := loadStore(dir)
	if err != nil {
		return nil, err
	}
	if receipt, ok := state.Operations[operationID]; ok {
		if receipt.Intent != fingerprint {
			return nil, fmt.Errorf("%w: operation ID reused with different intent", ErrConflict)
		}
		if err := s.persistence.Confirm(ctx, dir); err != nil {
			return nil, err
		}
		return cloneRecords(receipt.Results), nil
	}
	records, err := change(&state)
	if err != nil {
		return nil, err
	}
	state.Operations[operationID] = operationReceipt{Intent: fingerprint, Results: cloneRecords(records)}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err = s.persistence.Commit(ctx, dir, data); err != nil {
		return nil, err
	}
	return cloneRecords(records), nil
}
func cloneRecords(records []Record) []Record {
	result := make([]Record, len(records))
	for i, r := range records {
		result[i] = cloneRecord(r)
	}
	return result
}
func mutationIntent(kind string, input any) any {
	return struct {
		Kind  string `json:"kind"`
		Input any    `json:"input"`
	}{kind, input}
}
func (s *StructuredStore) Remember(ctx context.Context, input NewRecord) (Record, error) {
	return s.RememberWithOperation(ctx, RememberRequest{Record: input})
}
func (s *StructuredStore) RememberWithOperation(ctx context.Context, request RememberRequest) (Record, error) {
	input := request.Record
	if err := input.validate(); err != nil {
		return Record{}, err
	}
	return s.transaction(ctx, request.OperationID, mutationIntent("remember", input), func(state *storeState) (Record, error) {
		record, err := canonical(input)
		if err != nil {
			return Record{}, err
		}
		saveRecord(state, record)
		return record, nil
	})
}
func (s *StructuredStore) Update(ctx context.Context, input UpdateRequest) (Record, error) {
	if err := (MutationRequest{Owner: input.Owner, ID: input.ID, ExpectedVersion: input.ExpectedVersion}).validate(); err != nil {
		return Record{}, err
	}
	if err := input.Record.validate(); err != nil {
		return Record{}, err
	}
	if input.Record.Owner != input.Owner {
		return Record{}, ErrOwnershipDenied
	}
	return s.transaction(ctx, input.OperationID, mutationIntent("update", input), func(state *storeState) (Record, error) {
		old, err := currentRecord(state, input.Owner, input.ID, input.ExpectedVersion)
		if err != nil {
			return Record{}, err
		}
		record, err := canonical(input.Record)
		if err != nil {
			return Record{}, err
		}
		record.ID = old.ID
		record.Version = old.Version + 1
		record.Supersedes = old.Supersedes
		record.SupersededBy = old.SupersededBy
		saveRecord(state, record)
		return record, nil
	})
}
func currentRecord(state *storeState, owner Owner, id RecordID, version uint64) (Record, error) {
	if err := validateStructuredToken("record ID", string(id)); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := owner.validate(); err != nil {
		return Record{}, err
	}
	if version == 0 {
		return Record{}, fmt.Errorf("%w: expected version required", ErrInvalidInput)
	}
	record, ok := state.Records[id]
	if !ok || record.Owner != owner {
		return Record{}, ErrNotFound
	}
	if record.Version != version || record.State != RecordActive {
		return Record{}, ErrConflict
	}
	if version == ^uint64(0) {
		return Record{}, ErrConflict
	}
	return record, nil
}
func (s *StructuredStore) Get(ctx context.Context, owner Owner, id RecordID) (Record, error) {
	if err := owner.validate(); err != nil {
		return Record{}, err
	}
	if err := validateStructuredToken("record ID", string(id)); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	state, err := s.read(ctx)
	if err != nil {
		return Record{}, err
	}
	record, ok := state.Records[id]
	if !ok || record.Owner != owner {
		return Record{}, ErrNotFound
	}
	return cloneRecord(record), nil
}
func (s *StructuredStore) Recall(ctx context.Context, q Query) ([]Record, error) {
	if err := q.Owner.validate(); err != nil {
		return nil, err
	}
	state, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, r := range state.Records {
		if r.Owner == q.Owner && r.State == RecordActive && strings.Contains(strings.ToLower(r.Content), strings.ToLower(q.Text)) {
			records = append(records, cloneRecord(r))
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}
func (s *StructuredStore) Capabilities() StructuredCapabilities {
	return StructuredCapabilities{Remember: true, Get: true, Recall: true, Update: true, Supersede: true, Forget: true, History: true, ConditionalUpdate: true, AtomicSupersede: true, ImportLegacy: true}
}
func (s *StructuredStore) Health(ctx context.Context) (HealthStatus, error) {
	_, err := s.read(ctx)
	if err != nil {
		return HealthStatus{Reason: err.Error()}, err
	}
	return HealthStatus{Available: true}, nil
}

func saveRecord(state *storeState, record Record) {
	state.Records[record.ID] = record
	state.History[record.ID] = append(state.History[record.ID], cloneRecord(record))
}
func (s *StructuredStore) Supersede(ctx context.Context, input UpdateRequest) (Record, error) {
	if err := (MutationRequest{Owner: input.Owner, ID: input.ID, ExpectedVersion: input.ExpectedVersion}).validate(); err != nil {
		return Record{}, err
	}
	if err := input.Record.validate(); err != nil {
		return Record{}, err
	}
	if input.Owner != input.Record.Owner {
		return Record{}, ErrOwnershipDenied
	}
	return s.transaction(ctx, input.OperationID, mutationIntent("supersede", input), func(state *storeState) (Record, error) {
		old, err := currentRecord(state, input.Owner, input.ID, input.ExpectedVersion)
		if err != nil {
			return Record{}, err
		}
		replacement, err := canonical(input.Record)
		if err != nil {
			return Record{}, err
		}
		old.State = RecordSuperseded
		old.Version++
		old.SupersededBy = replacement.ID
		replacement.Supersedes = old.ID
		saveRecord(state, old)
		saveRecord(state, replacement)
		return replacement, nil
	})
}
func (s *StructuredStore) Forget(ctx context.Context, input MutationRequest) (Record, error) {
	if err := (MutationRequest{Owner: input.Owner, ID: input.ID, ExpectedVersion: input.ExpectedVersion}).validate(); err != nil {
		return Record{}, err
	}
	return s.transaction(ctx, input.OperationID, mutationIntent("forget", input), func(state *storeState) (Record, error) {
		record, err := currentRecord(state, input.Owner, input.ID, input.ExpectedVersion)
		if err != nil {
			return Record{}, err
		}
		record.Version++
		record.State = RecordDeleted
		saveRecord(state, record)
		return record, nil
	})
}
func (s *StructuredStore) History(ctx context.Context, owner Owner, id RecordID) ([]Record, error) {
	if _, err := s.Get(ctx, owner, id); err != nil {
		return nil, err
	}
	state, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]Record, len(state.History[id]))
	for i, r := range state.History[id] {
		records[i] = cloneRecord(r)
	}
	return records, nil
}

// Validate the entire canonical batch before exposing or extending it. Foreign
// JSON and inconsistent history must never be normalized into a valid store.
func validateStoreState(state storeState) error {
	if len(state.Records) != len(state.History) {
		return fmt.Errorf("record/history mismatch")
	}
	for id, r := range state.Records {
		history := state.History[id]
		if len(history) == 0 || !reflect.DeepEqual(history[len(history)-1], r) {
			return fmt.Errorf("current record/history mismatch")
		}
		for i, version := range history {
			if version.ID != id || version.Version != uint64(i)+1 || version.Owner != r.Owner || version.Layer == "" {
				return fmt.Errorf("invalid record identity/version")
			}
			if err := validateStructuredToken("record ID", string(id)); err != nil {
				return err
			}
			if err := (NewRecord{Owner: version.Owner, Type: version.Type, Content: version.Content, Source: version.Source, Evidence: version.Evidence, Layer: version.Layer}).validate(); err != nil {
				return err
			}
			switch version.State {
			case RecordActive:
				if version.SupersededBy != "" {
					return fmt.Errorf("active record has replacement")
				}
			case RecordSuperseded:
				if version.SupersededBy == "" {
					return fmt.Errorf("superseded record lacks replacement")
				}
			case RecordDeleted:
			default:
				return fmt.Errorf("unknown record state")
			}
			if i < len(history)-1 && version.State != RecordActive {
				return fmt.Errorf("retired record mutated")
			}
		}
		if r.SupersededBy != "" {
			next, ok := state.Records[r.SupersededBy]
			if !ok || next.Owner != r.Owner || next.Supersedes != r.ID || r.State != RecordSuperseded {
				return fmt.Errorf("invalid replacement lineage")
			}
		}
		if r.Supersedes != "" {
			previous, ok := state.Records[r.Supersedes]
			if !ok || previous.Owner != r.Owner || previous.SupersededBy != r.ID {
				return fmt.Errorf("invalid previous lineage")
			}
		}
	}
	for operationID, receipt := range state.Operations {
		if err := validateStructuredToken("operation ID", operationID); err != nil {
			return err
		}
		digest, err := hex.DecodeString(receipt.Intent)
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("invalid operation receipt")
		}
		for _, r := range receipt.Results {
			history := state.History[r.ID]
			if r.Version == 0 || r.Version > uint64(len(history)) || !reflect.DeepEqual(history[r.Version-1], r) {
				return fmt.Errorf("operation receipt/history mismatch")
			}
		}
	}
	return nil
}
