//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package adapter

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func classifySymlinkError(err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return errors.Join(ErrLinkUnavailable, fmt.Errorf("create soft link: %w", err))
	}
	return err
}
