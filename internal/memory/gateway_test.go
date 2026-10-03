package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"strings"
	"testing"
)

func TestConfiguredSearchIsReportedSeparatelyFromImplementation(t *testing.T) {
	config := &memory.ProviderConfig{Provider: "fixture", Capabilities: []memory.Capability{memory.CapabilitySearch}}
	report := memory.BuildStatus(config, memory.ProviderStatus{Available: true}, nil, nil)
	if len(report.RequestedCapabilities) != 1 || report.RequestedCapabilities[0] != memory.CapabilitySearch || len(report.Capabilities) != 0 || len(report.UnsupportedCapabilities) != 1 {
		t.Fatalf("search request was advertised as implemented: %+v", report)
	}
}

var _ = context.Background

var _ = errors.Is

type forbiddenProvider struct{}

func (forbiddenProvider) Capabilities() memory.StructuredCapabilities {
	panic("unauthorized provider access")
}

func (forbiddenProvider) Health(context.Context) (memory.HealthStatus, error) {
	panic("unauthorized provider access")
}

func TestWrongOwnerIsRejectedBeforeAnyProviderAccess(t *testing.T) {
	allowed := memory.Owner{Kind: memory.OwnerProject, ProjectID: "allowed"}
	other := memory.Owner{Kind: memory.OwnerProject, ProjectID: "other"}
	g, err := memory.NewGateway(forbiddenProvider{}, memory.Access{ReadOwners: []memory.Owner{allowed}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Search(context.Background(), memory.SearchRequest{Owner: other, Text: "secret"}); !errors.Is(err, memory.ErrOwnershipDenied) {
		t.Fatalf("wrong-owner: %v", err)
	}
}

type scoredFixture struct{ results []memory.ScoredRecord }

func (scoredFixture) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Recall: true, ScoredRecall: true}
}

func (scoredFixture) Health(context.Context) (memory.HealthStatus, error) {
	return memory.HealthStatus{Available: true}, nil
}

func (p scoredFixture) RecallScored(context.Context, memory.Query) ([]memory.ScoredRecord, error) {
	return p.results, nil
}

func TestScoredRetrievalKeepsSemanticMatchesAndCountsFullAttribution(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "scored"}
	record := func(id, content, source string) memory.Record {
		return memory.Record{ID: memory.RecordID(id), Version: 1, Owner: owner, Type: memory.TypeFact, Content: content, Source: source, Evidence: []string{"evidence:" + id}, State: memory.RecordActive, Layer: memory.LayerRaw}
	}
	fixture := scoredFixture{results: []memory.ScoredRecord{
		{Record: record("z", "automobile", "book"), Score: .9},
		{Record: record("a", "vehicle", "book"), Score: .9},
		{Record: record("huge", "transport", strings.Repeat("attribution", 100)), Score: 1},
		{Record: record("low", "car", "book"), Score: .1},
	}}
	g, err := memory.NewGateway(fixture, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{MaxResults: 3, ContentBytes: 100, ContextBytes: 270, MinRelevance: .5})
	if err != nil {
		t.Fatal(err)
	}
	found, err := g.Search(context.Background(), memory.SearchRequest{Owner: owner, Text: "car"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != "a" || found[0].Evidence[0] != "evidence:a" {
		t.Fatalf("semantic score/budget/stable tie: %+v", found)
	}
	encoded, err := json.Marshal(found)
	if err != nil || len(encoded) > 270 {
		t.Fatalf("context budget: %d %v", len(encoded), err)
	}
	if fixture.results[0].Record.ID != "z" {
		t.Fatal("Gateway reordered provider-owned input")
	}
}

func TestGatewayRejectsForeignAndMalformedProviderRecords(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "authorized"}
	baseline := memory.Record{ID: "neutral-id", Version: 1, Owner: owner, Type: memory.TypeFact, Content: "fact", Source: "source", State: memory.RecordActive, Layer: memory.LayerRaw}
	for _, test := range []struct {
		name     string
		record   memory.Record
		expected error
	}{
		{"foreign", func() memory.Record { r := baseline; r.Owner.ProjectID = "foreign"; return r }(), memory.ErrOwnershipDenied},
		{"invalid-id", func() memory.Record { r := baseline; r.ID = "invalid id"; return r }(), memory.ErrInvalidInput},
		{"invalid-scalar", func() memory.Record { r := baseline; r.Source = "\xff"; return r }(), memory.ErrInvalidInput},
		{"invalid-state", func() memory.Record { r := baseline; r.State = "UNKNOWN"; return r }(), memory.ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			g, err := memory.NewGateway(scoredFixture{results: []memory.ScoredRecord{{Record: test.record, Score: 1}}}, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			found, err := g.Search(context.Background(), memory.SearchRequest{Owner: owner})
			if !errors.Is(err, test.expected) || len(found) != 0 {
				t.Fatalf("unsafe provider response: %v %v", found, err)
			}
		})
	}
}

