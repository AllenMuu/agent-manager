//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package adapter

func classifySymlinkError(err error) error { return err }
