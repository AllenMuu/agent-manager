//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package operation

import (
	"errors"
	"os"
)

func platformFileLock(*os.File) (func() error, error) {
	return nil, errors.New("operating-system file locks are not supported on this platform")
}
