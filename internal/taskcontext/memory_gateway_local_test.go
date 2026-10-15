//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package taskcontext_test

import (
	"context"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memorypipeline"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"path/filepath"
	"reflect"
	"strings"
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

func TestTaskContextIncludesPipelineSummaryAndCanonicalMemoryOnly(t *testing.T) {
	ctx := context.Background()
	project := t.TempDir()
	artifacts, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-pipeline", project, time.Now())
	intent.Set("summary", "cache boundary")
	if _, err := artifacts.Init("task-pipeline", intent); err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "context-pipeline"}
	canonical, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Remember(ctx, canonical, memory.NewRecord{Owner: owner, Type: memory.TypeConstraint, Content: "Canonical cache boundaries are project scoped", Source: "accepted-review", Evidence: []string{"review:accepted"}}); err != nil {
		t.Fatal(err)
	}
	gateway, err := memory.NewGateway(canonical, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{MaxResults: 3, ContextBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	pipelineStore, err := memorypipeline.OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipelineStore.Close()
	evidence := memorypipeline.RunEvidence{
		Ref:    memorypipeline.RunEvidenceRef{ProjectID: owner.ProjectID, Runtime: "fixture", NativeRunID: "run-pipeline", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: "completed", SummaryText: "Completed run recorded a cache boundary discovery.",
		Discoveries: []string{"A derived cache boundary was observed."},
	}
	ref, err := memorypipeline.ReferenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := pipelineStore.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	extraction, err := memorypipeline.ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := pipelineStore.Consolidate(ctx, extraction); err != nil {
		t.Fatal(err)
	}
	resolver, err := memorypipeline.NewResolver(owner.ProjectID, pipelineStore, gateway, nil)
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := role.For(role.Planner)
	bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{
		Project: project, TaskID: "task-pipeline", Contract: contract,
		MemoryPipeline: resolver, MemoryPipelineOwner: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MemorySummary == nil || len(bundle.MemorySummary.EvidenceIDs) != 1 || bundle.MemorySummary.EvidenceIDs[0] != ref.ID {
		t.Fatalf("pipeline summary attribution = %#v", bundle.MemorySummary)
	}
	if len(bundle.MemoryRecords) != 1 || bundle.MemoryRecords[0].Source != "accepted-review" || bundle.MemoryRecords[0].Evidence[0] != "review:accepted" {
		t.Fatalf("canonical Memory attribution = %#v", bundle.MemoryRecords)
	}
	if len(bundle.MemoryDerived) != 0 || strings.Contains(bundle.MemoryRecords[0].Content, "derived cache boundary") {
		t.Fatalf("default task context included deeper derived memory: %#v", bundle)
	}
}
