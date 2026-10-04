package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
)

const WeakWriteSemantics = "explicit weak write: no CAS, atomic supersession, operation receipt or retry guarantee; concurrent writers can overwrite; reconcile uncertain outcomes by read only"

// WeakPreview is distinct from a strong mutation preview. The exact plan and
// authority are immutable. A nonce binds confirmation, not provider idempotency.
type WeakPreview struct {
	consumed *atomic.Bool
	gateway  *Gateway
	mutation Mutation
	observed *Record
	intentID string
	nonce    string
}
type WeakPlan struct {
	Operation Operation `json:"operation"`
	Record    NewRecord `json:"record"`
	Observed  *Record   `json:"observed,omitempty"`
	Semantics string    `json:"semantics"`
	Nonce     string    `json:"nonce"`
}

func (p WeakPreview) IntentID() string { return p.intentID }
func (p WeakPreview) Plan() WeakPlan {
	r := p.mutation.Record
	r.Evidence = append([]string(nil), r.Evidence...)
	plan := WeakPlan{Operation: p.mutation.Operation, Record: r, Semantics: WeakWriteSemantics, Nonce: p.nonce}
	if p.observed != nil {
		r := cloneRecord(*p.observed)
		plan.Observed = &r
	}
	return plan
}
func (g *Gateway) PreviewWeak(ctx context.Context, m Mutation) (WeakPreview, error) {
	if err := g.authorize(m.Record.Owner, true); err != nil {
		return WeakPreview{}, err
	}
	if !g.access.AllowWeakWrites {
		return WeakPreview{}, ErrUnsupported
	}
	if err := operationContext(ctx); err != nil {
		return WeakPreview{}, err
	}
	if (m.Operation != OperationAdd && m.Operation != OperationReplaceObserved) || m.OperationID != "" || m.Import != (LegacyImportRequest{}) {
		return WeakPreview{}, ErrUnsupported
	}
	if err := ValidateNewRecord(m.Record); err != nil {
		return WeakPreview{}, ErrInvalidInput
	}
	if strings.TrimSpace(m.Record.Source) == "" {
		return WeakPreview{}, ErrInvalidInput
	}
	if err := g.boundRecords([]Record{{Content: m.Record.Content, Source: m.Record.Source, Evidence: m.Record.Evidence}}); err != nil {
		return WeakPreview{}, err
	}
	if g.provider == nil {
		return WeakPreview{}, ErrUnavailable
	}
	var observed *Record
	switch m.Operation {
	case OperationAdd:
		if m.ID != "" || m.ExpectedVersion != 0 {
			return WeakPreview{}, ErrInvalidInput
		}
		if _, ok := g.provider.(RecordWriter); !ok || !g.provider.Capabilities().Remember {
			return WeakPreview{}, ErrUnsupported
		}
	case OperationReplaceObserved:
		if ValidateRecordID(m.ID) != nil || m.ExpectedVersion == 0 {
			return WeakPreview{}, ErrInvalidInput
		}
		if _, ok := g.provider.(ObservedRecordReplacer); !ok || !g.provider.Capabilities().ObservedReplace {
			return WeakPreview{}, ErrUnsupported
		}
		old, err := g.Get(ctx, m.Record.Owner, m.ID)
		if err != nil {
			return WeakPreview{}, err
		}
		if old.State != RecordActive || old.Version != m.ExpectedVersion {
			return WeakPreview{}, ErrConflict
		}
		if err := g.boundRecords([]Record{old}); err != nil {
			return WeakPreview{}, err
		}
		observed = &old
	}
	m.Record.Evidence = append([]string(nil), m.Record.Evidence...)
	nonce, err := neutralID()
	if err != nil {
		return WeakPreview{}, err
	}
	p := WeakPreview{consumed: new(atomic.Bool), gateway: g, mutation: m, observed: observed, nonce: string(nonce)}
	b, marshalErr := json.Marshal(p.Plan())
	contentBytes := len(p.mutation.Record.Content)
	if p.observed != nil {
		contentBytes += len(p.observed.Content)
	}
	if marshalErr != nil || len(b) > g.policy.ContextBytes || contentBytes > g.policy.ContentBytes {
		return WeakPreview{}, ErrInvalidInput
	}
	digest := sha256.Sum256(b)
	p.intentID = hex.EncodeToString(digest[:])
	return p, nil
}
func (g *Gateway) CommitWeak(ctx context.Context, p WeakPreview, c Confirmation) (Record, error) {
	if p.gateway != g || p.intentID == "" || !c.Confirmed || c.IntentID != p.intentID || c.Owner != p.mutation.Record.Owner || c.Source != p.mutation.Record.Source {
		return Record{}, ErrNotConfirmed
	}
	if err := g.authorize(c.Owner, true); err != nil {
		return Record{}, err
	}
	if !g.access.AllowWeakWrites {
		return Record{}, ErrUnsupported
	}
	if p.consumed == nil || !p.consumed.CompareAndSwap(false, true) {
		return Record{}, ErrNotConfirmed
	}
	var r Record
	var err error
	if p.mutation.Operation == OperationAdd {
		r, err = Remember(ctx, g.provider, p.Plan().Record)
	} else {
		if p.observed == nil {
			return Record{}, ErrInvalidInput
		}
		r, err = g.provider.(ObservedRecordReplacer).ReplaceObserved(ctx, cloneRecord(*p.observed), p.Plan().Record)
	}
	if err != nil {
		return Record{}, SafeError(err)
	}
	if err := ValidateRecord(r, c.Owner); err != nil {
		return Record{}, ErrOutcomeUnknown
	}
	if p.observed != nil && (r.ID != p.observed.ID || r.Version != p.observed.Version+1) {
		return Record{}, ErrOutcomeUnknown
	}
	expected := p.Plan().Record
	if r.Type != expected.Type || r.Content != expected.Content || r.Source != expected.Source || !reflect.DeepEqual(r.Evidence, expected.Evidence) || r.State != RecordActive {
		return Record{}, ErrOutcomeUnknown
	}
	if err := g.boundRecords([]Record{r}); err != nil {
		return Record{}, ErrOutcomeUnknown
	}
	return cloneRecord(r), nil
}

