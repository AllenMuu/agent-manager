//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package taskcontext_test

import (
	"context"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"reflect"
	"testing"
	"time"
)

func TestTaskContextReadsAttributedKnowledgeThroughSameGateway(t *testing.T) {
	ctx := context.Background()
	project := t.TempDir()
	artifacts, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-memory", project, time.Now())
	intent.Set("summary", "alpha")
	if _, err = artifacts.Init("task-memory", intent); err != nil {
		t.Fatal(err)
	}
	provider, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "context-project"}
	original, err := memory.Remember(ctx, provider, memory.NewRecord{Owner: owner, Type: memory.TypeProjectContext, Content: "alpha guidance", Source: "design:21", Evidence: []string{"review:21"}})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := memory.NewGateway(provider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{MaxResults: 1, ContextBytes: 500})
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := role.For(role.Planner)
	options := taskcontext.Options{Project: project, TaskID: "task-memory", Contract: contract, MemoryGateway: gateway, MemoryOwner: owner}
	bundle, err := taskcontext.ResolveWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	search, err := gateway.Search(ctx, memory.SearchRequest{Owner: owner, Text: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle.MemoryRecords, search) || len(bundle.MemoryRecords) != 1 || bundle.MemoryRecords[0].ID != original.ID || bundle.MemoryRecords[0].Source != "design:21" || bundle.MemoryRecords[0].Evidence[0] != "review:21" || len(bundle.Memory) != 0 {
		t.Fatalf("context path/attribution: %+v", bundle)
	}
	history, err := memory.History(ctx, provider, owner, original.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("implicit write: %v %v", history, err)
	}
	if _, err = gateway.Preview(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "forbidden"}}); err != memory.ErrOwnershipDenied {
		t.Fatalf("context Gateway has write authority: %v", err)
	}
}
