package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/config"
	"gopkg.in/yaml.v3"
)

func TestLoadUsesAgentManagerSkillsInHomeDirectoryByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(home, ".agent-manager", "skills")
	if loaded.LibraryPath != want {
		t.Errorf("LibraryPath = %q, want %q", loaded.LibraryPath, want)
	}
	if loaded.SyncMode != config.SyncAuto {
		t.Errorf("SyncMode = %q, want auto", loaded.SyncMode)
	}
}

func TestLoadReadsDefaultConfigAndSyncMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agent-manager", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("library: ./shared\nsync_mode: copy-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LibraryPath != filepath.Join(home, ".agent-manager", "shared") {
		t.Fatalf("LibraryPath = %q", loaded.LibraryPath)
	}
	if loaded.SyncMode != config.SyncCopyOnly {
		t.Fatalf("SyncMode = %q, want copy-only", loaded.SyncMode)
	}
}

func TestLoadRejectsUnsupportedSyncMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("sync_mode: hardlink\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "unsupported sync mode") {
		t.Fatalf("Load() error = %v, want unsupported sync mode", err)
	}
}

func TestLoadAcceptsEverySupportedSyncMode(t *testing.T) {
	for _, mode := range []config.SyncMode{config.SyncAuto, config.SyncSymlinkOnly, config.SyncCopyOnly} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("sync_mode: "+string(mode)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if loaded.SyncMode != mode {
				t.Fatalf("SyncMode = %q, want %q", loaded.SyncMode, mode)
			}
		})
	}
}

func TestSaveSyncModeCreatesDefaultConfigAndPreservesOtherValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agent-manager", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "# keep this comment\nversion: v1\nlibrary: ~/.agents/skills\ncustom: keep-me\nmemory:\n  provider: local\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSyncMode("", config.SyncSymlinkOnly); err != nil {
		t.Fatalf("SaveSyncMode() error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"# keep this comment", "library: ~/.agents/skills", "custom: keep-me", "provider: local", "sync_mode: symlink-only"} {
		if !strings.Contains(string(updated), preserved) {
			t.Errorf("updated config omitted %q:\n%s", preserved, updated)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestSaveSyncModeRejectsUnsupportedValue(t *testing.T) {
	if err := config.SaveSyncMode(filepath.Join(t.TempDir(), "config.yaml"), config.SyncMode("hardlink")); err == nil {
		t.Fatal("SaveSyncMode() error = nil, want unsupported mode")
	}
}

func TestSaveSyncModeUsesExplicitConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.SaveSyncMode(path, config.SyncCopyOnly); err != nil {
		t.Fatalf("SaveSyncMode() error = %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SyncMode != config.SyncCopyOnly {
		t.Fatalf("SyncMode = %q, want copy-only", loaded.SyncMode)
	}
}

func TestLoadKeepsLegacyLibraryAvailableAsExplicitOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: ~/.agents/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := filepath.Join(home, ".agents", "skills"); loaded.LibraryPath != want {
		t.Fatalf("LibraryPath=%q, want explicit legacy path %q", loaded.LibraryPath, want)
	}
}

func TestLoadUsesConfiguredLibraryPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: /var/lib/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LibraryPath != "/var/lib/skills" {
		t.Errorf("LibraryPath = %q, want %q", loaded.LibraryPath, "/var/lib/skills")
	}
	if loaded.Version != "v1" {
		t.Errorf("Version = %q, want v1 for a legacy configuration", loaded.Version)
	}
}

func TestLoadParsesCommentedYAMLConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("# Skill Manager configuration\nlibrary: \"/var/lib/shared skills\" # local library\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LibraryPath != "/var/lib/shared skills" {
		t.Errorf("LibraryPath = %q, want %q", loaded.LibraryPath, "/var/lib/shared skills")
	}
}

func TestLoadRejectsMalformedYAMLConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: [not closed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(configPath); err == nil {
		t.Fatal("Load() error = nil, want malformed YAML error")
	}
}

func TestLoadExplicitConfigDoesNotRequireHomeDirectory(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: /var/lib/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want explicit configuration to load without HOME", err)
	}
	if loaded.LibraryPath != "/var/lib/skills" {
		t.Errorf("LibraryPath = %q, want %q", loaded.LibraryPath, "/var/lib/skills")
	}
}

func TestLoadExpandsCurrentUserHomeRelativeLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: ~/.agents/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".agents", "skills"); loaded.LibraryPath != want {
		t.Fatalf("LibraryPath=%q, want %q", loaded.LibraryPath, want)
	}
}

func TestLoadParsesVersionedMemoryProviderConfiguration(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`version: v1
library: ./skills
memory:
  version: v1
  id: shared-memory
  provider: graphiti
  configuration:
    kind: env
    name: GRAPHITI_TOKEN
  scopes:
    - user
    - project
  capabilities:
    - read
    - write
    - search
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Memory == nil {
		t.Fatal("Memory = nil, want parsed provider configuration")
	}
	if loaded.Memory.ID != "shared-memory" || loaded.Memory.Provider != "graphiti" || loaded.Memory.Configuration.Kind != "env" {
		t.Fatalf("Memory = %#v", loaded.Memory)
	}
	if loaded.LibraryPath != filepath.Join(root, "skills") {
		t.Fatalf("LibraryPath = %q, want config-relative path", loaded.LibraryPath)
	}

	jsonBytes, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{string(jsonBytes), loaded.JournalEvidence()} {
		if strings.Contains(output, "GRAPHITI_TOKEN") {
			t.Fatalf("serialized config leaked provider reference: %s", output)
		}
		if !strings.Contains(output, "[redacted]") {
			t.Fatalf("serialized config omitted redaction: %s", output)
		}
	}
	yamlBytes, err := yaml.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if output := string(yamlBytes); strings.Contains(output, "GRAPHITI_TOKEN") || !strings.Contains(output, "[redacted]") {
		t.Fatalf("YAML config leaked or omitted redaction: %s", output)
	}
}

func TestLoadRejectsInvalidMemoryProviderReferencesScopesAndCapabilities(t *testing.T) {
	cases := []struct {
		name   string
		memory string
		want   string
	}{
		{
			name:   "unsupported reference kind",
			memory: "configuration:\n  kind: inline\n  name: secret\nscopes: [user]\ncapabilities: [read]",
			want:   "unsupported memory configuration reference kind",
		},
		{
			name:   "invalid environment reference",
			memory: "configuration:\n  kind: env\n  name: token=value\nscopes: [user]\ncapabilities: [read]",
			want:   "valid environment variable name",
		},
		{
			name:   "unsupported scope",
			memory: "configuration:\n  kind: env\n  name: TOKEN\nscopes: [workspace]\ncapabilities: [read]",
			want:   "unsupported memory scope",
		},
		{
			name:   "unsupported capability",
			memory: "configuration:\n  kind: env\n  name: TOKEN\nscopes: [user]\ncapabilities: [delete]",
			want:   "unsupported memory capability",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := "version: v1\nlibrary: /tmp/skills\nmemory:\n  version: v1\n  id: shared\n  provider: test\n  " + strings.ReplaceAll(tc.memory, "\n", "\n  ") + "\n"
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}
