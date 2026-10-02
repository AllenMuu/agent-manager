package memory_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

func TestStructuredMemoryRejectsIncompleteOwners(t *testing.T) {
	for _, owner := range []memory.Owner{
		{}, {Kind: memory.OwnerUser}, {Kind: memory.OwnerProject},
		{Kind: memory.OwnerAgent, AgentID: "codex"},
		{Kind: memory.OwnerAgent, ProjectID: "a"},
		{Kind: memory.OwnerSession, SessionID: "chat"},
		{Kind: memory.OwnerSession, UserID: "allen"},
		{Kind: "GLOBAL"},
		{Kind: memory.OwnerProject, ProjectID: "a", UserID: "allen"},
		{Kind: memory.OwnerAgent, ProjectID: "a", UserID: "allen", AgentID: "codex"},
	} {
		t.Run(string(owner.Kind)+owner.ProjectID+owner.AgentID+owner.SessionID, func(t *testing.T) {
			p := memory.NewInMemoryProvider()
			_, err := p.Remember(context.Background(), memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "inert"})
			if !errors.Is(err, memory.ErrInvalidInput) {
				t.Fatalf("Remember() = %v, want invalid-input", err)
			}
		})
	}
}

func TestStructuredMemoryRoundTripAndProjectIsolation(t *testing.T) {
	p := memory.NewInMemoryProvider()
	ctx := context.Background()
	a := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-a"}
	b := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-b"}
	input := memory.NewRecord{Owner: a, Type: memory.TypeConstraint, Content: "Never execute SKILL content", Source: "issue:7", Evidence: []string{"review:alpha", "review:beta"}, Layer: memory.LayerAtomic}
	got, err := p.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.Version != 1 || got.State != memory.RecordActive || got.Owner != a || got.Type != "CONSTRAINT" || got.Content != input.Content || got.Source != "issue:7" || got.Layer != "ATOMIC" || !reflect.DeepEqual(got.Evidence, []string{"review:alpha", "review:beta"}) {
		t.Fatalf("created record = %#v", got)
	}
	fetched, err := p.Get(ctx, a, got.ID)
	if err != nil || !reflect.DeepEqual(fetched, got) {
		t.Fatalf("Get() = %#v, %v", fetched, err)
	}
	recalled, err := p.Recall(ctx, memory.Query{Owner: a, Text: "SKILL"})
	if err != nil || len(recalled) != 1 || !reflect.DeepEqual(recalled[0], got) {
		t.Fatalf("Recall(a) = %#v, %v", recalled, err)
	}
	if _, err := p.Get(ctx, b, got.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("Get(b) = %v, want safe not-found", err)
	}
	records, err := p.Recall(ctx, memory.Query{Owner: b})
	if err != nil || len(records) != 0 {
		t.Fatalf("Recall(b) = %#v, %v", records, err)
	}
}

func TestStructuredMemoryRetainsMetadataDespiteCallerMutation(t *testing.T) {
	p := memory.NewInMemoryProvider()
	ctx := context.Background()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "a"}
	evidence := []string{"source:original"}
	record, err := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "original", Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	evidence[0] = "input:changed"
	record.Evidence[0] = "result:changed"
	fetched, err := p.Get(ctx, owner, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	fetched.Evidence[0] = "get:changed"
	recalled, err := p.Recall(ctx, memory.Query{Owner: owner})
	if err != nil || len(recalled) != 1 || !reflect.DeepEqual(recalled[0].Evidence, []string{"source:original"}) {
		t.Fatalf("Recall() = %#v, %v", recalled, err)
	}
	recalled[0].Evidence[0] = "recall:changed"
	fetched, err = p.Get(ctx, owner, record.ID)
	if err != nil || !reflect.DeepEqual(fetched.Evidence, []string{"source:original"}) {
		t.Fatalf("Get() = %#v, %v", fetched, err)
	}
}

func TestStructuredMemoryCanceledWriteStoresNothing(t *testing.T) {
	p := memory.NewInMemoryProvider()
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "allen"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "do not store"})
	if !errors.Is(err, memory.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Remember() = %v, want canceled", err)
	}
	records, err := p.Recall(context.Background(), memory.Query{Owner: owner})
	if err != nil || len(records) != 0 {
		t.Fatalf("Recall() = %#v, %v", records, err)
	}
}

// This represents an external backend exposing discovery but no read/write
// implementation. Missing optional methods must never look like empty success.
type discoveryOnlyProvider struct{}

func (discoveryOnlyProvider) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{}
}
func (discoveryOnlyProvider) Health(context.Context) (memory.HealthStatus, error) {
	return memory.HealthStatus{Available: true}, nil
}

