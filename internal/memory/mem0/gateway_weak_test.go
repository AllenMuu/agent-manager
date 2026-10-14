package mem0_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfirmedWeakGatewayCreateIsRetrievableWithProvenance(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, err := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := inputRecord()
	g, err := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := g.PreviewWeak(context.Background(), memory.Mutation{Operation: memory.OperationAdd, Record: input})
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.CommitWeak(context.Background(), preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: input.Owner, Source: input.Source})
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.Get(context.Background(), input.Owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Source != "design.md" || len(got.Evidence) != 1 || got.Evidence[0] != "issue22" || got.Content != "Use standard Go tests" {
		t.Fatalf("canonical provenance lost: %+v", got)
	}
}

func TestConfirmedWeakReplacementRetainsPriorProvenanceSeparately(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	ctx := context.Background()
	input := inputRecord()
	old, err := p.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{})
	input.Content = "Run go test ./... before delivery"
	input.Source = "review.md"
	input.Evidence = []string{"issue23"}
	preview, err := g.PreviewWeak(ctx, memory.Mutation{Operation: memory.OperationReplaceObserved, ID: old.ID, ExpectedVersion: 1, Record: input})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := g.CommitWeak(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: input.Owner, Source: input.Source})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != old.ID || updated.Version != 2 || updated.Content != "Run go test ./... before delivery" {
		t.Fatalf("replacement: %+v", updated)
	}
	history, err := g.RetainedHistory(ctx, input.Owner, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].State != memory.RecordSuperseded || history[0].Version != 1 || history[0].Content != "Use standard Go tests" || history[0].Source != "design.md" || history[0].Evidence[0] != "issue22" {
		t.Fatalf("prior lineage: %+v", history)
	}
	matches, err := g.Search(ctx, memory.SearchRequest{Owner: input.Owner, Text: "Go tests"})
	if err != nil || len(matches) != 1 || matches[0].Version != 2 {
		t.Fatalf("effective recall: %+v %v", matches, err)
	}
}

func TestWeakConfirmationCannotReplayAnUnreceiptedWrite(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	input := inputRecord()
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{})
	preview, err := g.PreviewWeak(context.Background(), memory.Mutation{Operation: memory.OperationAdd, Record: input})
	if err != nil {
		t.Fatal(err)
	}
	c := memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: input.Owner, Source: input.Source}
	if _, err = g.CommitWeak(context.Background(), preview, c); err != nil {
		t.Fatal(err)
	}
	if _, err = g.CommitWeak(context.Background(), preview, c); err != memory.ErrNotConfirmed {
		t.Fatalf("unreceipted confirmation replay accepted: %v", err)
	}
}

func TestWeakPreviewBoundsTheCompleteAttributedPlan(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	input := inputRecord()
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{ContextBytes: 400})
	if _, err := g.PreviewWeak(context.Background(), memory.Mutation{Operation: memory.OperationAdd, Record: input}); err != memory.ErrInvalidInput {
		t.Fatalf("full plan attribution/semantics bypassed budget: %v", err)
	}
}

func TestObservedWeakReplacementRejectsStalePreviewWithoutOverwriting(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	ctx := context.Background()
	input := inputRecord()
	old, err := p.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{})
	input.Content = "previewed replacement"
	preview, err := g.PreviewWeak(ctx, memory.Mutation{Operation: memory.OperationReplaceObserved, Record: input, ID: old.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	plan := preview.Plan()
	plan.Record.Content = "mutated display copy"
	plan.Observed.Content = "mutated snapshot copy"
	input.Content = "concurrent writer won"
	if _, err = p.Replace(ctx, memory.ReplaceRequest{Owner: input.Owner, ID: old.ID, Record: input}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.CommitWeak(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: input.Owner, Source: input.Source}); err != memory.ErrConflict {
		t.Fatalf("stale observed snapshot replaced: %v", err)
	}
	got, err := g.Get(ctx, input.Owner, old.ID)
	if err != nil || got.Content != "concurrent writer won" || got.Version != 2 {
		t.Fatalf("stale overwrite: %+v %v", got, err)
	}
}

func TestRetainedLineageIsBoundedAndStrongHistoryRemainsUnsupported(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	ctx := context.Background()
	input := inputRecord()
	r, err := p.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		input.Content = "replacement " + fmt.Sprint(i)
		r, err = p.ReplaceObserved(ctx, r, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}}, memory.RetrievalPolicy{})
	rs, err := g.RetainedHistory(ctx, input.Owner, r.ID)
	if err != nil || len(rs) != 8 || rs[0].Version != 2 || rs[7].Version != 9 {
		t.Fatalf("bounded retained lineage: %+v %v", rs, err)
	}
	if _, err = g.History(ctx, input.Owner, r.ID); err != memory.ErrUnsupported {
		t.Fatalf("retained snapshots silently enabled strong history: %v", err)
	}
}

