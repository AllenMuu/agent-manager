//go:build !windows

package subagent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// openDefinitionsDirectory opens the registry directory without following a
// symlink. Reading entries from the open handle also keeps discovery anchored
// to the directory that was inspected, even if its path is later replaced.
func openDefinitionsDirectory(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect SubAgent definitions directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("SubAgent definitions directory %q is a symlink; refusing to follow it", path)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("SubAgent definitions path %q is not a directory", path)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("SubAgent definitions directory %q is a symlink; refusing to follow it", path)
		}
		return nil, fmt.Errorf("inspect SubAgent definitions directory: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("inspect SubAgent definitions directory: unable to create file handle for %q", path)
	}
	info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect SubAgent definitions directory: %w", err)
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("SubAgent definitions path %q is not a directory", path)
	}
	return file, nil
}

// readRegularDefinition opens a definition without following a symlink and
// refuses non-regular files before reading any bytes. O_NONBLOCK is important
// for special files such as FIFOs: discovery must never wait for a writer.
func readRegularDefinition(path string, expectedParent os.FileInfo) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("definition is a symlink; refusing to follow %q", path)
		}
		return nil, fmt.Errorf("open definition without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("open definition without following links: unable to create file handle for %q", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect definition: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("definition %q is not a regular file (mode %s)", path, info.Mode().Type())
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("inspect definition parent: %w", err)
	}
	if expectedParent != nil && !os.SameFile(expectedParent, parent) {
		return nil, fmt.Errorf("definition parent changed; refusing to read %q", path)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxDefinitionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read definition: %w", err)
	}
	if len(contents) > maxDefinitionBytes {
		return nil, fmt.Errorf("definition %q exceeds maximum size of %d bytes", path, maxDefinitionBytes)
	}
	return contents, nil
}
