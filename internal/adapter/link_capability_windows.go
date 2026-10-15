//go:build windows

package adapter

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func classifySymlinkError(err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
		return errors.Join(ErrLinkUnavailable, fmt.Errorf("create soft link: %w", err))
	}
	return err
}
