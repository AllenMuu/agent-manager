package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeStructuredMemory(t *testing.T, configPath, input string, args ...string) (string, error) {
	t.Helper()
	command := cli.NewAgentManagerCommand()
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(out)
	command.SetIn(strings.NewReader(input))
	command.SetArgs(append([]string{"--config", configPath}, args...))
	_, err := command.ExecuteC()
	return out.String(), err
}

func structuredCLIConfig(t *testing.T) (configuration, root, project string) {
	t.Helper()
	root = t.TempDir()
	project = t.TempDir()
	configuration = filepath.Join(t.TempDir(), "config.yaml")
	data := "memory:\n  version: v1\n  id: local\n  provider: structured-local\n  configuration:\n    kind: file\n    name: " + root + "\n  scopes: [user, project]\n  capabilities: [read, write, search]\n  retrieval:\n    maxResults: 2\n    contentBytes: 100\n    contextBytes: 1000\n"
	if err := os.WriteFile(configuration, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestMemoryStatusSeparatesUnsupportedTextSearchFromRequestedCapabilities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(path, []byte("knowledge"), 0600); err != nil {
		t.Fatal(err)
	}
	configuration := writeMemoryStatusConfig(t, path, "read,search", "project")
	out, err := executeStructuredMemory(t, configuration, "", "memory", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var status memory.StatusReport
	if err = json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.RequestedCapabilities) != 2 || len(status.UnsupportedCapabilities) != 2 || len(status.Capabilities) != 1 || status.Capabilities[0] != memory.CapabilityWrite || strings.Contains(out, path) {
		t.Fatalf("status: %s", out)
	}
}
