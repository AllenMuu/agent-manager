//go:build windows

package operation

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	lockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func platformFileLock(file *os.File) (func() error, error) {
	const exclusiveLock = 0x00000002
	overlapped := &syscall.Overlapped{}
	result, _, callErr := lockFileEx.Call(file.Fd(), exclusiveLock, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
	if result == 0 {
		return nil, callErr
	}
	return func() error {
		result, _, callErr := unlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		if result == 0 {
			return callErr
		}
		return nil
	}, nil
}
