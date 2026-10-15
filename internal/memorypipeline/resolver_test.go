package memorypipeline

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

type countingEvidenceProvider struct {
	evidence RunEvidence
	reads    int
}

func (p *countingEvidenceProvider) Discover(context.Context, string) ([]RunEvidenceRef, error) {
	return []RunEvidenceRef{p.evidence.Ref}, nil
}
func (p *countingEvidenceProvider) Read(_ context.Context, ref RunEvidenceRef) (RunEvidence, error) {
	p.reads++
	if ref.ID != p.evidence.Ref.ID {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	return p.evidence, nil
}

type countingMemoryProvider struct {
	*memory.InMemoryProvider
	searches int
}

func (p *countingMemoryProvider) Recall(ctx context.Context, query memory.Query) ([]memory.Record, error) {
	p.searches++
	return p.InMemoryProvider.Recall(ctx, query)
}

func TestResolverChecksExactProjectBeforeEveryProvider(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	ref := storeEvidenceRef(t, "project-authorized", "run-authorized", "authorization")
	if _, err := store.ImportEvidence(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	evidence := RunEvidence{Ref: ref, Status: "completed", SummaryText: "private source evidence"}
	evidenceProvider := &countingEvidenceProvider{evidence: evidence}
	memoryProvider := &countingMemoryProvider{InMemoryProvider: memory.NewInMemoryProvider()}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: ref.ProjectID}
	gateway, err := memory.NewGateway(memoryProvider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(ref.ProjectID, store, gateway, evidenceProvider)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	foreign := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-foreign"}
	checks := []struct {
		name string
		call func() error
	}{
		{name: "summary", call: func() error { _, err := resolver.Summary(context.Background(), foreign); return err }},
		{name: "canonical", call: func() error {
			_, err := resolver.CanonicalSearch(context.Background(), foreign, "anything")
			return err
		}},
		{name: "derived", call: func() error {
			_, err := resolver.DerivedSearch(context.Background(), foreign, "anything", 10)
			return err
		}},
		{name: "evidence", call: func() error { _, err := resolver.Evidence(context.Background(), foreign, ref.ID); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, memory.ErrOwnershipDenied) {
				t.Fatalf("wrong project was not rejected before provider access: %v", err)
			}
		})
	}
	if memoryProvider.searches != 0 || evidenceProvider.reads != 0 {
		t.Fatalf("unauthorized provider calls: memory=%d evidence=%d", memoryProvider.searches, evidenceProvider.reads)
	}
}

func TestResolverStartsWithSummaryAndCanonicalThenOpensDerivedEvidenceOnDemand(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	evidence := RunEvidence{Ref: RunEvidenceRef{ProjectID: "project-layered", Runtime: "codex", NativeRunID: "run-layered", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}, Status: "completed", SummaryText: "raw evidence opened only on request", Discoveries: []string{"derived cache boundary fact"}}
	ref, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Ref = ref
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	extraction, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(ctx, extraction); err != nil {
		t.Fatal(err)
	}
	evidenceProvider := &countingEvidenceProvider{evidence: evidence}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: ref.ProjectID}
	memoryProvider := &countingMemoryProvider{InMemoryProvider: memory.NewInMemoryProvider()}
	if _, err := memoryProvider.Remember(ctx, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "canonical cache boundary policy", Source: "operator", Evidence: []string{"ticket:accepted"}}); err != nil {
		t.Fatal(err)
	}
	gateway, err := memory.NewGateway(memoryProvider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(ref.ProjectID, store, gateway, evidenceProvider)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := resolver.Initial(ctx, owner, "cache boundary")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Summary == nil || len(initial.Canonical) != 1 || len(initial.Derived) != 0 || evidenceProvider.reads != 0 {
		t.Fatalf("initial progressive context read too deeply: %#v reads=%d", initial, evidenceProvider.reads)
	}
	derived, err := resolver.DerivedSearch(ctx, owner, "derived cache boundary", 10)
	if err != nil || len(derived) != 1 {
		t.Fatalf("derived search: %#v, %v", derived, err)
	}
	opened, err := resolver.Evidence(ctx, owner, ref.ID)
	if err != nil || opened.Ref.ID != ref.ID || evidenceProvider.reads != 1 {
		t.Fatalf("explicit evidence lookup: %#v, %v reads=%d", opened, err, evidenceProvider.reads)
	}
}
