package adapter

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

// ErrSubAgentUnsupported means that a target-specific preview cannot
// represent every canonical SubAgent field or required capability. The
// returned plan is diagnostic-only and must not be passed to a mutating
// client.
var ErrSubAgentUnsupported = errors.New("SubAgent representation is unsupported")

// SubAgentScope selects the native project or user-level agent directory.
type SubAgentScope string

const (
	SubAgentProject SubAgentScope = "project"
	SubAgentGlobal  SubAgentScope = "global"
)

// SubAgentRequest supplies the already-resolved root for a preview. Root is a
// project directory for project scope and a home directory for global scope.
// Adapters only derive a destination; they never create or modify it.
type SubAgentRequest struct {
	Root  string
	Scope SubAgentScope
}

// SubAgentInspection reports target compatibility without rendering or
// writing a native file.
type SubAgentInspection struct {
	Target                  Target
	Definition              subagent.Definition
	Destination             string
	Format                  string
	Supported               bool
	UnsupportedFields       []string
	UnsupportedCapabilities []resource.Capability
}

// SubAgentPlan is a render-only target-specific preview. Content is returned
// to the caller for display or later guarded publication and is never written
// by this package.
type SubAgentPlan struct {
	Target                  Target
	Definition              subagent.Definition
	Destination             string
	Format                  string
	Content                 string
	UnsupportedFields       []string
	UnsupportedCapabilities []resource.Capability
}

// SubAgentAdapter exposes the target-specific SubAgent inspection seam.
type SubAgentAdapter interface {
	AgentAdapter
	InspectSubAgent(subagent.Definition, SubAgentRequest) (SubAgentInspection, error)
	PlanSubAgent(subagent.Definition, SubAgentRequest) (SubAgentPlan, error)
}

// Supported reports whether a render plan is complete and safe to publish.
func (p SubAgentPlan) Supported() bool {
	return p.Destination != "" && p.Content != "" && len(p.UnsupportedFields) == 0 && len(p.UnsupportedCapabilities) == 0
}

func (a directoryAdapter) InspectSubAgent(definition subagent.Definition, request SubAgentRequest) (SubAgentInspection, error) {
	if err := validateSubAgentDefinition(definition); err != nil {
		return SubAgentInspection{}, err
	}
	plan := a.subAgentPlan(definition, request)
	inspection := SubAgentInspection{
		Target: a.target, Definition: definition, Destination: plan.Destination,
		Format: plan.Format, Supported: len(plan.UnsupportedFields) == 0 && len(plan.UnsupportedCapabilities) == 0,
		UnsupportedFields:       append([]string(nil), plan.UnsupportedFields...),
		UnsupportedCapabilities: append([]resource.Capability(nil), plan.UnsupportedCapabilities...),
	}
	return inspection, nil
}

func (a directoryAdapter) PlanSubAgent(definition subagent.Definition, request SubAgentRequest) (SubAgentPlan, error) {
	if err := validateSubAgentDefinition(definition); err != nil {
		return SubAgentPlan{}, err
	}
	plan := a.subAgentPlan(definition, request)
	if !planSupported(plan) {
		return plan, fmt.Errorf("%w for %s: unsupported fields=%v capabilities=%v", ErrSubAgentUnsupported, a.target, plan.UnsupportedFields, plan.UnsupportedCapabilities)
	}
	return plan, nil
}