func TestRetainedLineageRejectsForeignAndLossySnapshots(t *testing.T) {
	for _, kind := range []string{"foreign-owner", "duplicate-version", "overflow-version", "unpaired-surrogate", "wrong-neutral-id"} {
		t.Run(kind, func(t *testing.T) {
			base := fixture(t)
			defer base.Close()
			corrupt := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				base.Config.Handler.ServeHTTP(rec, r)
				data := rec.Body.Bytes()
				if corrupt && r.Method == "GET" {
					var remote map[string]any
					json.Unmarshal(data, &remote)
					meta := remote["metadata"].(map[string]any)
					raw := meta["am_retained_lineage"].(string)
					switch kind {
					case "foreign-owner":
						raw = strings.ReplaceAll(raw, "project-A", "foreign-project")
					case "duplicate-version":
						raw = strings.TrimSuffix(raw, "]") + "," + strings.TrimPrefix(raw, "[")
					case "overflow-version":
						raw = strings.Replace(raw, `"version":1`, `"version":18446744073709551616`, 1)
					case "unpaired-surrogate":
						raw = strings.Replace(raw, "design.md", `\ud800`, 1)
					case "wrong-neutral-id":
						var snapshots []memory.Record
						json.Unmarshal([]byte(raw), &snapshots)
						snapshots[0].ID = "memory-other"
						b, _ := json.Marshal(snapshots)
						raw = string(b)
					}
					meta["am_retained_lineage"] = raw
					data, _ = json.Marshal(remote)
				}
				w.WriteHeader(rec.Code)
				w.Write(data)
			}))
			defer server.Close()
			p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
			ctx := context.Background()
			input := inputRecord()
			r, err := p.Remember(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			input.Content = "replacement"
			r, err = p.ReplaceObserved(ctx, r, input)
			if err != nil {
				t.Fatal(err)
			}
			corrupt = true
			g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}}, memory.RetrievalPolicy{})
			if _, err = g.RetainedHistory(ctx, input.Owner, r.ID); err == nil {
				t.Fatal("corrupt prior canonical lineage was accepted")
			}
		})
	}
}

func TestWeakWriteOptInAndForeignOwnerFailBeforeRemoteAccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	input := inputRecord()
	ctx := context.Background()
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{input.Owner}, WriteOwners: []memory.Owner{input.Owner}}, memory.RetrievalPolicy{})
	if _, err := g.PreviewWeak(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: input}); err != memory.ErrUnsupported {
		t.Fatalf("weak writes lacked explicit opt-in: %v", err)
	}
	input.Owner = memory.Owner{Kind: memory.OwnerProject, ProjectID: "foreign"}
	if _, err := g.PreviewWeak(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: input}); err != memory.ErrOwnershipDenied {
		t.Fatalf("foreign weak preview: %v", err)
	}
	if _, err := g.RetainedHistory(ctx, input.Owner, "memory-id"); err != memory.ErrOwnershipDenied {
		t.Fatalf("foreign retained lineage: %v", err)
	}
	if calls != 0 {
		t.Fatalf("denial reached backend: %d", calls)
	}
}
