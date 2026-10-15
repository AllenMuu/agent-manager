//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package filelock

import (
	"os"
	"syscall"
)

func platformLock(file *os.File) (func() error, error) {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	return func() error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }, nil
}
