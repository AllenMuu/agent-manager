//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const structuredLocalSupported = true

func lockStore(ctx context.Context, dir *os.File) (func(), error) {
	f, err := openStoreFile(dir, "memory.lock", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for {
		if err = operationContext(ctx); err != nil {
			f.Close()
			return nil, err
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	current, err := openStoreFile(dir, "memory.lock", unix.O_RDONLY)
	if err != nil {
		f.Close()
		return nil, err
	}
	a, e1 := f.Stat()
	b, e2 := current.Stat()
	current.Close()
	if e1 != nil || e2 != nil || !os.SameFile(a, b) {
		f.Close()
		return nil, fmt.Errorf("%w: lock file replaced", ErrUnavailable)
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
