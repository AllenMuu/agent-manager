//go:build !windows && !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package run

import (
	"errors"
	"os"
)

func lockPlatformFile(*os.File) (func() error, error) {
	return nil, errors.New("cross-process run state locking is unsupported on this platform")
}