func TestAgentStatusDoesNotTurnMissingImplementationIntoAvailabilityGap(t *testing.T) {
	config := &memory.ProviderConfig{Provider: "fixture", Capabilities: []memory.Capability{memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}
	report := memory.BuildStatus(config, memory.ProviderStatus{Capabilities: []memory.Capability{memory.CapabilityRead}, Scopes: []memory.Scope{memory.ScopeProject}}, []memory.AgentAccess{{Agent: "fixture-agent", Capabilities: []memory.Capability{memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}}, nil)
	agent := report.Agents[0]
	if len(agent.RequestedCapabilities) != 1 || len(agent.ImplementedCapabilities) != 0 || len(agent.UnsupportedCapabilities) != 1 || agent.UnsupportedCapabilities[0] != memory.CapabilitySearch || len(agent.UnavailableCapabilities) != 0 {
		t.Fatalf("unsupported search mislabeled unavailable: %+v", agent)
	}
}

type uncertainWriteFixture struct{}

func (uncertainWriteFixture) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Remember: true}
}

func (uncertainWriteFixture) Health(context.Context) (memory.HealthStatus, error) {
	return memory.HealthStatus{Available: true}, nil
}

func (uncertainWriteFixture) RememberWithOperation(context.Context, memory.RememberRequest) (memory.Record, error) {
	return memory.Record{}, fmt.Errorf("%w: %w: SECRET_REF", memory.ErrOutcomeUnknown, context.Canceled)
}

func TestGatewayRetainsUncertainWriteCategoryWhenCancellationIsACause(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	gateway, err := memory.NewGateway(uncertainWriteFixture{}, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := gateway.Preview(context.Background(), memory.Mutation{Operation: memory.OperationAdd, Record: memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "intent"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Commit(context.Background(), preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner})
	if !errors.Is(err, memory.ErrOutcomeUnknown) || strings.Contains(err.Error(), "SECRET_REF") {
		t.Fatalf("lost uncertain recovery category: %v", err)
	}
}

func TestGatewayDoesNotAdvertiseBasicRememberAsConfirmedAdd(t *testing.T) {
	ctx := context.Background()
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	provider := memory.NewInMemoryProvider()
	gateway, err := memory.NewGateway(provider, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	input := memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "basic provider fact", Source: "report:F1"}
	preview, err := gateway.Preview(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: input})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Commit(ctx, preview, memory.Confirmation{}); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatalf("unconfirmed basic add: %v", err)
	}
	if _, err := gateway.Commit(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("confirmed add silently fell back to basic Remember: %v", err)
	}
	records, err := memory.Recall(ctx, provider, memory.Query{Owner: owner})
	if err != nil || len(records) != 0 {
		t.Fatalf("unsupported add mutated: %v %v", records, err)
	}
	created, err := memory.Remember(ctx, provider, input)
	if err != nil || created.Content != input.Content {
		t.Fatalf("generic basic Remember compatibility: %v %v", created, err)
	}
	report := gateway.Status(ctx, &memory.ProviderConfig{Provider: "basic-only", Capabilities: []memory.Capability{memory.CapabilityWrite}})
	for _, capability := range report.Capabilities {
		if capability == memory.CapabilityWrite {
			t.Errorf("unsupported confirmed add advertised as write: %+v", report)
		}
	}
	if report.StructuredCapabilities == nil || report.StructuredCapabilities.Remember || len(report.UnsupportedCapabilities) != 1 || report.UnsupportedCapabilities[0] != memory.CapabilityWrite {
		t.Fatalf("basic provider contract confused with Gateway writes: %+v", report)
	}
}

func TestAuthenticationDiagnosticCategoryPreservesSafeWrappedErrors(t *testing.T) {
	for _, err := range []error{memory.ErrAuthentication, fmt.Errorf("synthetic-private-backend-detail: %w", memory.ErrAuthentication), memory.SafeError(fmt.Errorf("synthetic-private-backend-detail: %w", memory.ErrAuthentication))} {
		if got := memory.DiagnosticCategory(err); got != "authentication" {
			t.Fatalf("rejected authentication diagnostic = %q", got)
		}
	}
	for _, test := range []struct {
		err  error
		want string
	}{
		{nil, ""}, {memory.ErrCanceled, "canceled"}, {memory.ErrUnavailable, "unavailable"}, {memory.ErrUnsupported, "unsupported"}, {memory.ErrOwnershipDenied, "ownership-denied"}, {memory.ErrConflict, "conflict"}, {memory.ErrInvalidInput, "invalid-input"}, {memory.ErrNotFound, "not-found"}, {memory.ErrNotConfirmed, "not-confirmed"}, {memory.ErrOutcomeUnknown, "outcome-unknown"}, {memory.ErrNotCommitted, "not-committed"},
	} {
		if got := memory.DiagnosticCategory(test.err); got != test.want {
			t.Fatalf("existing diagnostic changed: %q want %q", got, test.want)
		}
	}
}
