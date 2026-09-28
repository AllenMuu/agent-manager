package operation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// FingerprintPath returns a deterministic digest of a filesystem tree without
// following symbolic links or creating snapshots. It is suitable for detecting
// edits between a reviewed operation plan and its execution.
func FingerprintPath(path string) (string, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if err := fingerprintPath(h, root, root); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fingerprintPath(h hash.Hash, root, current string) error {
	info, err := os.Lstat(current)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, current)
	if err != nil {
		return err
	}
	writeFingerprintField(h, filepath.ToSlash(relative))
	var mode [8]byte
	binary.BigEndian.PutUint64(mode[:], uint64(info.Mode()))
	_, _ = h.Write(mode[:])

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(current)
		if err != nil {
			return err
		}
		writeFingerprintField(h, target)
	case info.IsDir():
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(entries)))
		_, _ = h.Write(count[:])
		for _, entry := range entries {
			if err := fingerprintPath(h, root, filepath.Join(current, entry.Name())); err != nil {
				return err
			}
		}
	case info.Mode().IsRegular():
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		contentHash := sha256.New()
		_, copyErr := io.Copy(contentHash, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		writeFingerprintField(h, hex.EncodeToString(contentHash.Sum(nil)))
	default:
		return ErrUnsafePath
	}
	return nil
}

func writeFingerprintField(h hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = io.WriteString(h, value)
}
