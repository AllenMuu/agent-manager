package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// FileSyncer is the external filesystem durability boundary. A nil syncer uses
// os.File.Sync. It permits testing real atomic replacement at both sync stages.
type FileSyncer interface{ Sync(*os.File) error }

// StorePersistence atomically replaces the canonical batch through an anchored
// directory handle. Before replacement, errors must mean no commit. After
// replacement an unconfirmed durability result must wrap ErrOutcomeUnknown.
// Confirm must establish durable state without repeating the mutation.
// Implementations must preserve these semantics and never follow symlinks.
type StorePersistence interface {
	Commit(context.Context, *os.File, []byte) error
	Confirm(context.Context, *os.File) error
}
type AtomicFilePersistence struct{ Syncer FileSyncer }

func (p AtomicFilePersistence) Commit(ctx context.Context, dir *os.File, data []byte) error {
	err := replaceStoreFile(ctx, dir, "memory.json", data, p.Syncer)
	if err != nil && !errors.Is(err, ErrOutcomeUnknown) {
		return fmt.Errorf("%w: %w", ErrNotCommitted, err)
	}
	return err
}

type StoreOption func(*StructuredStore) error

func WithPersistence(p StorePersistence) StoreOption {
	return func(s *StructuredStore) error {
		if p == nil {
			return ErrInvalidInput
		}
		s.persistence = p
		return nil
	}
}

// Confirm durably flushes a recognized receipt before a retry reports success.
func (p AtomicFilePersistence) Confirm(ctx context.Context, dir *os.File) error {
	if err := operationContext(ctx); err != nil {
		return err
	}
	if err := syncStoreFile(p.Syncer, dir); err != nil {
		return fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return nil
}
