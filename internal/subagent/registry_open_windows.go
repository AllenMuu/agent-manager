//go:build windows

package subagent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Windows does not expose the Unix O_NOFOLLOW/O_NONBLOCK flags used by the
// hardened implementation. Lstat-before-open plus post-open identity checks
// provide the safest portable fallback available through the standard library.
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
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open SubAgent definitions directory: %w", err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect SubAgent definitions directory: %w", err)
	}
	if !opened.IsDir() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("SubAgent definitions directory changed while opening %q", path)
	}
	return file, nil
}

func readRegularDefinition(path string, expectedParent os.FileInfo) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect definition: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("definition is a symlink; refusing to follow %q", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open definition without following links: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect definition: %w", err)
	}
	if !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("definition %q is not a regular file (mode %s)", path, opened.Mode().Type())
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("definition changed while opening %q", path)
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
