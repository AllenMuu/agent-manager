package memoryprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"github.com/AllenMuu/skill-manager/internal/memoryprovider"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExplicitMem0SelectionReportsHonestGatewayCapabilities(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.Write([]byte(`{"results":[]}`)) }))
	defer server.Close()
	b, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true, "timeoutMilliseconds": 1000})
	t.Setenv("AM_MEM0_CONFIG", string(b))
	c := memory.ProviderConfig{Version: "v1", ID: "shared", Provider: "mem0", Configuration: memory.ConfigReference{Kind: "env", Name: "AM_MEM0_CONFIG"}, Scopes: []memory.Scope{memory.ScopeProject}, Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilityWrite, memory.CapabilitySearch}}
	p, err := memoryprovider.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatal("construction must be read-free")
	}
	if !p.Capabilities().Remember || !p.Capabilities().BasicReplace {
		t.Fatal("basic provider missing")
	}
	status, err := memoryprovider.Discover(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	caps := status.StructuredCapabilities
	if !status.Available || caps == nil || !caps.Get || !caps.ScoredRecall || caps.Remember || caps.Update || caps.Forget || caps.BasicReplace || caps.BasicRemove {
		t.Fatalf("Gateway status overclaimed: %+v", status)
	}
	t.Setenv("AM_MEM0_CONFIG", `{"allowNetwork":false}`)
	if _, err = memoryprovider.Open(c); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("network opt-in: %v", err)
	}
}

func TestMem0ConfigurationRejectsRawSecretsAndUnsafeReferences(t *testing.T) {
	c := memory.ProviderConfig{Version: "v1", ID: "shared", Provider: "mem0", Configuration: memory.ConfigReference{Kind: "env", Name: "AM_MEM0_CONFIG"}, Scopes: []memory.Scope{memory.ScopeUser}}
	for _, data := range []string{
		`{"endpoint":"https://example.com","allowNetwork":true,"apiKey":"never-persist"}`,
		`{"endpoint":"https://example.com","allowNetwork":true,"secretReference":{"kind":"env","name":"bad variable"}}`,
	} {
		var fields map[string]any
		json.Unmarshal([]byte(data), &fields)
		fields["contract"] = mem0.ContractVersion
		b, _ := json.Marshal(fields)
		t.Setenv("AM_MEM0_CONFIG", string(b))
		if _, err := memoryprovider.Open(c); err != memory.ErrInvalidInput {
			t.Fatalf("unsafe configuration accepted/category lost: %v", err)
		}
	}
}

func TestNonSecretFileConfigurationAndJournalEvidence(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "mem0.json")
	data, _ := json.Marshal(map[string]any{"endpoint": "https://mem0.example.test", "contract": mem0.ContractVersion, "allowNetwork": true, "secretReference": map[string]string{"kind": "env", "name": "OPAQUE_MEM0_SECRET"}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	c := memory.ProviderConfig{Version: "v1", ID: "shared", Provider: "mem0", Configuration: memory.ConfigReference{Kind: "file", Name: path}, Scopes: []memory.Scope{memory.ScopeUser}}
	if _, err := memoryprovider.Open(c); err != nil {
		t.Fatal(err)
	}
	if evidence := c.JournalEvidence(); strings.Contains(evidence, path) || strings.Contains(evidence, "OPAQUE_MEM0_SECRET") {
		t.Fatalf("private reference persisted: %s", evidence)
	}
	if _, err := memoryprovider.Open(memory.ProviderConfig{Version: "v1", ID: "shared", Provider: "mem0", Configuration: memory.ConfigReference{Kind: "file", Name: directory}, Scopes: []memory.Scope{memory.ScopeUser}}); err != memory.ErrInvalidInput {
		t.Fatalf("nonregular config accepted: %v", err)
	}
}

func TestExternalMem0ConfigurationRejectsNestedUnknownFieldsBeforeAccess(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	t.Setenv("OPAQUE_MEM0_SECRET", "synthetic-not-resolved-at-construction")
	c := memory.ProviderConfig{Version: "v1", ID: "shared", Provider: "mem0", Configuration: memory.ConfigReference{Kind: "env", Name: "AM_MEM0_CONFIG"}, Scopes: []memory.Scope{memory.ScopeUser}}
	for _, field := range []string{"apiKey", "unknownOption"} {
		t.Run(field, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true, "secretReference": map[string]string{"kind": "env", "name": "OPAQUE_MEM0_SECRET", field: "synthetic-undocumented-value"}})
			t.Setenv("AM_MEM0_CONFIG", string(data))
			if _, err := memoryprovider.Open(c); err != memory.ErrInvalidInput {
				t.Fatalf("nested unknown field accepted: %v", err)
			}
			if requests.Load() != 0 {
				t.Fatal("invalid configuration caused service/auth access")
			}
		})
	}
	// The documented opaque reference remains compatible with strict decoding.
	data, _ := json.Marshal(map[string]any{"endpoint": server.URL, "contract": mem0.ContractVersion, "allowNetwork": true, "secretReference": map[string]string{"kind": "env", "name": "OPAQUE_MEM0_SECRET"}})
	t.Setenv("AM_MEM0_CONFIG", string(data))
	if _, err := memoryprovider.Open(c); err != nil {
		t.Fatalf("documented reference rejected: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("construction contacted service")
	}
	for _, trailing := range []string{" {}", " true", " invalid"} {
		t.Setenv("AM_MEM0_CONFIG", string(data)+trailing)
		if _, err := memoryprovider.Open(c); err != memory.ErrInvalidInput {
			t.Fatalf("trailing input accepted: %v", err)
		}
	}
}
