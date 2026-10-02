package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Access is trusted caller authority, bound once at the delivery boundary.
// Requested owner labels never add permissions to this exact allowlist.
type Access struct {
	ReadOwners  []Owner
	WriteOwners []Owner
}
type RetrievalPolicy struct {
	MaxResults   int     `json:"maxResults" yaml:"maxResults"`
	ContentBytes int     `json:"contentBytes" yaml:"contentBytes"`
	ContextBytes int     `json:"contextBytes" yaml:"contextBytes"`
	MinRelevance float64 `json:"minRelevance" yaml:"minRelevance"`
}
type Gateway struct {
	provider StructuredProvider
	access   Access
	policy   RetrievalPolicy
}

func NewGateway(p StructuredProvider, access Access, policy RetrievalPolicy) (*Gateway, error) {
	var err error
	policy, err = policy.normalized()
	if err != nil {
		return nil, err
	}
	for _, owners := range [][]Owner{access.ReadOwners, access.WriteOwners} {
		for _, owner := range owners {
			if err := owner.validate(); err != nil {
				return nil, err
			}
		}
	}
	return &Gateway{provider: p, access: Access{ReadOwners: append([]Owner(nil), access.ReadOwners...), WriteOwners: append([]Owner(nil), access.WriteOwners...)}, policy: policy}, nil
}
func (g *Gateway) authorize(owner Owner, write bool) error {
	if g == nil {
		return ErrOwnershipDenied
	}
	if err := owner.validate(); err != nil {
		return err
	}
	owners := g.access.ReadOwners
	if write {
		owners = g.access.WriteOwners
	}
	for _, allowed := range owners {
		if owner == allowed {
			return nil
		}
	}
	return ErrOwnershipDenied
}

var ErrNotConfirmed = errors.New("Memory mutation not confirmed")

type Operation string

const (
	OperationAdd       Operation = "add"
	OperationUpdate    Operation = "update"
	OperationSupersede Operation = "supersede"
	OperationForget    Operation = "forget"
	OperationImport    Operation = "import"
)

// Mutation names exact owner, version, content and provenance. OperationID is
// retained for safe provider receipt retries; no retry is performed implicitly.
type Mutation struct {
	Operation       Operation           `json:"operation"`
	Record          NewRecord           `json:"record"`
	ID              RecordID            `json:"id,omitempty"`
	ExpectedVersion uint64              `json:"expectedVersion,omitempty"`
	OperationID     string              `json:"operationId,omitempty"`
	Import          LegacyImportRequest `json:"import,omitempty"`
}
type Preview struct {
	gateway  *Gateway
	mutation Mutation
	intentID string
}

// MutationPlan contains only fields the selected operation applies. Owner is
// always explicit; imports and retirement do not pretend to apply a record body.
type MutationPlan struct {
	Operation       Operation            `json:"operation"`
	Owner           Owner                `json:"owner"`
	ID              RecordID             `json:"id,omitempty"`
	ExpectedVersion uint64               `json:"expectedVersion,omitempty"`
	OperationID     string               `json:"operationId,omitempty"`
	Record          *NewRecord           `json:"record,omitempty"`
	Import          *LegacyImportRequest `json:"import,omitempty"`
}

func (p Preview) IntentID() string { return p.intentID }
func (p Preview) Plan() MutationPlan {
	m := p.mutation
	plan := MutationPlan{Operation: m.Operation, Owner: m.Record.Owner, ID: m.ID, ExpectedVersion: m.ExpectedVersion, OperationID: m.OperationID}
	switch m.Operation {
	case OperationAdd, OperationUpdate, OperationSupersede:
		record := m.Record
		record.Evidence = append([]string(nil), m.Record.Evidence...)
		plan.Record = &record
	case OperationImport:
		input := m.Import
		plan.Import = &input
	}
	return plan
}

type Confirmation struct {
	Confirmed bool
	IntentID  string
	Owner     Owner
	Source    string
}

