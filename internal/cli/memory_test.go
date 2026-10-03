package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
)

type memoryStatusJSON struct {
	Configured   bool                    `json:"configured"`
	Provider     string                  `json:"provider"`
	State        string                  `json:"state"`
	Available    bool                    `json:"available"`
	Reason       string                  `json:"reason"`
	Capabilities []string                `json:"capabilities"`
	Scopes       []string                `json:"scopes"`
	Agents       []memoryAgentStatusJSON `json:"agents"`
}

type memoryAgentStatusJSON struct {
	Agent                   string   `json:"agent"`
	State                   string   `json:"state"`
	Reason                  string   `json:"reason"`
	Capabilities            []string `json:"capabilities"`
	Scopes                  []string `json:"scopes"`
	UnsupportedCapabilities []string `json:"unsupportedCapabilities"`
	UnavailableCapabilities []string `json:"unavailableCapabilities"`
	UnsupportedScopes       []string `json:"unsupportedScopes"`
	UnavailableScopes       []string `json:"unavailableScopes"`
}

func TestMemoryStatusJSONReportsConfiguredAvailableProviderAndAgentMappings(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte(`{"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusConfig(t, store, "read,search", "user,project")

	output := executeMemoryStatus(t, configPath, "--json")
	var status memoryStatusJSON
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status: %v; output=%q", err, output)
	}
	if !status.Configured || status.Provider != "file" || status.State != "available" || !status.Available {
		t.Fatalf("status = %#v, want configured available file provider", status)
	}
	if strings.Contains(output, store) {
		t.Fatalf("status leaked provider reference %q: %s", store, output)
	}
	if len(status.Capabilities) != 1 || status.Capabilities[0] != "write" || len(status.Scopes) != 2 {
		t.Fatalf("provider mapping = %#v, want implemented append and user/project; text read/search are requests", status)
	}
	if agent := findMemoryAgent(status.Agents, "codex"); agent == nil || agent.State != "unsupported" || len(agent.Capabilities) != 0 || len(agent.Scopes) != 0 {
		t.Fatalf("codex mapping = %#v, want unsupported with no access", agent)
	}
}

func TestMemoryStatusJSONReportsConfiguredUnavailableProviderWithReason(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-memory.json")
	configPath := writeMemoryStatusConfig(t, missing, "read", "user")

	output := executeMemoryStatus(t, configPath, "--json")
	var status memoryStatusJSON
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status: %v; output=%q", err, output)
	}
	if !status.Configured || status.State != "unavailable" || status.Available {
		t.Fatalf("status = %#v, want configured unavailable provider", status)
	}
	if status.Reason == "" || !strings.Contains(strings.ToLower(status.Reason), "available") {
		t.Fatalf("status reason = %q, want actionable availability reason", status.Reason)
	}
	if strings.Contains(output, missing) {
		t.Fatalf("status leaked provider reference %q: %s", missing, output)
	}
	if agent := findMemoryAgent(status.Agents, "claude-code"); agent == nil || agent.State != "unavailable" {
		t.Fatalf("claude-code mapping = %#v, want unavailable", agent)
	}
}

func TestMemoryStatusJSONReportsUnsupportedConfiguredProvider(t *testing.T) {
	configPath := writeMemoryStatusProviderConfig(t, "graphiti", "GRAPHITI_URL", "read", "user")

	output := executeMemoryStatus(t, configPath, "--json")
	var status memoryStatusJSON
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status: %v; output=%q", err, output)
	}
	if !status.Configured || status.Provider != "graphiti" || status.State != "unsupported" || status.Available {
		t.Fatalf("status = %#v, want configured unsupported provider", status)
	}
	if status.Reason == "" || !strings.Contains(strings.ToLower(status.Reason), "supported") {
		t.Fatalf("status reason = %q, want supported-provider guidance", status.Reason)
	}
}

func TestMemoryStatusJSONReportsUnconfiguredProvider(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: /tmp/skills\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeMemoryStatus(t, configPath, "--json")
	var status memoryStatusJSON
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status: %v; output=%q", err, output)
	}
	if status.Configured || status.State != "unconfigured" || status.Available {
		t.Fatalf("status = %#v, want unconfigured provider", status)
	}
	if status.Reason == "" || !strings.Contains(strings.ToLower(status.Reason), "config") {
		t.Fatalf("status reason = %q, want configuration guidance", status.Reason)
	}
}

func TestMemoryStatusHumanReportsUnsupportedAgentLocation(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte(`{"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusConfig(t, store, "read", "user")
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".unsupported-agent", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}

	output := executeMemoryStatus(t, configPath, "--project", project)
	if !strings.Contains(output, "Memory provider: file") || !strings.Contains(output, "State: available") {
		t.Fatalf("human status = %q, want provider and state", output)
	}
	if !strings.Contains(output, "unsupported-agent") || !strings.Contains(output, "unsupported") {
		t.Fatalf("human status = %q, want unsupported agent mapping", output)
	}
	if strings.Contains(output, store) {
		t.Fatalf("human status leaked provider reference %q: %s", store, output)
	}
}

func TestSkillManagerAliasExposesMemoryStatus(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: /tmp/skills\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := cli.NewSkillManagerCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"--config", configPath, "memory", "status", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skill-manager memory status: %v; output=%q", err, output.String())
	}
	if !strings.Contains(output.String(), `"state":"unconfigured"`) {
		t.Fatalf("alias output = %q, want unconfigured status", output.String())
	}
}

