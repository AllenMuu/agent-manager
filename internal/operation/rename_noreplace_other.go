//go:build !linux && !darwin && !windows

package operation

import (
	"fmt"
	"runtime"
)

func renameNoReplace(_, _ string) error {
	return fmt.Errorf("atomic no-replace restore is unsupported on %s", runtime.GOOS)
}
