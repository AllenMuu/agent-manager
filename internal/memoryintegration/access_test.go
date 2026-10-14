package memoryintegration_test

import (
	"context"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTrustedAccessResolvesPrivateFileSecretOnlyAfterAuthorizedRead(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-API-Key") != "synthetic-key" {
			t.Error("missing server-only secret")
		}
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	root := t.TempDir()
	secret := filepath.Join(root, "private-key")
	os.WriteFile(secret, []byte("synthetic-key"), 0600)
	external := filepath.Join(root, "provider.json")
	b, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true, "secretReference": map[string]any{"kind": "file", "name": secret}})
	os.WriteFile(external, b, 0600)
	access := filepath.Join(root, "access.json")
	b, _ = json.Marshal(map[string]any{"agent": "codex", "owner": map[string]string{"kind": "PROJECT", "projectId": "one-project"}, "provider": map[string]any{"version": "v1", "id": "shared", "provider": "mem0", "configuration": map[string]string{"kind": "file", "name": external}, "scopes": []string{"project"}}})
	os.WriteFile(access, b, 0600)
	c, err := memoryintegration.LoadAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.Gateway()
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("configuration resolved server access")
	}
	foreign := memory.Owner{Kind: memory.OwnerProject, ProjectID: "foreign"}
	if _, err = g.Search(context.Background(), memory.SearchRequest{Owner: foreign}); err != memory.ErrOwnershipDenied || requests.Load() != 0 {
		t.Fatalf("foreign access reached credentials/provider: %v", err)
	}
	if _, err = g.Search(context.Background(), memory.SearchRequest{Owner: c.Owner, Text: "constraint"}); err != nil {
		t.Fatal(err)
	}
	os.Remove(secret)
	os.Symlink(external, secret)
	_, err = g.Search(context.Background(), memory.SearchRequest{Owner: c.Owner, Text: "constraint"})
	if err != memory.ErrAuthentication {
		t.Fatalf("symlink credential accepted: %v", err)
	}
	if strings.Contains(memory.SafeError(err).Error(), secret) {
		t.Fatal("private reference escaped")
	}
}

func TestLoadAccessAcceptsPiWithExplicitProjectOwner(t *testing.T) {
	root := t.TempDir()
	access := filepath.Join(root, "pi-access.json")
	data := []byte(`{"agent":"pi","owner":{"kind":"PROJECT","projectId":"shared-project"},"allowWeakWrites":true,"provider":{"version":"v1","id":"shared","provider":"mem0","configuration":{"kind":"file","name":"/private/provider.json"},"scopes":["project"]}}`)
	if err := os.WriteFile(access, data, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := memoryintegration.LoadAccess(access)
	if err != nil {
		t.Fatalf("Pi project access rejected: %v", err)
	}
	if config.Agent != "pi" || config.Owner.ProjectID != "shared-project" || !config.AllowWeakWrites {
		t.Fatalf("Pi access was changed: %+v", config.SessionConfig)
	}
}

func TestLoadAccessRejectsClaudeOutsideIssue23Scope(t *testing.T) {
	root := t.TempDir()
	access := filepath.Join(root, "claude-access.json")
	data := []byte(`{"agent":"claude-code","owner":{"kind":"PROJECT","projectId":"shared-project"},"provider":{"version":"v1","id":"shared","provider":"mem0","configuration":{"kind":"file","name":"/private/provider.json"},"scopes":["project"]}}`)
	if err := os.WriteFile(access, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := memoryintegration.LoadAccess(access); err == nil {
		t.Fatal("Claude access unexpectedly included in the Codex/Pi issue scope")
	}
}
