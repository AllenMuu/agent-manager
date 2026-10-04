package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryIntegrationConfigurationContainsOnlySessionAccess(t *testing.T) {
	dir := t.TempDir()
	access := filepath.Join(dir, "access.json")
	os.WriteFile(access, []byte(`{"agent":"claude-code","owner":{"kind":"PROJECT","projectId":"one-project"},"allowWeakWrites":false}`), 0600)
	command := cli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"memory", "integration-config", "--agent", "claude-code", "--access", access, "--binary", "/approved/agent-manager"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(output.Bytes(), &config) != nil {
		t.Fatal(output.String())
	}
	server := config.Servers["shared_memory"]
	if server.Command != "/approved/agent-manager" || strings.Join(server.Args, " ") != "memory serve-mcp --access "+access {
		t.Fatalf("access-only native config: %+v", server)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("private memory copied: %v", entries)
	}
}

func TestMemoryIntegrationStatusReportsReadOnlyWithoutInventingLiveProof(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider.json")
	os.WriteFile(provider, []byte(`{"endpoint":"http://127.0.0.1:18768","allowNetwork":true,"contract":"mem0-oss-python-2.2.1-94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"}`), 0600)
	access := filepath.Join(root, "access.json")
	b, _ := json.Marshal(map[string]any{"agent": "claude-code", "owner": map[string]string{"kind": "PROJECT", "projectId": "shared"}, "provider": map[string]any{"version": "v1", "id": "shared", "provider": "mem0", "configuration": map[string]string{"kind": "file", "name": provider}, "scopes": []string{"project"}}})
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
	if status.Agent != "claude-code" || status.Mechanism != "stdio-mcp" || status.Write || status.LiveIdentity != "unverified-client-claims" {
		t.Fatalf("read-only/live status overclaim: %+v", status)
	}
}
