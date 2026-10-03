package memory

import "context"

// ReplaceRequest is explicitly unconditional; it carries no expected version.
type ReplaceRequest struct {
	Owner  Owner
	ID     RecordID
	Record NewRecord
}
type RecordReplacer interface {
	Replace(context.Context, ReplaceRequest) (Record, error)
}
type RecordRemover interface {
	Remove(context.Context, Owner, RecordID) error
}

// Replace dispatches an explicitly weaker operation. It never provides CAS,
// operation receipts, or Gateway preview/confirmation semantics.
func Replace(ctx context.Context, p StructuredProvider, input ReplaceRequest) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	m, ok := p.(RecordReplacer)
	if err := providerReady(ctx, p, ok && p.Capabilities().BasicReplace); err != nil {
		return Record{}, err
	}
	return m.Replace(ctx, input)
}

// Remove unconditionally deletes one owned record; no version guard is implied.
func Remove(ctx context.Context, p StructuredProvider, owner Owner, id RecordID) error {
	if p == nil {
		return ErrUnavailable
	}
	m, ok := p.(RecordRemover)
	if err := providerReady(ctx, p, ok && p.Capabilities().BasicRemove); err != nil {
		return err
	}
	return m.Remove(ctx, owner, id)
}