func (g *Gateway) Preview(ctx context.Context, m Mutation) (Preview, error) {
	if err := g.authorize(m.Record.Owner, true); err != nil {
		return Preview{}, err
	}
	if err := operationContext(ctx); err != nil {
		return Preview{}, err
	}
	if m.Operation != OperationImport && m.Import != (LegacyImportRequest{}) {
		return Preview{}, ErrInvalidInput
	}
	if (m.Operation == OperationAdd || m.Operation == OperationImport) && (m.ID != "" || m.ExpectedVersion != 0) {
		return Preview{}, ErrInvalidInput
	}
	if m.Operation == OperationForget || m.Operation == OperationImport {
		if m.Record.Type != "" || m.Record.Content != "" || m.Record.Source != "" || m.Record.Layer != "" || len(m.Record.Evidence) != 0 {
			return Preview{}, ErrInvalidInput
		}
	}
	switch m.Operation {
	case OperationAdd:
		if err := m.Record.validate(); err != nil {
			return Preview{}, SafeError(err)
		}
	case OperationUpdate, OperationSupersede, OperationForget:
		if err := (MutationRequest{Owner: m.Record.Owner, ID: m.ID, ExpectedVersion: m.ExpectedVersion}).validate(); err != nil {
			return Preview{}, SafeError(err)
		}
		if m.Operation != OperationForget {
			if err := m.Record.validate(); err != nil {
				return Preview{}, SafeError(err)
			}
		}
		current, err := Get(ctx, g.provider, m.Record.Owner, m.ID)
		if err != nil {
			return Preview{}, SafeError(err)
		}
		if err := validateGatewayRecord(current, m.Record.Owner); err != nil {
			return Preview{}, err
		}
		if current.ID != m.ID {
			return Preview{}, ErrInvalidInput
		}
		// The confirmed provider operation enforces active state/version CAS.
		// Do not preempt an identical persisted receipt after an uncertain
		// commit: fresh CLI processes must be able to reconcile it safely.

	case OperationImport:
		if m.Import.Owner != m.Record.Owner {
			return Preview{}, ErrOwnershipDenied
		}
		sample := NewRecord{Owner: m.Import.Owner, Type: m.Import.Type, Content: "import", Source: m.Import.Source, Layer: m.Import.Layer}
		if err := sample.validate(); err != nil {
			return Preview{}, SafeError(err)
		}
		if m.Import.OperationID != "" {
			if m.OperationID != "" && m.OperationID != m.Import.OperationID {
				return Preview{}, ErrInvalidInput
			}
			m.OperationID = m.Import.OperationID
		}
		if strings.TrimSpace(m.Import.Source) == "" {
			return Preview{}, ErrInvalidInput
		}
		data, err := readImportPreview(m.Import.Path)
		if err != nil {
			return Preview{}, err
		}
		digest := sha256.Sum256(data)
		expected := hex.EncodeToString(digest[:])
		if m.Import.ExpectedContentSHA256 != "" && m.Import.ExpectedContentSHA256 != expected {
			return Preview{}, ErrConflict
		}
		m.Import.ExpectedContentSHA256 = expected
	default:
		return Preview{}, ErrUnsupported
	}
	if m.OperationID != "" && validateStructuredToken("operation ID", m.OperationID) != nil {
		return Preview{}, ErrInvalidInput
	}
	m.Record.Evidence = append([]string(nil), m.Record.Evidence...)
	if m.OperationID == "" {
		id, err := neutralID()
		if err != nil {
			return Preview{}, err
		}
		m.OperationID = string(id)
	}
	if m.Operation == OperationImport {
		m.Import.OperationID = m.OperationID
	}
	preview := Preview{gateway: g, mutation: m}
	b, err := json.Marshal(preview.Plan())
	if err != nil {
		return Preview{}, ErrInvalidInput
	}
	digest := sha256.Sum256(b)
	preview.intentID = hex.EncodeToString(digest[:])
	return preview, nil
}
func (g *Gateway) Commit(ctx context.Context, p Preview, c Confirmation) ([]Record, error) {
	if p.gateway != g || p.intentID == "" || !c.Confirmed || c.IntentID != p.intentID || c.Owner != p.mutation.Record.Owner {
		return nil, ErrNotConfirmed
	}
	if p.mutation.Operation == OperationImport && c.Source != p.mutation.Import.Source {
		return nil, ErrNotConfirmed
	}
	m := p.mutation
	m.Record.Evidence = append([]string(nil), m.Record.Evidence...)
	if err := g.authorize(m.Record.Owner, true); err != nil {
		return nil, err
	}
	var r Record
	var err error
	switch m.Operation {
	case OperationAdd:
		r, err = RememberWithOperation(ctx, g.provider, RememberRequest{Record: m.Record, OperationID: m.OperationID})
	case OperationUpdate:
		r, err = Update(ctx, g.provider, UpdateRequest{Owner: m.Record.Owner, ID: m.ID, ExpectedVersion: m.ExpectedVersion, Record: m.Record, OperationID: m.OperationID})
	case OperationSupersede:
		r, err = Supersede(ctx, g.provider, UpdateRequest{Owner: m.Record.Owner, ID: m.ID, ExpectedVersion: m.ExpectedVersion, Record: m.Record, OperationID: m.OperationID})
	case OperationForget:
		r, err = Forget(ctx, g.provider, MutationRequest{Owner: m.Record.Owner, ID: m.ID, ExpectedVersion: m.ExpectedVersion, OperationID: m.OperationID})
	case OperationImport:
		records, err := ImportLegacy(ctx, g.provider, m.Import, ImportConfirmation{Confirmed: true, Owner: m.Record.Owner, Source: c.Source})
		if err != nil {
			return nil, SafeError(err)
		}
		for _, record := range records {
			if err := validateGatewayRecord(record, m.Record.Owner); err != nil {
				return nil, err
			}
		}
		return cloneRecords(records), nil
	default:
		return nil, ErrUnsupported
	}

	if err != nil {
		return nil, SafeError(err)
	}
	if err := validateGatewayRecord(r, m.Record.Owner); err != nil {
		return nil, err
	}
	return []Record{cloneRecord(r)}, nil
}

