package taskcontext_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type outageFixture struct{}

func (outageFixture) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Recall: true}
}

func (outageFixture) Health(context.Context) (memory.HealthStatus, error) {
	return memory.HealthStatus{Available: true}, nil
}

func (outageFixture) Recall(context.Context, memory.Query) ([]memory.Record, error) {
	return nil, fmt.Errorf("external provider error contains SECRET_REF")
}

func TestMemoryOutageLeavesOtherTaskContextResourcesAvailable(t *testing.T) {
	project := t.TempDir()
	artifacts, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "outage", project, time.Now())
	intent.Set("summary", "alpha")
	if _, err = artifacts.Init("outage", intent); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(project+"/AGENTS.md", []byte("repository guidance"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "outage"}
	gateway, err := memory.NewGateway(outageFixture{}, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := role.For(role.Planner)
	bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{Project: project, TaskID: "outage", Contract: contract, MemoryGateway: gateway, MemoryOwner: owner})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MemoryState.Available || bundle.MemoryDiagnostic != "unavailable" || len(bundle.RepositoryKnowledge) != 1 || len(bundle.Artifacts) != 1 || len(bundle.MemoryRecords) != 0 {
		t.Fatalf("outage context: %+v", bundle)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil || strings.Contains(string(encoded), "SECRET_REF") {
		t.Fatalf("raw provider error leaked: %s %v", encoded, err)
	}
}

type contextAuthResolver struct {
	key   string
	calls int
}

func (s *contextAuthResolver) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	s.calls++
	if ref.Kind != "env" || ref.Name != "CONTEXT_SECRET_REFERENCE" {
		return "", memory.ErrAuthentication
	}
	return s.key, nil
}
func TestMem0AuthenticationDiagnosticRetainsIndependentTaskResources(t *testing.T) {
	const key = "synthetic-private-context-key"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-API-Key") != key {
			t.Error("credential absent from transport")
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(key))
	}))
	defer server.Close()
	ref := memory.ConfigReference{Kind: "env", Name: "CONTEXT_SECRET_REFERENCE"}
	resolver := &contextAuthResolver{key: key}
	provider, err := mem0.New(mem0.Config{Endpoint: server.URL, AllowNetwork: true, Contract: mem0.ContractVersion, SecretReference: &ref}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "auth-context"}
	gateway, err := memory.NewGateway(provider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	project, library := t.TempDir(), t.TempDir()
	store, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "auth-context", project, time.Now())
	intent.Set("summary", "Go tests")
	if _, err = store.Init("auth-context", intent); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("Independent repository guidance"), 0600); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(library, "helper")
	if err = os.Mkdir(skill, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: Helper\ndescription: Independent task guidance\n---\nUse Go tests.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	contract, _ := role.For(role.Planner)
	bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{Project: project, TaskID: "auth-context", Contract: contract, Library: library, SelectedSkills: []string{"helper"}, MemoryGateway: gateway, MemoryOwner: owner})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MemoryDiagnostic != "authentication" || bundle.MemoryState.Available || bundle.MemoryState.Reason != memory.ErrAuthentication.Error() {
		t.Fatalf("authentication projected incorrectly: %q %+v", bundle.MemoryDiagnostic, bundle.MemoryState)
	}
	if len(bundle.Artifacts) != 1 || len(bundle.RepositoryKnowledge) != 1 || len(bundle.Skills) != 1 || bundle.Skills[0].Identifier != "helper" || len(bundle.MemoryRecords) != 0 {
		t.Fatalf("independent resources lost: %+v", bundle)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{key, server.URL, ref.Name} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private transport evidence escaped into context")
		}
	}
	if !strings.Contains(string(encoded), "Memory context unavailable: authentication") {
		t.Fatalf("diagnostic warning missing: %s", encoded)
	}
	if requests.Load() != 1 || resolver.calls != 1 {
		t.Fatalf("auth failure caused extra accesses: requests=%d resolves=%d", requests.Load(), resolver.calls)
	}
}