func TestStructuredMemoryDistinguishesUnsupportedFromUnavailable(t *testing.T) {
	ctx := context.Background()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "a"}
	_, err := memory.Recall(ctx, discoveryOnlyProvider{}, memory.Query{Owner: owner})
	if !errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("Recall(discovery only) = %v", err)
	}
	var unavailable *memory.InMemoryProvider
	_, err = memory.Recall(ctx, unavailable, memory.Query{Owner: owner})
	if !errors.Is(err, memory.ErrUnavailable) || errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("Recall(unavailable) = %v", err)
	}
	p := memory.NewInMemoryProvider()
	caps := p.Capabilities()
	if !caps.Get || !caps.Remember || !caps.Recall || caps.Update || caps.Forget || caps.Supersede {
		t.Fatalf("Capabilities() = %#v", caps)
	}
	health, err := p.Health(ctx)
	if err != nil || !health.Available {
		t.Fatalf("Health() = %#v, %v", health, err)
	}
	health, err = unavailable.Health(ctx)
	if !errors.Is(err, memory.ErrUnavailable) || health.Available {
		t.Fatalf("Health(unavailable) = %#v, %v", health, err)
	}
}

func TestStructuredMemoryRejectsInvalidRecordsWithoutStoringThem(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "a"}
	for _, input := range []memory.NewRecord{
		{Owner: owner, Content: "missing type"},
		{Owner: owner, Type: "unknown", Content: "unknown type"},
		{Owner: owner, Type: "FACT", Content: " \n\t"},
		{Owner: owner, Type: "FACT", Content: "bad layer", Layer: "invented"},
		{Owner: memory.Owner{Kind: memory.OwnerUser, UserID: " \t"}, Type: "FACT", Content: "bad owner"},
	} {
		p := memory.NewInMemoryProvider()
		if _, err := p.Remember(context.Background(), input); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("Remember(%#v) = %v", input, err)
		}
		records, err := p.Recall(context.Background(), memory.Query{Owner: owner})
		if err != nil || len(records) != 0 {
			t.Fatalf("invalid input persisted: %#v, %v", records, err)
		}
	}
}

func TestStructuredMemoryConcurrentWritesRetainEveryRecord(t *testing.T) {
	p := memory.NewInMemoryProvider()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "a"}
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: "FACT", Content: fmt.Sprintf("fact-%d", i)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	records, err := p.Recall(ctx, memory.Query{Owner: owner})
	if err != nil || len(records) != 24 {
		t.Fatalf("Recall() retained %d/24 records, %v", len(records), err)
	}
	ids := map[memory.RecordID]bool{}
	for _, r := range records {
		if ids[r.ID] {
			t.Fatalf("duplicate canonical ID: %s", r.ID)
		}
		ids[r.ID] = true
	}
}

func TestStructuredMemoryAllOwnerPartitionsAreExact(t *testing.T) {
	ctx := context.Background()
	p := memory.NewInMemoryProvider()
	owners := []memory.Owner{
		{Kind: memory.OwnerUser, UserID: "allen"},
		{Kind: memory.OwnerUser, UserID: "other"},
		{Kind: memory.OwnerProject, ProjectID: "a"},
		{Kind: memory.OwnerProject, ProjectID: "b"},
		{Kind: memory.OwnerAgent, UserID: "allen", AgentID: "codex"},
		{Kind: memory.OwnerAgent, ProjectID: "a", AgentID: "codex"},
		{Kind: memory.OwnerAgent, ProjectID: "a", AgentID: "claude"},
		{Kind: memory.OwnerSession, UserID: "allen", SessionID: "session-a"},
		{Kind: memory.OwnerSession, ProjectID: "a", SessionID: "session-a"},
		{Kind: memory.OwnerSession, ProjectID: "a", SessionID: "session-b"},
	}
	for _, owner := range owners {
		record, err := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: "FACT", Content: "same words"})
		if err != nil {
			t.Fatal(err)
		}
		for _, queryOwner := range owners {
			_, err := p.Get(ctx, queryOwner, record.ID)
			if queryOwner == owner && err != nil {
				t.Fatal(err)
			}
			if queryOwner != owner && !errors.Is(err, memory.ErrNotFound) {
				t.Fatalf("owner %#v read %#v: %v", queryOwner, owner, err)
			}
		}
	}
	for _, owner := range owners {
		records, err := p.Recall(ctx, memory.Query{Owner: owner, Text: "SAME"})
		if err != nil || len(records) != 1 || records[0].Owner != owner {
			t.Fatalf("partition %#v: %#v, %v", owner, records, err)
		}
	}
}

func TestStructuredMemoryRejectsEmptyIDAtGetBoundary(t *testing.T) {
	p := memory.NewInMemoryProvider()
	if _, err := memory.Get(context.Background(), p, memory.Owner{Kind: memory.OwnerProject, ProjectID: "a"}, ""); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("Get(empty ID) = %v", err)
	}
}