func TestMemoryPromoteRequiresConfirmationAndDoesNotLeakKnowledge(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusProviderConfigWithCapabilities(t, "file", store, "write", "user")

	output, err := executeMemoryPromote(t, configPath, "n\n", "user", "secret knowledge", false)
	if err != nil {
		t.Fatalf("memory promote refusal: %v; output=%q", err, output)
	}
	if strings.Contains(output, "secret knowledge") || strings.Contains(output, store) {
		t.Fatalf("promotion output leaked secret or provider reference: %q", output)
	}
	contents, readErr := os.ReadFile(store)
	if readErr != nil || string(contents) != "existing\n" {
		t.Fatalf("refused promotion changed store: %q, %v", contents, readErr)
	}
}

func TestMemoryPromoteWithYesAppendsKnowledge(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusProviderConfigWithCapabilities(t, "file", store, "write", "user")

	output, err := executeMemoryPromote(t, configPath, "", "user", "secret knowledge", true)
	if err != nil {
		t.Fatalf("memory promote: %v; output=%q", err, output)
	}
	if strings.Contains(output, "secret knowledge") || strings.Contains(output, store) {
		t.Fatalf("promotion output leaked secret or provider reference: %q", output)
	}
	contents, readErr := os.ReadFile(store)
	if readErr != nil || string(contents) != "existing\nsecret knowledge\n" {
		t.Fatalf("successful promotion store = %q, %v", contents, readErr)
	}
}

func TestMemoryPromoteRefusesUnsupportedScopeAndCapabilityWithoutMutation(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(store, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusProviderConfigWithCapabilities(t, "file", store, "read", "user")

	for _, tc := range []struct {
		name  string
		scope string
		want  string
	}{
		{name: "scope", scope: "project", want: "scope"},
		{name: "write capability", scope: "user", want: "write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := executeMemoryPromote(t, configPath, "", tc.scope, "knowledge", true)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error = %v, output=%q; want %q refusal", err, output, tc.want)
			}
			if strings.Contains(output, "knowledge") || strings.Contains(output, store) {
				t.Fatalf("refusal output leaked secret or provider reference: %q", output)
			}
		})
	}
	contents, err := os.ReadFile(store)
	if err != nil || string(contents) != "existing\n" {
		t.Fatalf("refused promotions changed store: %q, %v", contents, err)
	}
}

