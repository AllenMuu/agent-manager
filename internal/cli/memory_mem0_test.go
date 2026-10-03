package cli_test

import (
	"encoding/json"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestUnsupportedMem0LifecyclePreviewRejectsBeforeRemoteAccess(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			data, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true})
			t.Setenv("MEM0_UNSUPPORTED_CONFIG", string(data))
			configuration := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(configuration, []byte("memory:\n  version: v1\n  id: shared\n  provider: mem0\n  configuration:\n    kind: env\n    name: MEM0_UNSUPPORTED_CONFIG\n  scopes: [user]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"update", "supersede", "forget"} {
				args := []string{"memory", operation, "neutral-id", "--user", "explicit-user", "--expected-version", "1", "--yes"}
				if operation != "forget" {
					args = append(args, "--type", "FACT", "--content", "inert")
				}
				output, err := executeStructuredMemory(t, configuration, "", args...)
				if err != memory.ErrUnsupported {
					t.Errorf("%s expected unsupported before remote access: %v", operation, err)
				}
				if strings.Contains(output, "Memory mutation plan") {
					t.Errorf("%s exposed an unsupported confirmation plan", operation)
				}
			}
			if requests.Load() != 0 {
				t.Fatalf("unsupported preview issued %d remote requests", requests.Load())
			}
		})
	}
}
