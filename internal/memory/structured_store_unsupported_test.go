//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package memory_test

import (
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"testing"
)

func TestStructuredStoreUnsupportedPlatform(t *testing.T) {
	_, err := memory.OpenStructuredStore(t.TempDir())
	if !errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("unsupported platform: %v", err)
	}
}
