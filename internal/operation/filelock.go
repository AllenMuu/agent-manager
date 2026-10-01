package operation

import (
	"fmt"
	"os"
	"path/filepath"
)

// acquireFileLock acquires an operating-system lock that is shared by separate
// CLI and WebUI processes using the same project.
func acquireFileLock(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock: %w", err)
	}
	unlock, err := platformFileLock(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire operation lock: %w", err)
	}
	return func() error {
		unlockErr := unlock()
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