// Get and History intentionally expose retired records for explicit inspection.
func (g *Gateway) Get(ctx context.Context, owner Owner, id RecordID) (Record, error) {
	if err := g.authorize(owner, false); err != nil {
		return Record{}, err
	}
	r, err := Get(ctx, g.provider, owner, id)
	if err != nil {
		return Record{}, SafeError(err)
	}
	if err := validateGatewayRecord(r, owner); err != nil {
		return Record{}, err
	}
	if r.ID != id {
		return Record{}, ErrInvalidInput
	}
	return cloneRecord(r), nil
}
func (g *Gateway) History(ctx context.Context, owner Owner, id RecordID) ([]Record, error) {
	if err := g.authorize(owner, false); err != nil {
		return nil, err
	}
	rs, err := History(ctx, g.provider, owner, id)
	if err != nil {
		return nil, SafeError(err)
	}
	for _, r := range rs {
		if err := validateGatewayRecord(r, owner); err != nil {
			return nil, err
		}
		if r.ID != id {
			return nil, ErrInvalidInput
		}
	}
	return cloneRecords(rs), nil
}

// SafeError preserves diagnostic categories without exposing backend references
// or raw transport errors. Uncertain outcomes are never retried automatically.
func SafeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		return ErrOutcomeUnknown
	}
	if errors.Is(err, ErrNotCommitted) {
		return ErrNotCommitted
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrCanceled
	}
	for _, category := range []error{ErrCanceled, ErrOwnershipDenied, ErrInvalidInput, ErrUnsupported, ErrNotFound, ErrConflict, ErrNotConfirmed, ErrOutcomeUnknown, ErrNotCommitted, ErrUnavailable} {
		if errors.Is(err, category) {
			return category
		}
	}
	return ErrUnavailable
}

func readImportPreview(path string) ([]byte, error) {
	if !utf8.ValidString(path) {
		return nil, ErrInvalidInput
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidInput
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, ErrInvalidInput
	}
	dir, err := openStoreDirectory(parent)
	if err != nil {
		return nil, ErrInvalidInput
	}
	defer dir.Close()
	data, err := readStoreFile(dir, filepath.Base(path))
	if err != nil {
		return nil, SafeError(err)
	}
	if !utf8.Valid(data) {
		return nil, ErrInvalidInput
	}
	return data, nil
}

// DiagnosticCategory is output-safe and independent of backend error text.
func DiagnosticCategory(err error) string {
	if err == nil {
		return ""
	}
	safe := SafeError(err)
	switch safe {
	case ErrCanceled:
		return "canceled"
	case ErrOwnershipDenied:
		return "ownership-denied"
	case ErrInvalidInput:
		return "invalid-input"
	case ErrUnsupported:
		return "unsupported"
	case ErrNotFound:
		return "not-found"
	case ErrConflict:
		return "conflict"
	case ErrNotConfirmed:
		return "not-confirmed"
	case ErrOutcomeUnknown:
		return "outcome-unknown"
	case ErrNotCommitted:
		return "not-committed"
	default:
		return "unavailable"
	}
}

func validateGatewayRecord(r Record, owner Owner) error {
	if r.Owner != owner {
		return ErrOwnershipDenied
	}
	if validateStructuredToken("record ID", string(r.ID)) != nil || r.Version == 0 || r.Layer == "" {
		return ErrInvalidInput
	}
	if err := (NewRecord{Owner: r.Owner, Type: r.Type, Content: r.Content, Source: r.Source, Evidence: r.Evidence, Layer: r.Layer}).validate(); err != nil {
		return ErrInvalidInput
	}
	switch r.State {
	case RecordActive, RecordSuperseded, RecordDeleted:
	default:
		return ErrInvalidInput
	}
	for _, id := range []RecordID{r.Supersedes, r.SupersededBy} {
		if id != "" && validateStructuredToken("record ID", string(id)) != nil {
			return ErrInvalidInput
		}
	}
	return nil
}
