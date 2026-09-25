//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package memory

import "os"

// These targets do not expose a common no-follow open primitive through the
// standard library. DiscoverFileProvider still performs Lstat, opens the file,
// and verifies the opened identity with os.SameFile. That closes ordinary
// replacement races; a platform-specific symlink swap during the open remains
// a weaker guarantee and is reported by the post-open identity check whenever
// the platform exposes distinguishable file identities.
func openMemoryFileNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}

func openMemoryFileAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
}

func truncateMemoryFile(file *os.File, _ string, _ os.FileInfo, size int64) error {
	return fileAppendTruncate(file, size)
}
