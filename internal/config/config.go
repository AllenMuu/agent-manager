// Package config loads the local Agent Manager configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"gopkg.in/yaml.v3"
)

// SyncMode controls how managed Skills are materialized in agent directories.
type SyncMode string

const (
	SyncAuto        SyncMode = "auto"
	SyncSymlinkOnly SyncMode = "symlink-only"
	SyncCopyOnly    SyncMode = "copy-only"
)

// Config contains machine-local Agent Manager settings. Memory is optional so
// legacy Skill-only configuration files remain source-compatible.
type Config struct {
	Version     string                 `json:"version" yaml:"version"`
	LibraryPath string                 `json:"library" yaml:"library"`
	SyncMode    SyncMode               `json:"sync_mode" yaml:"sync_mode"`
	Memory      *memory.ProviderConfig `json:"memory,omitempty" yaml:"memory,omitempty"`
}

// Load reads the selected machine-local configuration. An empty path selects
// ~/.agent-manager/config.yaml; a missing default file keeps backward
// compatibility by using the default library and automatic link-first sync.
func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return defaults()
		} else if err != nil {
			return Config{}, fmt.Errorf("inspect default configuration: %w", err)
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	version, library, syncMode, provider, err := parseConfiguration(string(contents))
	if err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}
	if library == "" {
		defaultPath, err := defaultLibraryPath()
		if err != nil {
			return Config{}, err
		}
		return Config{Version: version, LibraryPath: defaultPath, SyncMode: syncMode, Memory: provider}, nil
	}
	if library == "~" || strings.HasPrefix(library, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve configured library home: %w", err)
		}
		library = filepath.Join(home, strings.TrimPrefix(library, "~"))
	} else if !filepath.IsAbs(library) {
		library = filepath.Join(filepath.Dir(path), library)
	}
	library, err = filepath.Abs(library)
	if err != nil {
		return Config{}, fmt.Errorf("resolve configured library: %w", err)
	}
	return Config{Version: version, LibraryPath: library, SyncMode: syncMode, Memory: provider}, nil
}

// DefaultPath returns the path used when a command does not receive --config.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve configuration home: %w", err)
	}
	return filepath.Join(home, ".agent-manager", "config.yaml"), nil
}

func defaults() (Config, error) {
	library, err := defaultLibraryPath()
	if err != nil {
		return Config{}, err
	}
	return Config{Version: "v1", LibraryPath: library, SyncMode: SyncAuto}, nil
}

// SaveSyncMode atomically updates only sync_mode, preserving comments and all
// other configuration entries (including settings unknown to this version).
// An empty path writes the standard machine-local configuration file.
func SaveSyncMode(path string, mode SyncMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve configuration path: %w", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read configuration for update: %w", err)
	}
	var document yaml.Node
	if len(contents) == 0 {
		document = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	} else if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse configuration for update: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("configuration must be a YAML mapping")
	}
	mapping := document.Content[0]
	updated := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "sync_mode" {
			continue
		}
		mapping.Content[i+1].Kind = yaml.ScalarNode
		mapping.Content[i+1].Tag = "!!str"
		mapping.Content[i+1].Value = string(mode)
		mapping.Content[i+1].Content = nil
		updated = true
		break
	}
	if !updated {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "sync_mode"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(mode)},
		)
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	return writeConfigAtomically(path, encoded)
}

func writeConfigAtomically(path string, contents []byte) (err error) {
	directory := filepath.Dir(path)
	mode := os.FileMode(0o600)
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("configuration path is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect configuration before update: %w", statErr)
	}
	temp, err := os.CreateTemp(directory, ".agent-manager-config-*.tmp")
	if err != nil {
		return fmt.Errorf("stage configuration update: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("set configuration permissions: %w", err)
	}
	if _, err := temp.Write(contents); err != nil {
		return fmt.Errorf("write configuration update: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync configuration update: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close configuration update: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish configuration update: %w", err)
	}
	return nil
}

func (m SyncMode) Validate() error {
	switch m {
	case SyncAuto, SyncSymlinkOnly, SyncCopyOnly:
		return nil
	default:
		return fmt.Errorf("unsupported sync mode %q", m)
	}
}

func (m SyncMode) Effective() SyncMode {
	if m == "" {
		return SyncAuto
	}
	return m
}

// Redacted returns an output-safe copy of the configuration. Provider
// references identify external secret stores and are intentionally omitted
// from status, diagnostics, and journal evidence.
func (c Config) Redacted() Config {
	redacted := c
	if c.Memory != nil {
		provider := c.Memory.Redacted()
		redacted.Memory = &provider
	}
	return redacted
}

// MarshalJSON keeps external provider references out of serialized config
// evidence while preserving the legacy library field name.
func (c Config) MarshalJSON() ([]byte, error) {
	type plain Config
	return json.Marshal(plain(c.Redacted()))
}

// MarshalYAML applies the same redaction rule to YAML diagnostics and output.
func (c Config) MarshalYAML() (any, error) {
	redacted := c.Redacted()
	return struct {
		Version  string                 `yaml:"version"`
		Library  string                 `yaml:"library"`
		SyncMode SyncMode               `yaml:"sync_mode"`
		Memory   *memory.ProviderConfig `yaml:"memory,omitempty"`
	}{Version: redacted.Version, Library: redacted.LibraryPath, SyncMode: redacted.SyncMode.Effective(), Memory: redacted.Memory}, nil
}

// JournalEvidence returns a redacted JSON representation suitable for local
// operation evidence. It never resolves or reads a provider reference.
func (c Config) JournalEvidence() string {
	b, _ := json.Marshal(c.Redacted())
	return string(b)
}

func (c Config) String() string { return c.JournalEvidence() }

func defaultLibraryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".agent-manager", "skills"), nil
}

func parseConfiguration(contents string) (string, string, SyncMode, *memory.ProviderConfig, error) {
	var fileConfig struct {
		Version  string                 `yaml:"version"`
		Library  string                 `yaml:"library"`
		SyncMode SyncMode               `yaml:"sync_mode"`
		Memory   *memory.ProviderConfig `yaml:"memory"`
	}
	if err := yaml.Unmarshal([]byte(contents), &fileConfig); err != nil {
		return "", "", "", nil, err
	}
	if fileConfig.Version == "" {
		fileConfig.Version = "v1"
	}
	if fileConfig.Version != "v1" {
		return "", "", "", nil, fmt.Errorf("unsupported configuration version %q", fileConfig.Version)
	}
	if fileConfig.SyncMode == "" {
		fileConfig.SyncMode = SyncAuto
	}
	if err := fileConfig.SyncMode.Validate(); err != nil {
		return "", "", "", nil, err
	}
	if fileConfig.Memory != nil {
		if err := fileConfig.Memory.Validate(); err != nil {
			return "", "", "", nil, fmt.Errorf("validate memory provider configuration: %w", err)
		}
	}
	return fileConfig.Version, fileConfig.Library, fileConfig.SyncMode, fileConfig.Memory, nil
}
