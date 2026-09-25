// Package subagent stores and discovers agent-neutral SubAgent definitions.
package subagent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/resource"
	"gopkg.in/yaml.v3"
)

const (
	// Version is the canonical SubAgent document schema version.
	Version = "v1"
	// DefinitionsDirectory is relative to an Agent Manager data root.
	DefinitionsDirectory = "subagents"
	// maxDefinitionBytes bounds the amount of untrusted YAML read during
	// discovery. Definitions should contain metadata, not an unbounded blob.
	maxDefinitionBytes = 1 << 20
)

// SkillReferenceChecker reports whether an identifier exists in the configured
// Skill catalog. Discovery never opens or executes referenced Skill content.
type SkillReferenceChecker func(identifier string) bool

// Definition is the agent-neutral, versioned SubAgent contract.
type Definition struct {
	Version              string                 `yaml:"version" json:"version"`
	ID                   string                 `yaml:"id" json:"id"`
	Name                 string                 `yaml:"name" json:"name"`
	Role                 string                 `yaml:"role" json:"role"`
	Instructions         string                 `yaml:"instructions" json:"instructions"`
	Skills               []string               `yaml:"skills,omitempty" json:"skills,omitempty"`
	Compatibility        resource.Compatibility `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	RequiredCapabilities []resource.Capability  `yaml:"requiredCapabilities,omitempty" json:"requiredCapabilities,omitempty"`
}

// Diagnostic identifies one definition that cannot be safely registered.
type Diagnostic struct {
	ID      string `json:"id,omitempty" yaml:"id,omitempty"`
	Path    string `json:"path" yaml:"path"`
	Message string `json:"message" yaml:"message"`
}

func (d Diagnostic) Error() string { return fmt.Sprintf("%s: %s", d.Path, d.Message) }

// Registry is a read-only view of canonical definitions below one data root.
type Registry struct {
	root    string
	checker SkillReferenceChecker
}

// DefaultDataRoot returns the machine-local Agent Manager root. It is kept
// separate from target-agent locations and from the shared Skill library.
func DefaultDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for Agent Manager data root: %w", err)
	}
	return filepath.Join(home, ".agents"), nil
}

// NewDefaultRegistry constructs a registry rooted at DefaultDataRoot.
func NewDefaultRegistry(checker SkillReferenceChecker) (Registry, error) {
	root, err := DefaultDataRoot()
	if err != nil {
		return Registry{}, err
	}
	return NewRegistry(root, checker)
}

// NewRegistry configures a registry. The root must be an absolute local path;
// no directories or files are created by this function.
func NewRegistry(root string, checker SkillReferenceChecker) (Registry, error) {
	if root == "" {
		return Registry{}, errors.New("Agent Manager data root must not be empty")
	}
	if !filepath.IsAbs(root) {
		return Registry{}, fmt.Errorf("Agent Manager data root must be absolute: %q", root)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Registry{}, fmt.Errorf("resolve Agent Manager data root: %w", err)
	}
	return Registry{root: filepath.Clean(absRoot), checker: checker}, nil
}

// Root returns the configured, cleaned data root.
func (r Registry) Root() string { return r.root }

// SkillReferenceChecker returns the validated Skill-registry boundary used by
// mutation workflows. Callers must pass it to guarded SubAgent installation;
// render-only inspection intentionally remains independent of the catalog.
func (r Registry) SkillReferenceChecker() SkillReferenceChecker { return r.checker }

// Discover reads YAML definitions only. Invalid files are omitted and returned
// as actionable diagnostics; a missing definitions directory means empty.
func (r Registry) Discover() ([]Definition, []Diagnostic, error) {
	dir := filepath.Join(r.root, DefinitionsDirectory)
	dirHandle, err := openDefinitionsDirectory(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Definition{}, nil, nil
		}
		return nil, nil, err
	}
	if dirHandle == nil {
		return []Definition{}, nil, nil
	}
	defer dirHandle.Close()
	dirInfo, err := dirHandle.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect SubAgent definitions directory: %w", err)
	}
	entries, err := dirHandle.ReadDir(-1)
	if err != nil {
		return nil, nil, fmt.Errorf("read SubAgent definitions: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	type discovered struct {
		definition Definition
		path       string
	}
	var discoveredDefinitions []discovered
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if !isDefinitionFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		definition, err := readDefinition(path, dirInfo)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{ID: definition.ID, Path: path, Message: err.Error()})
			continue
		}
		if err := definition.Validate(r.checker); err != nil {
			diagnostics = append(diagnostics, Diagnostic{ID: definition.ID, Path: path, Message: err.Error()})
			continue
		}
		discoveredDefinitions = append(discoveredDefinitions, discovered{definition: definition, path: path})
	}
	byID := make(map[string][]discovered, len(discoveredDefinitions))
	for _, item := range discoveredDefinitions {
		byID[item.definition.ID] = append(byID[item.definition.ID], item)
	}
	var definitions []Definition
	for _, item := range discoveredDefinitions {
		duplicates := byID[item.definition.ID]
		if len(duplicates) > 1 {
			others := make([]string, 0, len(duplicates)-1)
			for _, other := range duplicates {
				if other.path != item.path {
					others = append(others, other.path)
				}
			}
			diagnostics = append(diagnostics, Diagnostic{ID: item.definition.ID, Path: item.path, Message: fmt.Sprintf("duplicate id %q; also defined in %s", item.definition.ID, strings.Join(others, ", "))})
			continue
		}
		definitions = append(definitions, item.definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].Path < diagnostics[j].Path })
	return definitions, diagnostics, nil
}

func readDefinition(path string, parent os.FileInfo) (Definition, error) {
	contents, err := readRegularDefinition(path, parent)
	if err != nil {
		return Definition{}, err
	}
	var definition Definition
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return definition, fmt.Errorf("parse canonical v1 definition: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return definition, errors.New("parse canonical v1 definition: multiple YAML documents are not allowed")
		}
		return definition, fmt.Errorf("parse canonical v1 definition: trailing YAML document: %w", err)
	}
	return definition, nil
}

// Validate checks schema fields and referenced Skills without mutating state.
func (d Definition) Validate(checker SkillReferenceChecker) error {
	if d.Version != Version {
		return fmt.Errorf("unsupported version %q; expected %s", d.Version, Version)
	}
	if !validIdentifier(d.ID) {
		return fmt.Errorf("invalid id %q; use lowercase letters, digits, '.', '_' or '-'", d.ID)
	}
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(d.Role) == "" {
		return errors.New("role is required")
	}
	if strings.TrimSpace(d.Instructions) == "" {
		return errors.New("instructions are required")
	}
	for _, skill := range d.Skills {
		if !validIdentifier(skill) {
			return fmt.Errorf("invalid Skill reference %q", skill)
		}
		if checker == nil {
			return fmt.Errorf("cannot verify referenced Skill %q: no Skill catalog checker configured", skill)
		}
		if !checker(skill) {
			return fmt.Errorf("referenced Skill %q was not found in the configured Skill catalog", skill)
		}
	}
	for _, capability := range d.RequiredCapabilities {
		contract := resource.ManagedResource{Version: Version, ID: d.ID, Kind: resource.SubAgent, RequiredCapabilities: []resource.Capability{capability}}
		if err := contract.Validate(); err != nil {
			return fmt.Errorf("required capability %q is invalid: %w", capability, err)
		}
	}
	for _, agent := range d.Compatibility.Agents {
		if !validIdentifier(agent) {
			return fmt.Errorf("invalid compatibility agent %q", agent)
		}
	}
	return nil
}

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func validIdentifier(value string) bool {
	return value != "" && value != "." && value != ".." && identifierPattern.MatchString(value)
}

func isDefinitionFile(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