// Full attributed records, including lineage, must fit all configured budgets.
// Inspection rejects an oversized history rather than silently claiming it is complete.
func (g *Gateway) boundRecords(rs []Record) error {
	if len(rs) > g.policy.MaxResults {
		return ErrInvalidInput
	}
	content := 0
	for _, r := range rs {
		content += len(r.Content)
		if content > g.policy.ContentBytes {
			return ErrInvalidInput
		}
	}
	b, err := json.Marshal(rs)
	if err != nil || len(b) > g.policy.ContextBytes {
		return ErrInvalidInput
	}
	return nil
}

const OperationReplaceObserved Operation = "replace-observed-weak"

type ObservedRecordReplacer interface {
	ReplaceObserved(context.Context, Record, NewRecord) (Record, error)
}
type RetainedRecordHistorian interface {
	RetainedHistory(context.Context, Owner, RecordID) ([]Record, error)
}

func (g *Gateway) RetainedHistory(ctx context.Context, owner Owner, id RecordID) ([]Record, error) {
	if err := g.authorize(owner, false); err != nil {
		return nil, err
	}
	if ValidateRecordID(id) != nil {
		return nil, ErrInvalidInput
	}
	if g.provider == nil {
		return nil, ErrUnavailable
	}
	h, ok := g.provider.(RetainedRecordHistorian)
	if !ok || !g.provider.Capabilities().RetainedHistory {
		return nil, ErrUnsupported
	}
	if err := providerReady(ctx, g.provider, true); err != nil {
		return nil, SafeError(err)
	}
	rs, err := h.RetainedHistory(ctx, owner, id)
	if err != nil {
		return nil, SafeError(err)
	}
	var version uint64
	for _, r := range rs {
		if err := ValidateRecord(r, owner); err != nil {
			return nil, err
		}
		if r.ID != id || r.State != RecordSuperseded || r.Version <= version {
			return nil, ErrInvalidInput
		}
		version = r.Version
	}
	if err := g.boundRecords(rs); err != nil {
		return nil, err
	}
	return cloneRecords(rs), nil
}

// Inspect returns one attributed record within the same query budgets.
func (g *Gateway) Inspect(ctx context.Context, owner Owner, id RecordID) (Record, error) {
	r, err := g.Get(ctx, owner, id)
	if err != nil {
		return Record{}, err
	}
	if err = g.boundRecords([]Record{r}); err != nil {
		return Record{}, err
	}
	return r, nil
}

// IntegrationCapabilities combines trusted authority and implemented interfaces
// without health/network access. Configuration labels alone cannot grant support.
type IntegrationCapabilities struct {
	Read            bool `json:"read"`
	Search          bool `json:"search"`
	WeakAdd         bool `json:"weakAdd"`
	WeakReplace     bool `json:"weakReplace"`
	RetainedHistory bool `json:"retainedHistory"`
}

func (g *Gateway) IntegrationCapabilities(owner Owner) IntegrationCapabilities {
	out := IntegrationCapabilities{}
	if g.authorize(owner, false) != nil || g.provider == nil {
		return out
	}
	caps := g.provider.Capabilities()
	_, reader := g.provider.(RecordReader)
	_, recaller := g.provider.(RecordRecaller)
	_, history := g.provider.(RetainedRecordHistorian)
	out.Read = reader && caps.Get
	out.Search = recaller && caps.Recall
	out.RetainedHistory = history && caps.RetainedHistory
	if g.authorize(owner, true) == nil && g.access.AllowWeakWrites {
		_, writer := g.provider.(RecordWriter)
		_, replacer := g.provider.(ObservedRecordReplacer)
		out.WeakAdd = writer && caps.Remember
		out.WeakReplace = replacer && caps.ObservedReplace
	}
	return out
}
