package operation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SyncPaths flushes each published path and the directory entries that lead
// to it. Symlinks are never followed; syncing their parent makes the link
// publication durable.
func SyncPaths(paths []string) error {
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := syncPublishedPath(filepath.Clean(abs)); err != nil {
			return fmt.Errorf("sync published path %s: %w", abs, err)
		}
	}
	return nil
}

func syncPublishedPath(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return syncDirectoryChain(filepath.Dir(path))
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return syncDirectoryChain(filepath.Dir(path))
	}
	if info.IsDir() {
		if err := syncPublishedTree(path); err != nil {
			return err
		}
		return syncDirectoryChain(filepath.Dir(path))
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafePath
	}
	if err := syncRegularFile(path); err != nil {
		return err
	}
	return syncDirectoryChain(filepath.Dir(path))
}

func syncPublishedTree(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			if err := syncPublishedTree(path); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return ErrUnsafePath
		}
		if err := syncRegularFile(path); err != nil {
			return err
		}
	}
	return syncDirectory(root)
}

func syncRegularFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("sync file: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func syncDirectoryChain(path string) error {
	return syncDirectoryChainWith(path, syncDirectory)
}

func syncDirectoryChainWith(path string, syncDir func(string) error) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	physical, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return err
	}
	for current := filepath.Clean(physical); ; current = filepath.Dir(current) {
		if err := syncDir(current); err != nil {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
