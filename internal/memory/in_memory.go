package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// InMemoryProvider is deterministic, process-local contract storage. It is not
// durable and is never registered as a default provider or legacy importer.
type InMemoryProvider struct {
	mu      sync.RWMutex
	records []Record
}

func NewInMemoryProvider() *InMemoryProvider { return &InMemoryProvider{} }
func (p *InMemoryProvider) Remember(ctx context.Context, input NewRecord) (Record, error) {
	if _, err := p.Health(ctx); err != nil {
		return Record{}, err
	}
	if err := input.validate(); err != nil {
		return Record{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := operationContext(ctx); err != nil {
		return Record{}, err
	}
	layer := input.Layer
	if layer == "" {
		layer = LayerRaw
	}
	record := Record{ID: RecordID(fmt.Sprintf("memory-%020d", len(p.records)+1)), Version: 1, Owner: input.Owner, Type: input.Type, Content: input.Content, Source: input.Source, Evidence: append([]string(nil), input.Evidence...), Layer: layer, State: RecordActive}
	p.records = append(p.records, record)
	return cloneRecord(record), nil
}
func (p *InMemoryProvider) Get(ctx context.Context, owner Owner, id RecordID) (Record, error) {
	if _, err := p.Health(ctx); err != nil {
		return Record{}, err
	}
	if err := owner.validate(); err != nil {
		return Record{}, err
	}
	if err := validateToken("record ID", string(id)); err != nil {
		return Record{}, fmt.Errorf("%w: invalid record ID", ErrInvalidInput)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := operationContext(ctx); err != nil {
		return Record{}, err
	}
	for _, record := range p.records {
		if record.ID == id && record.Owner == owner {
			return cloneRecord(record), nil
		}
	}
	return Record{}, ErrNotFound
}
func (p *InMemoryProvider) Recall(ctx context.Context, q Query) ([]Record, error) {
	if _, err := p.Health(ctx); err != nil {
		return nil, err
	}
	if err := q.Owner.validate(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := operationContext(ctx); err != nil {
		return nil, err
	}
	records := []Record{}
	for _, record := range p.records {
		if record.Owner == q.Owner && strings.Contains(strings.ToLower(record.Content), strings.ToLower(q.Text)) {
			records = append(records, cloneRecord(record))
		}
	}
	return records, nil
}

func cloneRecord(record Record) Record {
	record.Evidence = append([]string(nil), record.Evidence...)
	return record
}

func (p *InMemoryProvider) Capabilities() StructuredCapabilities {
	return StructuredCapabilities{Remember: true, Get: true, Recall: true}
}
func (p *InMemoryProvider) Health(ctx context.Context) (HealthStatus, error) {
	if err := operationContext(ctx); err != nil {
		return HealthStatus{}, err
	}
	if p == nil {
		return HealthStatus{Reason: "in-memory provider is not initialized"}, ErrUnavailable
	}
	return HealthStatus{Available: true}, nil
}
