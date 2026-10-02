//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package memory

import (
	"context"
	"os"
)

func openStoreDirectory(string) (*os.File, error)    { return nil, ErrUnsupported }
func readStoreFile(*os.File, string) ([]byte, error) { return nil, ErrUnsupported }
func replaceStoreFile(context.Context, *os.File, string, []byte, FileSyncer) error {
	return ErrUnsupported
}

func syncStoreFile(FileSyncer, *os.File) error { return ErrUnsupported }
