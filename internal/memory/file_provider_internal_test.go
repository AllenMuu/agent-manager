package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromoteRollsBackPartialWrite(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newWritableFileProviderForTest(t, store)
	originalWrite := fileAppendWrite
	t.Cleanup(func() { fileAppendWrite = originalWrite })
	fileAppendWrite = func(file *os.File, data []byte) (int, error) {
		n := len(data) / 2
		if n == 0 {
			n = 1
		}
		if _, err := file.Write(data[:n]); err != nil {
			return 0, err
		}
		return n, errors.New("injected write failure")
	}

	if err := provider.Promote(ScopeUser, "partial knowledge"); err == nil {
		t.Fatal("Promote() error = nil, want injected write failure")
	}
	assertFileBytes(t, store, original)
}

func TestPromoteRollsBackSyncFailure(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newWritableFileProviderForTest(t, store)
	originalSync := fileAppendSync
	t.Cleanup(func() { fileAppendSync = originalSync })
	var calls int
	fileAppendSync = func(file *os.File) error {
		calls++
		if calls == 1 {
			return errors.New("injected sync failure")
		}
		return file.Sync()
	}

	if err := provider.Promote(ScopeUser, "sync knowledge"); err == nil {
		t.Fatal("Promote() error = nil, want injected sync failure")
	}
	assertFileBytes(t, store, original)
}

func TestPromoteRollsBackCloseFailure(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newWritableFileProviderForTest(t, store)
	originalClose := fileAppendClose
	t.Cleanup(func() { fileAppendClose = originalClose })
	var calls int
	fileAppendClose = func(file *os.File) error {
		calls++
		if calls == 1 {
			return errors.New("injected close failure")
		}
		return file.Close()
	}

	if err := provider.Promote(ScopeUser, "close knowledge"); err == nil {
		t.Fatal("Promote() error = nil, want injected close failure")
	}
	assertFileBytes(t, store, original)
}

func TestPromoteHandlesShortWrites(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newWritableFileProviderForTest(t, store)
	originalWrite := fileAppendWrite
	t.Cleanup(func() { fileAppendWrite = originalWrite })
	var calls int
	fileAppendWrite = func(file *os.File, data []byte) (int, error) {
		calls++
		if calls == 1 {
			n := len(data) / 2
			if n == 0 {
				n = 1
			}
			if _, err := file.Write(data[:n]); err != nil {
				return 0, err
			}
			return n, nil
		}
		return file.Write(data)
	}

	if err := provider.Promote(ScopeUser, "short knowledge"); err != nil {
		t.Fatalf("Promote() error = %v, want short write recovery", err)
	}
	assertFileBytes(t, store, []byte("existing\nshort knowledge\n"))
}

func TestPromotePreservesConcurrentAppendDuringRollback(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newWritableFileProviderForTest(t, store)
	originalWrite := fileAppendWrite
	t.Cleanup(func() { fileAppendWrite = originalWrite })
	fileAppendWrite = func(file *os.File, data []byte) (int, error) {
		if _, err := file.Write(data); err != nil {
			return 0, err
		}
		external, err := os.OpenFile(store, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return len(data), err
		}
		_, _ = external.WriteString("external append\n")
		_ = external.Close()
		return len(data), errors.New("injected write failure after concurrent append")
	}

	err := provider.Promote(ScopeUser, "knowledge to roll back")
	if err == nil || !strings.Contains(err.Error(), "recovery warning") {
		t.Fatalf("Promote() error = %v, want recovery warning", err)
	}
	contents, readErr := os.ReadFile(store)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(contents), "external append\n") {
		t.Fatalf("concurrent append was truncated: %q", contents)
	}
}

func newWritableFileProviderForTest(t *testing.T, store string) *FileProvider {
	t.Helper()
	provider, err := NewFileProvider(ProviderConfig{
		Version: "v1", ID: "local", Provider: FileProviderID,
		Configuration: ConfigReference{Kind: "file", Name: store},
		Scopes:        []Scope{ScopeUser},
		Capabilities:  []Capability{CapabilityWrite},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	return provider
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("file = %q, want %q", got, want)
	}
}
