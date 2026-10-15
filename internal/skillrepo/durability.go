package skillrepo

import (
	"os"
	"path/filepath"
)

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func syncDirectoryChain(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	physical, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return err
	}
	for current := filepath.Clean(physical); ; current = filepath.Dir(current) {
		if err := syncDirectory(current); err != nil {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
