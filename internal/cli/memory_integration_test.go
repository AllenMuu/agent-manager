package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
)

func TestMemoryIntegrationConfigurationContainsOnlySessionAccess(t *testing.T) {
	dir := t.TempDir()
	access := filepath.Join(dir, "access.json")
	os.WriteFile(access, []byte(`{"agent":"codex","owner":{"kind":"PROJECT","projectId":"one-project"},"allowWeakWrites":false}`), 0600)
	command := cli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"memory", "integration-config", "--agent", "codex", "--access", access, "--binary", "/approved/agent-manager"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	quotedAccess, _ := json.Marshal(access)
	want := "[mcp_servers.shared_memory]\ncommand = \"/approved/agent-manager\"\nargs = [\"memory\", \"serve-mcp\", \"--access\", " + string(quotedAccess) + "]\n"
	if output.String() != want {
		t.Fatalf("access-only native config = %q, want %q", output.String(), want)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("private memory copied: %v", entries)
	}
}

func TestPiIntegrationStatusReportsReadOnlyWithoutInventingLiveProof(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider.json")
	os.WriteFile(provider, []byte(`{"endpoint":"http://127.0.0.1:18768","allowNetwork":true,"contract":"mem0-oss-python-2.2.1-94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"}`), 0600)
	access := filepath.Join(root, "access.json")
	b, _ := json.Marshal(map[string]any{"agent": "pi", "owner": map[string]string{"kind": "PROJECT", "projectId": "shared"}, "provider": map[string]any{"version": "v1", "id": "shared", "provider": "mem0", "configuration": map[string]string{"kind": "file", "name": provider}, "scopes": []string{"project"}}})
	os.WriteFile(access, b, 0600)
	command := cli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"memory", "integration-status", "--access", access})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Agent        string `json:"agent"`
		Mechanism    string `json:"mechanism"`
		Write        bool   `json:"write"`
		LiveIdentity string `json:"liveIdentity"`
	}
	json.Unmarshal(output.Bytes(), &status)
	if status.Agent != "pi" || status.Mechanism != "stdio-mcp" || status.Write || status.LiveIdentity != "unverified-client-claims" {
		t.Fatalf("read-only/live status overclaim: %+v", status)
	}
}

func TestPiIntegrationConfigContainsOnlyProjectMCPAccess(t *testing.T) {
	dir := t.TempDir()
	access := filepath.Join(dir, "pi-access.json")
	if err := os.WriteFile(access, []byte(`{"agent":"pi","owner":{"kind":"PROJECT","projectId":"one-project"},"allowWeakWrites":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := cli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"memory", "integration-config", "--agent", "pi", "--access", access, "--binary", "/approved/agent-manager"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]struct {
			Command     string   `json:"command"`
			Args        []string `json:"args"`
			DirectTools []string `json:"directTools"`
			ToolPrefix  string   `json:"toolPrefix"`
			Lifecycle   string   `json:"lifecycle"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatalf("Pi project MCP config is not JSON: %s (%v)", output.String(), err)
	}
	server := config.Servers["shared_memory"]
	if len(config.Servers) != 1 || server.Command != "/approved/agent-manager" || strings.Join(server.Args, " ") != "memory serve-mcp --access "+access {
		t.Fatalf("Pi access-only project config: %+v", server)
	}
	wantTools := []string{"memory_get", "memory_search", "memory_retained_history", "memory_status", "memory_preview_weak", "memory_confirm_weak"}
	if !reflect.DeepEqual(server.DirectTools, wantTools) || server.ToolPrefix != "mcp" || server.Lifecycle != "keep-alive" {
		t.Fatalf("Pi adapter tool configuration: %+v", server)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("configuration copied memory or wrote files: entries=%v err=%v", entries, err)
	}
}

func TestMemoryIntegrationConfigRejectsOutOfScopeClaudeTarget(t *testing.T) {
	command := cli.NewRootCommand()
	command.SetArgs([]string{"memory", "integration-config", "--agent", "claude-code", "--access", "/private/access.json", "--binary", "/approved/agent-manager"})
	if err := command.Execute(); err == nil {
		t.Fatal("Claude configuration unexpectedly included in the Codex/Pi issue scope")
	}
}
