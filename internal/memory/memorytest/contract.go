// Package memorytest provides behavioral contract tests for structured Memory
// providers. Factories must return fresh isolated storage; adapters may reuse
// this suite with local fixtures without introducing a live service dependency.
package memorytest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

// Run checks create/get/recall through optional public contracts. It does not
// inspect provider internals, require a vendor SDK or call legacy promotion.
func Run(t *testing.T, newProvider func(t *testing.T) memory.StructuredProvider) {
	t.Helper()
	t.Run("canonical vocabulary and provenance", func(t *testing.T) {
		p := newProvider(t)
		ctx := context.Background()
		owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-alpha"}
		types := []memory.KnowledgeType{"FACT", "DECISION", "PREFERENCE", "CONSTRAINT", "EXPERIENCE", "PROCEDURE", "SKILL", "TASK", "ERROR_SOLUTION", "PROJECT_CONTEXT"}
		layers := []memory.Layer{"RAW", "ATOMIC", "COMPOSITE", "PROFILE"}
		for _, kind := range types {
			for _, layer := range layers {
				created, err := memory.Remember(ctx, p, memory.NewRecord{Owner: owner, Type: kind, Content: "inert knowledge", Source: "issue:7", Evidence: []string{"review:one", "review:two"}, Layer: layer})
				if err != nil {
					t.Fatal(err)
				}
				if created.ID == "" || created.Version != 1 || created.State != "ACTIVE" || created.Owner != owner || created.Type != kind || created.Layer != layer || created.Content != "inert knowledge" || created.Source != "issue:7" || !reflect.DeepEqual(created.Evidence, []string{"review:one", "review:two"}) {
					t.Fatalf("canonical result: %#v", created)
				}
				fetched, err := memory.Get(ctx, p, owner, created.ID)
				if err != nil || !reflect.DeepEqual(fetched, created) {
					t.Fatalf("Get: %#v, %v", fetched, err)
				}
			}
		}
		records, err := memory.Recall(ctx, p, memory.Query{Owner: owner, Text: "knowledge"})
		if err != nil || len(records) != 40 {
			t.Fatalf("Recall: %d/40 records, %v", len(records), err)
		}
		seen := map[memory.RecordID]bool{}
		for _, record := range records {
			if seen[record.ID] {
				t.Fatalf("duplicate ID: %s", record.ID)
			}
			seen[record.ID] = true
		}
	})
	t.Run("two projects and stable recall", func(t *testing.T) {
		p := newProvider(t)
		ctx := context.Background()
		a := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-alpha"}
		b := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-beta"}
		first, err := memory.Remember(ctx, p, memory.NewRecord{Owner: a, Type: "FACT", Content: "zeta knowledge"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := memory.Remember(ctx, p, memory.NewRecord{Owner: b, Type: "FACT", Content: "other knowledge"}); err != nil {
			t.Fatal(err)
		}
		second, err := memory.Remember(ctx, p, memory.NewRecord{Owner: a, Type: "TASK", Content: "alpha knowledge"})
		if err != nil {
			t.Fatal(err)
		}
		if first.Source != "" || len(first.Evidence) != 0 || first.Layer != "RAW" {
			t.Fatalf("invented provenance/default layer: %#v", first)
		}
		if _, err := memory.Get(ctx, p, b, first.ID); !errors.Is(err, memory.ErrNotFound) && !errors.Is(err, memory.ErrOwnershipDenied) {
			t.Fatalf("cross-owner Get: %v", err)
		}
		var previous []memory.Record
		for i := 0; i < 3; i++ {
			records, err := memory.Recall(ctx, p, memory.Query{Owner: a, Text: "KNOWLEDGE"})
			if err != nil || len(records) != 2 {
				t.Fatalf("owned recall: %#v, %v", records, err)
			}
			want := map[memory.RecordID]memory.Record{first.ID: first, second.ID: second}
			for _, record := range records {
				expected, ok := want[record.ID]
				if !ok || !reflect.DeepEqual(record, expected) {
					t.Fatalf("unexpected recall record: %#v", record)
				}
				delete(want, record.ID)
			}
			if len(want) != 0 {
				t.Fatalf("missing owned records: %#v", want)
			}
			if i > 0 && !reflect.DeepEqual(records, previous) {
				t.Fatalf("recall order changed: %#v -> %#v", previous, records)
			}
			previous = records
		}
		records, err := memory.Recall(ctx, p, memory.Query{Owner: b})
		if err != nil || len(records) != 1 || records[0].Content != "other knowledge" {
			t.Fatalf("project beta: %#v, %v", records, err)
		}
	})
	t.Run("canceled write and invalid owner", func(t *testing.T) {
		p := newProvider(t)
		owner := memory.Owner{Kind: memory.OwnerUser, UserID: "user-alpha"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := memory.Remember(ctx, p, memory.NewRecord{Owner: owner, Type: "FACT", Content: "canceled"}); !errors.Is(err, memory.ErrCanceled) {
			t.Fatalf("canceled write: %v", err)
		}
		if _, err := memory.Remember(context.Background(), p, memory.NewRecord{Owner: memory.Owner{Kind: memory.OwnerUser}, Type: "FACT", Content: "invalid"}); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("invalid owner: %v", err)
		}
		records, err := memory.Recall(context.Background(), p, memory.Query{Owner: owner})
		if err != nil || len(records) != 0 {
			t.Fatalf("rejected writes became visible: %#v, %v", records, err)
		}
	})
}