func (a directoryAdapter) subAgentPlan(definition subagent.Definition, request SubAgentRequest) SubAgentPlan {
	plan := SubAgentPlan{Target: a.target, Definition: definition}
	if request.Root == "" || !filepath.IsAbs(request.Root) {
		plan.UnsupportedFields = []string{"placement root"}
		return plan
	}
	if request.Scope != "" && request.Scope != SubAgentProject && request.Scope != SubAgentGlobal {
		plan.UnsupportedFields = []string{"placement scope"}
		return plan
	}
	base := filepath.Clean(request.Root)
	switch a.target {
	case ClaudeCode:
		plan.Format = "claude-code-markdown"
		plan.Destination = filepath.Join(base, ".claude", "agents", definition.ID+".md")
		plan.Content = renderClaudeSubAgent(definition)
	case Codex:
		plan.Format = "codex-toml"
		plan.Destination = filepath.Join(base, ".codex", "agents", definition.ID+".toml")
		var err error
		plan.Content, err = renderCodexSubAgent(definition)
		if err != nil {
			plan.UnsupportedFields = []string{"rendered content: " + err.Error()}
		}
	case Pi:
		// Pi's documented native locations contain Skills, settings, and
		// context files, but no native SubAgent definition format. Never guess
		// a write location or claim semantic support.
		plan.UnsupportedFields = []string{"id", "name", "role", "instructions", "skills", "compatibility", "requiredCapabilities"}
	}
	if len(definition.Compatibility.Agents) > 0 && !containsTarget(definition.Compatibility.Agents, a.target) {
		plan.UnsupportedFields = append(plan.UnsupportedFields, "compatibility: target is not declared compatible")
	}
	plan.UnsupportedCapabilities = unsupportedSubAgentCapabilities(a, definition.RequiredCapabilities)
	return plan
}

func validateSubAgentDefinition(definition subagent.Definition) error {
	return definition.Validate(func(string) bool { return true })
}

func unsupportedSubAgentCapabilities(a directoryAdapter, required []resource.Capability) []resource.Capability {
	return resource.MissingCapabilities(required, a.Capabilities(resource.SubAgent))
}

func planSupported(plan SubAgentPlan) bool {
	return plan.Destination != "" && plan.Content != "" && len(plan.UnsupportedFields) == 0 && len(plan.UnsupportedCapabilities) == 0
}

func renderClaudeSubAgent(definition subagent.Definition) string {
	description := definition.Name
	if definition.Role != "" {
		description += " — " + definition.Role
	}
	return "---\nname: " + definition.ID + "\ndescription: " + strconv.Quote(description) + "\n---\n\n" + renderedInstructions(definition) + "\n"
}

func renderCodexSubAgent(definition subagent.Definition) (string, error) {
	description := definition.Name
	if definition.Role != "" {
		description += " — " + definition.Role
	}
	id, err := tomlQuote(definition.ID)
	if err != nil {
		return "", fmt.Errorf("id: %w", err)
	}
	desc, err := tomlQuote(description)
	if err != nil {
		return "", fmt.Errorf("description: %w", err)
	}
	instructions, err := tomlQuote(renderedInstructions(definition))
	if err != nil {
		return "", fmt.Errorf("developer_instructions: %w", err)
	}
	return "name = " + id + "\ndescription = " + desc + "\ndeveloper_instructions = " + instructions + "\n", nil
}

func containsTarget(targets []string, target Target) bool {
	for _, value := range targets {
		if value == string(target) {
			return true
		}
	}
	return false
}

func renderedInstructions(definition subagent.Definition) string {
	text := definition.Instructions
	if len(definition.Skills) > 0 {
		text += "\n\nManaged Skills: " + strings.Join(definition.Skills, ", ")
	}
	if len(definition.Compatibility.Agents) > 0 {
		text += "\nCompatible agents: " + strings.Join(definition.Compatibility.Agents, ", ")
	}
	return text
}

// tomlQuote emits a TOML basic string. strconv.Quote is not suitable because
// it can emit Go-only escapes such as \\a and \\xNN.
func tomlQuote(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("invalid UTF-8")
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

// InspectSubAgent is a convenience for a project-scoped read-only preview.
func InspectSubAgent(target Target, definition subagent.Definition, project string) (SubAgentInspection, error) {
	a, ok := ForAgent(target)
	if !ok {
		return SubAgentInspection{}, fmt.Errorf("unsupported target %q", target)
	}
	return a.InspectSubAgent(definition, SubAgentRequest{Root: project, Scope: SubAgentProject})
}

// PlanSubAgent is a convenience for a project-scoped render preview.
func PlanSubAgent(target Target, definition subagent.Definition, project string) (SubAgentPlan, error) {
	a, ok := ForAgent(target)
	if !ok {
		return SubAgentPlan{}, fmt.Errorf("unsupported target %q", target)
	}
	return a.PlanSubAgent(definition, SubAgentRequest{Root: project, Scope: SubAgentProject})
}
