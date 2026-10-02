//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package memory

import (
	"context"
	"os"
)

func lockStore(context.Context, *os.File) (func(), error) { return nil, ErrUnsupported }
