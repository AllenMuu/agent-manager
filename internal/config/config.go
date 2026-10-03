// Package config loads the local Skill Manager configuration.
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

// Config contains machine-local Agent Manager settings. Memory is optional so
// legacy Skill-only configuration files remain source-compatible.
type Config struct {
	Version     string                 `json:"version" yaml:"version"`
	LibraryPath string                 `json:"library" yaml:"library"`
	Memory      *memory.ProviderConfig `json:"memory,omitempty" yaml:"memory,omitempty"`
}

// Load returns the default skill library when path is empty. A supplied config
// file may set its library field to select another local skill library.
func Load(path string) (Config, error) {
	if path == "" {
		defaultPath, err := defaultLibraryPath()
		if err != nil {
			return Config{}, err
		}
		return Config{Version: "v1", LibraryPath: defaultPath}, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	version, library, provider, err := parseConfiguration(string(contents))
	if err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}
	if library == "" {
		defaultPath, err := defaultLibraryPath()
		if err != nil {
			return Config{}, err
		}
		return Config{Version: version, LibraryPath: defaultPath, Memory: provider}, nil
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
	return Config{Version: version, LibraryPath: library, Memory: provider}, nil
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
		Version string                 `yaml:"version"`
		Library string                 `yaml:"library"`
		Memory  *memory.ProviderConfig `yaml:"memory,omitempty"`
	}{Version: redacted.Version, Library: redacted.LibraryPath, Memory: redacted.Memory}, nil
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
	return filepath.Join(home, ".agents", "skills"), nil
}

func parseConfiguration(contents string) (string, string, *memory.ProviderConfig, error) {
	var fileConfig struct {
		Version string                 `yaml:"version"`
		Library string                 `yaml:"library"`
		Memory  *memory.ProviderConfig `yaml:"memory"`
	}
	if err := yaml.Unmarshal([]byte(contents), &fileConfig); err != nil {
		return "", "", nil, err
	}
	if fileConfig.Version == "" {
		fileConfig.Version = "v1"
	}
	if fileConfig.Version != "v1" {
		return "", "", nil, fmt.Errorf("unsupported configuration version %q", fileConfig.Version)
	}
	if fileConfig.Memory != nil {
		if err := fileConfig.Memory.Validate(); err != nil {
			return "", "", nil, fmt.Errorf("validate memory provider configuration: %w", err)
		}
	}
	return fileConfig.Version, fileConfig.Library, fileConfig.Memory, nil
}
