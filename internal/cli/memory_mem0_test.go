package cli_test

import (
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMem0CLIStatusKeepsAuthenticationFailureSafe(t *testing.T) {
	const key = "synthetic-private-mem0-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != key {
			t.Error("missing transport auth")
		}
		w.WriteHeader(401)
		w.Write([]byte(key))
	}))
	defer server.Close()
	t.Setenv("MEM0_CLI_SECRET", key)
	b, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true, "secretReference": map[string]string{"kind": "env", "name": "MEM0_CLI_SECRET"}})
	t.Setenv("MEM0_CLI_CONFIG", string(b))
	configuration := filepath.Join(t.TempDir(), "config.yaml")
	data := "memory:\n  version: v1\n  id: shared\n  provider: mem0\n  configuration:\n    kind: env\n    name: MEM0_CLI_CONFIG\n  scopes: [user, project]\n  capabilities: [read, write, search]\n"
	if err := os.WriteFile(configuration, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configuration, "", "memory", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var status memory.StatusReport
	if err = json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if status.Available || !strings.Contains(out, "authentication") || strings.Contains(out, key) || strings.Contains(out, "MEM0_CLI_SECRET") || strings.Contains(out, "MEM0_CLI_CONFIG") {
		t.Fatalf("unsafe/lost auth status: %s", out)
	}
	_, err = executeStructuredMemory(t, configuration, "", "memory", "add", "--user", "explicit-user", "--type", "FACT", "--content", "inert", "--yes")
	if err != memory.ErrUnsupported {
		t.Fatalf("unsupported confirmed add: %v", err)
	}
}
