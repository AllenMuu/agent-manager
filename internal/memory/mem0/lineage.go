package mem0

import (
	"context"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"net/url"
	"reflect"
)

const maxRetainedVersions = 8
const maxLineageBytes = 256 * 1024

// RetainedHistory is a bounded snapshot carried by the canonical backend object.
// It is not backend history, atomic lineage, or a provider operation receipt.
func (p *Provider) RetainedHistory(ctx context.Context, owner memory.Owner, id memory.RecordID) ([]memory.Record, error) {
	rr, err := p.locate(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	var current remoteRecord
	if err = p.request(ctx, "GET", "/memories/"+url.PathEscape(rr.ID), nil, &current); err != nil {
		return nil, err
	}
	r, err := canonical(current, owner)
	if err != nil {
		return nil, err
	}
	if r.ID != id {
		return nil, memory.ErrInvalidInput
	}
	return lineage(current, r)
}
func lineage(rr remoteRecord, current memory.Record) ([]memory.Record, error) {
	raw := rr.Metadata["am_retained_lineage"]
	if raw == "" {
		return []memory.Record{}, nil
	}
	if len(raw) > maxLineageBytes || memory.ValidateCanonicalJSON([]byte(raw)) != nil {
		return nil, memory.ErrInvalidInput
	}
	var rs []memory.Record
	if json.Unmarshal([]byte(raw), &rs) != nil || rs == nil || len(rs) > maxRetainedVersions {
		return nil, memory.ErrInvalidInput
	}
	var version uint64
	for _, r := range rs {
		if err := memory.ValidateRecord(r, current.Owner); err != nil {
			return nil, err
		}
		if r.ID != current.ID || r.State != memory.RecordSuperseded || r.Version <= version || r.Version >= current.Version {
			return nil, memory.ErrInvalidInput
		}
		version = r.Version
	}
	return rs, nil
}

// Recheck is advisory: the subsequent PUT remains unconditional. A concurrent
// writer can intervene after the comparison; no CAS guarantee is advertised.
func (p *Provider) ReplaceObserved(ctx context.Context, observed memory.Record, input memory.NewRecord) (memory.Record, error) {
	if memory.ValidateRecord(observed, observed.Owner) != nil || memory.ValidateNewRecord(input) != nil || input.Owner != observed.Owner {
		return memory.Record{}, memory.ErrInvalidInput
	}
	rr, err := p.locate(ctx, observed.Owner, observed.ID)
	if err != nil {
		return memory.Record{}, err
	}
	old, err := canonical(rr, observed.Owner)
	if err != nil {
		return memory.Record{}, err
	}
	if old.State != memory.RecordActive || !reflect.DeepEqual(old, observed) {
		return memory.Record{}, memory.ErrConflict
	}
	return p.replaceRemote(ctx, rr, old, input)
}
func (p *Provider) replaceRemote(ctx context.Context, rr remoteRecord, old memory.Record, input memory.NewRecord) (memory.Record, error) {
	if old.Version == ^uint64(0) || old.State != memory.RecordActive {
		return memory.Record{}, memory.ErrConflict
	}
	history, err := lineage(rr, old)
	if err != nil {
		return memory.Record{}, err
	}
	prior := old
	prior.State = memory.RecordSuperseded
	history = append(history, prior)
	if len(history) > maxRetainedVersions {
		history = history[len(history)-maxRetainedVersions:]
	}
	b, err := json.Marshal(history)
	if err != nil || len(b) > maxLineageBytes {
		return memory.Record{}, memory.ErrInvalidInput
	}
	layer := input.Layer
	if layer == "" {
		layer = memory.LayerRaw
	}
	r := memory.Record{ID: old.ID, Version: old.Version + 1, Owner: input.Owner, Type: input.Type, Content: input.Content, Source: input.Source, Evidence: input.Evidence, State: memory.RecordActive, Layer: layer}
	meta := metadata(r)
	meta["am_retained_lineage"] = string(b)
	if err = p.request(ctx, "PUT", "/memories/"+url.PathEscape(rr.ID), map[string]any{"text": r.Content, "metadata": meta}, nil); err != nil {
		return memory.Record{}, err
	}
	var out remoteRecord
	if err = p.request(ctx, "GET", "/memories/"+url.PathEscape(rr.ID), nil, &out); err != nil {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	got, err := canonical(out, input.Owner)
	if err != nil || !reflect.DeepEqual(got, r) || out.Metadata["am_retained_lineage"] != string(b) {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	return got, nil
}
