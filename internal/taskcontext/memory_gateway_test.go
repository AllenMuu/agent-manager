package taskcontext_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"os"
	"strings"
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
