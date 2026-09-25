package memory_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

func TestDiscoverFileProviderReportsConfiguredCapabilitiesAndScopes(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.NewFileProvider(memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: store},
		Scopes:        []memory.Scope{memory.ScopeUser, memory.ScopeProject},
		Capabilities:  []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	status := provider.Status()
	if !status.Available || status.Reason != "" {
		t.Fatalf("status = %#v, want available without warning", status)
	}
	if len(status.Capabilities) != 2 || status.Capabilities[0] != memory.CapabilityRead || status.Capabilities[1] != memory.CapabilitySearch {
		t.Fatalf("capabilities = %#v", status.Capabilities)
	}
	if len(status.Scopes) != 2 || status.Scopes[0] != memory.ScopeUser || status.Scopes[1] != memory.ScopeProject {
		t.Fatalf("scopes = %#v", status.Scopes)
	}
}

func TestDiscoverFileProviderReportsActionableUnavailableStore(t *testing.T) {
	store := filepath.Join(t.TempDir(), "missing.json")
	status, err := memory.DiscoverFileProvider(fileProviderConfig(store))
	if !errors.Is(err, memory.ErrProviderUnavailable) {
		t.Fatalf("DiscoverFileProvider() error = %v, want unavailable error", err)
	}
	if status.Available || !strings.Contains(status.Reason, "create it or update") || strings.Contains(status.Reason, store) || strings.Contains(err.Error(), store) {
		t.Fatalf("unavailable status = %#v, want actionable reason", status)
	}
	if _, statErr := os.Stat(store); !os.IsNotExist(statErr) {
		t.Fatalf("discovery created missing store: %v", statErr)
	}
}

func TestDiscoverFileProviderRejectsMisconfiguredReference(t *testing.T) {
	cfg := fileProviderConfig(filepath.Join(t.TempDir(), "memory.json"))
	cfg.Configuration = memory.ConfigReference{Kind: "env", Name: "MEMORY_STORE"}
	status, err := memory.DiscoverFileProvider(cfg)
	if !errors.Is(err, memory.ErrProviderUnavailable) {
		t.Fatalf("DiscoverFileProvider() error = %v, want unavailable error", err)
	}
	if status.Available || !strings.Contains(status.Reason, "kind=file") {
		t.Fatalf("misconfigured status = %#v", status)
	}
}

func TestDiscoverFileProviderRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "external-memory.json")
	link := filepath.Join(root, "memory.json")
	original := []byte(`{"records":["keep"]}`)
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("test environment cannot create symlinks: %v", err)
		}
		t.Fatal(err)
	}
	status, err := memory.DiscoverFileProvider(fileProviderConfig(link))
	if !errors.Is(err, memory.ErrProviderUnavailable) {
		t.Fatalf("DiscoverFileProvider() error = %v, want unavailable error", err)
	}
	if status.Available || !strings.Contains(status.Reason, "not a symlink") {
		t.Fatalf("symlink status = %#v", status)
	}
	if content, readErr := os.ReadFile(target); readErr != nil || string(content) != string(original) {
		t.Fatalf("symlink discovery touched target: %q, %v", content, readErr)
	}
}

func TestDiscoverFileProviderReportsPermissionDeniedStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission bits do not enforce this fixture")
	}
	store := filepath.Join(t.TempDir(), "private-memory.json")
	if err := os.WriteFile(store, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o600) })
	status, err := memory.DiscoverFileProvider(fileProviderConfig(store))
	if err == nil {
		t.Skip("test process can read mode-zero files; permission denial is not enforceable")
	}
	if !errors.Is(err, memory.ErrProviderUnavailable) || status.Available || !strings.Contains(status.Reason, "permissions") {
		t.Fatalf("permission-denied status = %#v, err=%v", status, err)
	}
}

func TestDiscoverFileProviderDoesNotResolveNetworkReferences(t *testing.T) {
	cfg := fileProviderConfig("https://memory.example.invalid/store")
	status, err := memory.DiscoverFileProvider(cfg)
	if !errors.Is(err, memory.ErrProviderUnavailable) {
		t.Fatalf("DiscoverFileProvider() error = %v, want unavailable error", err)
	}
	if status.Available || !strings.Contains(status.Reason, "absolute") {
		t.Fatalf("network-like reference status = %#v", status)
	}
}

func TestFileProviderPromotionRequiresInitializedProvider(t *testing.T) {
	provider := &memory.FileProvider{}
	if !errors.Is(provider.Promote(memory.ScopeUser, "knowledge"), memory.ErrFileProviderPromotionUnsupported) {
		t.Fatal("Promote() did not reject an uninitialized provider")
	}
}

func TestFileProviderPromoteAppendsExplicitKnowledge(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.NewFileProvider(memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: store},
		Scopes:        []memory.Scope{memory.ScopeUser},
		Capabilities:  []memory.Capability{memory.CapabilityWrite},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	if err := provider.Promote(memory.ScopeUser, "remember this"); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	contents, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing\nremember this\n" {
		t.Fatalf("store = %q, want appended knowledge", contents)
	}
}

func TestFileProviderPromoteRejectsUnsupportedScopeAndCapability(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.NewFileProvider(memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: store},
		Scopes:        []memory.Scope{memory.ScopeUser},
		Capabilities:  []memory.Capability{memory.CapabilityRead},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	if err := provider.Promote(memory.ScopeProject, "knowledge"); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("Promote(project) error = %v, want scope refusal", err)
	}
	if err := provider.Promote(memory.ScopeUser, "knowledge"); err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("Promote(user) error = %v, want write-capability refusal", err)
	}
	contents, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing\n" {
		t.Fatalf("store changed after refused promotions: %q", contents)
	}
}

func TestFileProviderPromoteRejectsReplacementAfterDiscovery(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "memory.json")
	oldStore := filepath.Join(root, "memory.old.json")
	if err := os.WriteFile(store, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.NewFileProvider(memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: store},
		Scopes:        []memory.Scope{memory.ScopeUser},
		Capabilities:  []memory.Capability{memory.CapabilityWrite},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	if err := os.Rename(store, oldStore); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := provider.Promote(memory.ScopeUser, "must not write"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Promote() error = %v, want identity refusal", err)
	}
	if contents, readErr := os.ReadFile(store); readErr != nil || string(contents) != "replacement\n" {
		t.Fatalf("replacement store changed: %q, %v", contents, readErr)
	}
	if contents, readErr := os.ReadFile(oldStore); readErr != nil || string(contents) != "original\n" {
		t.Fatalf("original store changed: %q, %v", contents, readErr)
	}
}

func TestFileProviderPromoteRejectsEmptyKnowledge(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.NewFileProvider(memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: store},
		Scopes:        []memory.Scope{memory.ScopeUser},
		Capabilities:  []memory.Capability{memory.CapabilityWrite},
	})
	if err != nil {
		t.Fatalf("NewFileProvider() error = %v", err)
	}
	if err := provider.Promote(memory.ScopeUser, "  \n"); err == nil || !strings.Contains(err.Error(), "knowledge") {
		t.Fatalf("Promote(empty) error = %v, want knowledge refusal", err)
	}
}

func fileProviderConfig(path string) memory.ProviderConfig {
	return memory.ProviderConfig{
		Version: "v1", ID: "local", Provider: memory.FileProviderID,
		Configuration: memory.ConfigReference{Kind: "file", Name: path},
		Scopes:        []memory.Scope{memory.ScopeUser},
		Capabilities:  []memory.Capability{memory.CapabilityRead},
	}
}
