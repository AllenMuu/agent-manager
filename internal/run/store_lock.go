package run

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const storeLockFileName = ".state.lock"

func acquireStoreLock(root string) (func() error, error) {
	path := filepath.Join(root, storeLockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open run state lock: %w", err)
	}
	closeOnError := func(err error) (func() error, error) {
		_ = file.Close()
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("inspect run state lock: %w", err))
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return closeOnError(fmt.Errorf("inspect run state lock path: %w", err))
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || !os.SameFile(openedInfo, pathInfo) {
		return closeOnError(errors.New("run state lock must be a direct regular file"))
	}
	if err := file.Chmod(0o600); err != nil {
		return closeOnError(fmt.Errorf("secure run state lock: %w", err))
	}
	unlock, err := lockPlatformFile(file)
	if err != nil {
		return closeOnError(fmt.Errorf("lock run state: %w", err))
	}
	return func() error {
		unlockErr := unlock()
		closeErr := file.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}
