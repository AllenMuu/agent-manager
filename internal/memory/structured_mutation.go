package memory

import "context"

// UpdateRequest replaces caller metadata on the same neutral ID. Ownership is
// immutable and ExpectedVersion is mandatory for every lifecycle mutation.
type UpdateRequest struct {
	Owner           Owner     `json:"owner"`
	ID              RecordID  `json:"id"`
	ExpectedVersion uint64    `json:"expectedVersion"`
	Record          NewRecord `json:"record"`
	OperationID     string    `json:"operationId,omitempty"`
}

// RecordUpdater is the optional conditional update boundary; support for a
// basic provider update alone does not advertise ConditionalUpdate.
type RecordUpdater interface {
	Update(context.Context, UpdateRequest) (Record, error)
}

func Update(ctx context.Context, p StructuredProvider, input UpdateRequest) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	mutator, ok := p.(RecordUpdater)
	if err := providerReady(ctx, p, ok && p.Capabilities().Update && p.Capabilities().ConditionalUpdate); err != nil {
		return Record{}, err
	}
	return mutator.Update(ctx, input)
}

// MutationRequest conditions retirement on the current ACTIVE version.
type MutationRequest struct {
	Owner           Owner    `json:"owner"`
	ID              RecordID `json:"id"`
	ExpectedVersion uint64   `json:"expectedVersion"`
	OperationID     string   `json:"operationId,omitempty"`
}
type RecordSuperseder interface {
	Supersede(context.Context, UpdateRequest) (Record, error)
}
type RecordForgetter interface {
	Forget(context.Context, MutationRequest) (Record, error)
}
type RecordHistorian interface {
	History(context.Context, Owner, RecordID) ([]Record, error)
}

func Supersede(ctx context.Context, p StructuredProvider, input UpdateRequest) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	m, ok := p.(RecordSuperseder)
	if err := providerReady(ctx, p, ok && p.Capabilities().Supersede && p.Capabilities().AtomicSupersede); err != nil {
		return Record{}, err
	}
	return m.Supersede(ctx, input)
}
func Forget(ctx context.Context, p StructuredProvider, input MutationRequest) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	m, ok := p.(RecordForgetter)
	if err := providerReady(ctx, p, ok && p.Capabilities().Forget); err != nil {
		return Record{}, err
	}
	return m.Forget(ctx, input)
}
func History(ctx context.Context, p StructuredProvider, owner Owner, id RecordID) ([]Record, error) {
	if p == nil {
		return nil, ErrUnavailable
	}
	m, ok := p.(RecordHistorian)
	if err := providerReady(ctx, p, ok && p.Capabilities().History); err != nil {
		return nil, err
	}
	return m.History(ctx, owner, id)
}

// RememberRequest exposes caller-supplied operation IDs without changing the
// legacy Provider/Promote or the original RecordWriter contract.
type RememberRequest struct {
	Record      NewRecord `json:"record"`
	OperationID string    `json:"operationId,omitempty"`
}
type RecordOperationWriter interface {
	RememberWithOperation(context.Context, RememberRequest) (Record, error)
}

func RememberWithOperation(ctx context.Context, p StructuredProvider, input RememberRequest) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	m, ok := p.(RecordOperationWriter)
	if err := providerReady(ctx, p, ok && p.Capabilities().Remember); err != nil {
		return Record{}, err
	}
	return m.RememberWithOperation(ctx, input)
}
