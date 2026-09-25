//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory

import (
	"os"

	"golang.org/x/sys/unix"
)

func openMemoryFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openMemoryFileAppend(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_APPEND|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func truncateMemoryFile(file *os.File, _ string, _ os.FileInfo, size int64) error {
	return fileAppendTruncate(file, size)
}
