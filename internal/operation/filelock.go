package operation

import "github.com/AllenMuu/skill-manager/internal/filelock"

func acquireFileLock(path string) (func() error, error) { return filelock.Acquire(path) }
