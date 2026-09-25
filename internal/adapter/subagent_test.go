package adapter_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
	"gopkg.in/yaml.v3"
)

func TestClaudeCodeSubAgentPlanRendersCanonicalFieldsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{
		Version: subagent.Version, ID: "reviewer", Name: "Code Reviewer", Role: "Reviews changes",
		Instructions: "Review the diff.", Skills: []string{"go-helper"},
		Compatibility:        resource.Compatibility{Agents: []string{"claude-code"}},
		RequiredCapabilities: []resource.Capability{resource.CapabilityFilesystemWrite},
	}
	a, ok := adapter.ForAgent(adapter.ClaudeCode)
	if !ok {
		t.Fatal("Claude Code adapter is unavailable")
	}
	plan, err := a.PlanSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject})
	if err != nil {
		t.Fatalf("PlanSubAgent() error = %v", err)
	}
	if plan.Destination != filepath.Join(root, ".claude", "agents", "reviewer.md") || plan.Format != "claude-code-markdown" {
		t.Fatalf("plan location = %#v", plan)
	}
	if !strings.Contains(plan.Content, "name: reviewer") || !strings.Contains(plan.Content, "Review the diff.") || !strings.Contains(plan.Content, "go-helper") || !strings.Contains(plan.Content, "claude-code") {
		t.Fatalf("rendered plan dropped canonical details: %q", plan.Content)
	}
	parts := strings.SplitN(plan.Content, "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("rendered plan has no YAML frontmatter: %q", plan.Content)
	}
	var frontmatter struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil || frontmatter.Name != definition.ID {
		t.Fatalf("frontmatter = %#v, error = %v", frontmatter, err)
	}
	if _, err := os.Lstat(plan.Destination); !os.IsNotExist(err) {
		t.Fatalf("inspection/planning wrote %q: %v", plan.Destination, err)
	}
}

func TestCodexSubAgentPlanRendersNativeTOMLAndSurfacesUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{
		Version: subagent.Version, ID: "reviewer", Name: "Code Reviewer", Role: "Reviews changes",
		Instructions: "Review the diff.", Skills: []string{"go-helper"},
		Compatibility:        resource.Compatibility{Agents: []string{"codex"}},
		RequiredCapabilities: []resource.Capability{resource.CapabilityMemorySearch},
	}
	a, ok := adapter.ForAgent(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter is unavailable")
	}
	plan, err := a.PlanSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject})
	if !errors.Is(err, adapter.ErrSubAgentUnsupported) {
		t.Fatalf("PlanSubAgent() error = %v, want explicit unsupported result", err)
	}
	if plan.Destination != filepath.Join(root, ".codex", "agents", "reviewer.toml") || plan.Format != "codex-toml" {
		t.Fatalf("plan location = %#v", plan)
	}
	if !strings.Contains(plan.Content, `name = "reviewer"`) || !strings.Contains(plan.Content, "developer_instructions") || !strings.Contains(plan.Content, "go-helper") {
		t.Fatalf("rendered plan = %q", plan.Content)
	}
	if len(plan.UnsupportedFields) != 0 || !containsCapability(plan.UnsupportedCapabilities, resource.CapabilityMemorySearch) {
		t.Fatalf("unsupported details = %#v", plan)
	}
	if _, err := os.Lstat(plan.Destination); !os.IsNotExist(err) {
		t.Fatalf("inspection/planning wrote %q: %v", plan.Destination, err)
	}
}

func TestSubAgentInspectionSurfacesUndeclaredCompatibilityTarget(t *testing.T) {
	definition := subagent.Definition{
		Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review.",
		Compatibility: resource.Compatibility{Agents: []string{"codex"}},
	}
	a, ok := adapter.ForAgent(adapter.ClaudeCode)
	if !ok {
		t.Fatal("Claude Code adapter is unavailable")
	}
	inspection, err := a.InspectSubAgent(definition, adapter.SubAgentRequest{Root: t.TempDir(), Scope: adapter.SubAgentProject})
	if err != nil {
		t.Fatalf("InspectSubAgent() error = %v", err)
	}
	if inspection.Supported || !containsString(inspection.UnsupportedFields, "compatibility: target is not declared compatible") {
		t.Fatalf("inspection = %#v, want explicit compatibility warning", inspection)
	}
}

func TestPiSubAgentPlanHasNoPlacementWhenTargetCannotRepresentSubAgents(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review."}
	a, ok := adapter.ForAgent(adapter.Pi)
	if !ok {
		t.Fatal("Pi adapter is unavailable")
	}
	plan, err := a.PlanSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject})
	if !errors.Is(err, adapter.ErrSubAgentUnsupported) {
		t.Fatalf("PlanSubAgent() error = %v, want explicit unsupported result", err)
	}
	if plan.Destination != "" || plan.Content != "" || plan.Format != "" {
		t.Fatalf("Pi plan implies an unsupported native write: %#v", plan)
	}
	for _, field := range []string{"id", "name", "role", "instructions", "skills", "compatibility", "requiredCapabilities"} {
		if !containsString(plan.UnsupportedFields, field) {
			t.Errorf("Pi unsupported fields = %v, missing %q", plan.UnsupportedFields, field)
		}
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("Pi inspection/planning changed root: entries=%v error=%v", entries, readErr)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsCapability(values []resource.Capability, want resource.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
