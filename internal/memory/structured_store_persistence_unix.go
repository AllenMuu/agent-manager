//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openStoreDirectory(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), absolute), nil
}
func openStoreFile(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("regular file required: %s", name)
	}
	return f, nil
}
func readStoreFile(dir *os.File, name string) ([]byte, error) {
	f, err := openStoreFile(dir, name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
func replaceStoreFile(ctx context.Context, dir *os.File, name string, data []byte, syncer FileSyncer) error {
	if err := operationContext(ctx); err != nil {
		return err
	}
	id, err := neutralID()
	if err != nil {
		return err
	}
	temp := "." + string(id) + ".tmp"
	f, err := openStoreFile(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(dir.Fd()), temp, 0)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = syncStoreFile(syncer, f); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = operationContext(ctx); err != nil {
		return err
	}
	// Reject a direct symlink or special file already occupying the destination.
	existing, e := openStoreFile(dir, name, unix.O_RDONLY)
	if e == nil {
		existing.Close()
	} else if !os.IsNotExist(e) {
		return e
	}
	if err = unix.Renameat(int(dir.Fd()), temp, int(dir.Fd()), name); err != nil {
		return err
	}
	if err = syncStoreFile(syncer, dir); err != nil {
		return fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return nil
}

func syncStoreFile(syncer FileSyncer, file *os.File) error {
	if syncer != nil {
		return syncer.Sync(file)
	}
	return file.Sync()
}

func inspectStoreFile(dir *os.File, name string) (os.FileInfo, error) {
	file, err := openStoreFile(dir, name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}