func TestMemoryStatusDoesNotImplicitlyPromote(t *testing.T) {
	store := filepath.Join(t.TempDir(), "memory.json")
	original := []byte("existing\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryStatusProviderConfigWithCapabilities(t, "file", store, "write", "user")
	_ = executeMemoryStatus(t, configPath, "--json")
	contents, err := os.ReadFile(store)
	if err != nil || string(contents) != string(original) {
		t.Fatalf("status changed provider store: %q, %v", contents, err)
	}
}

func TestMemoryProviderRemainsReadOnlyAcrossOrdinaryResourceAndSubAgentWorkflows(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "demo", "Demo", "demo skill", "tag\n")
	dataRoot := t.TempDir()
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review changes
instructions: Review the diff.
`)
	project := t.TempDir()
	store := filepath.Join(t.TempDir(), "memory-store")
	original := []byte("existing knowledge\n")
	if err := os.WriteFile(store, original, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeMemoryFixtureConfig(t, library, store, "read,write,search", "user,project")

	workflows := []struct {
		name string
		args []string
	}{
		{name: "memory status", args: []string{"memory", "status", "--json", "--project", project}},
		{name: "skill search", args: []string{"search", "demo"}},
		{name: "skill list", args: []string{"list", "--project", project, "--json"}},
		{name: "skill activation", args: []string{"add", "demo", "--project", project, "--target", "codex", "--yes"}},
		{name: "subagent list", args: []string{"subagents", "list", "--root", dataRoot, "--library", library, "--json"}},
		{name: "subagent installation", args: []string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"}},
	}
	for _, workflow := range workflows {
		t.Run(workflow.name, func(t *testing.T) {
			command := cli.NewAgentManagerCommand()
			output := &bytes.Buffer{}
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs(append([]string{"--config", configPath}, workflow.args...))
			if err := command.Execute(); err != nil {
				t.Fatalf("workflow failed: %v; output=%q", err, output.String())
			}
			contents, err := os.ReadFile(store)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != string(original) {
				t.Fatalf("workflow changed provider store: %q; want %q", contents, original)
			}
		})
	}
}

func executeMemoryStatus(t *testing.T, configPath string, args ...string) string {
	t.Helper()
	root := cli.NewAgentManagerCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"--config", configPath, "memory", "status"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("memory status: %v; output=%q", err, output.String())
	}
	return output.String()
}

func writeMemoryStatusConfig(t *testing.T, store string, capabilities, scopes string) string {
	t.Helper()
	return writeMemoryStatusProviderConfigWithCapabilities(t, "file", store, capabilities, scopes)
}

func writeMemoryStatusProviderConfig(t *testing.T, provider, reference, capabilities, scopes string) string {
	t.Helper()
	return writeMemoryStatusProviderConfigWithCapabilities(t, provider, reference, capabilities, scopes)
}

func writeMemoryStatusProviderConfigWithCapabilities(t *testing.T, provider, reference, capabilities, scopes string) string {
	t.Helper()
	return writeMemoryProviderConfig(t, "/tmp/skills", provider, reference, capabilities, scopes)
}

func writeMemoryFixtureConfig(t *testing.T, library, reference, capabilities, scopes string) string {
	t.Helper()
	return writeMemoryProviderConfig(t, library, "file", reference, capabilities, scopes)
}

func writeMemoryProviderConfig(t *testing.T, library, provider, reference, capabilities, scopes string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	kind := "file"
	if provider != "file" {
		kind = "env"
	}
	contents := "library: " + library + "\nmemory:\n  version: v1\n  id: local-memory\n  provider: " + provider + "\n  configuration:\n    kind: " + kind + "\n    name: " + reference + "\n  capabilities: [" + capabilities + "]\n  scopes: [" + scopes + "]\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeMemoryPromote(t *testing.T, configPath, input, scope, knowledge string, yes bool) (string, error) {
	t.Helper()
	root := cli.NewAgentManagerCommand()
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetIn(strings.NewReader(input))
	args := []string{"--config", configPath, "memory", "promote", "--scope", scope, "--knowledge", knowledge}
	if yes {
		args = append(args, "--yes")
	}
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func findMemoryAgent(agents []memoryAgentStatusJSON, id string) *memoryAgentStatusJSON {
	for i := range agents {
		if agents[i].Agent == id {
			return &agents[i]
		}
	}
	return nil
}
