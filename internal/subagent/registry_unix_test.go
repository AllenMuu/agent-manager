//go:build !windows

package subagent_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/subagent"
)

func TestDiscoverReportsSpecialDefinitionWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, subagent.DefinitionsDirectory, "blocked.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := subagent.NewRegistry(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var definitions []subagent.Definition
	var diagnostics []subagent.Diagnostic
	var discoverErr error
	go func() {
		definitions, diagnostics, discoverErr = registry.Discover()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Discover() blocked while inspecting a special file")
	}
	if discoverErr != nil {
		t.Fatal(discoverErr)
	}
	if len(definitions) != 0 || len(diagnostics) != 1 || !strings.Contains(strings.ToLower(diagnostics[0].Error()), "regular") {
		t.Fatalf("Discover() = (%v, %v), want special-file diagnostic", definitions, diagnostics)
	}
}
