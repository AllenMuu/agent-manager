//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package filelock

import (
	"errors"
	"os"
)

func platformLock(*os.File) (func() error, error) {
	return nil, errors.New("operating-system file locks are not supported on this platform")
}
