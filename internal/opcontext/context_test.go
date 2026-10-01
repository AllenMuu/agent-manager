package opcontext_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/opcontext"
)

type fixedProvider struct {
	records []opcontext.Record
	reads   int
}

func (p *fixedProvider) Read(context.Context, opcontext.Query) ([]opcontext.Record, error) {
	p.reads++
	return append([]opcontext.Record(nil), p.records...), nil
}

var _ opcontext.Provider = (*fixedProvider)(nil)

type mutatingProvider struct {
	reads int
}

func (p *mutatingProvider) Read(_ context.Context, query opcontext.Query) ([]opcontext.Record, error) {
	p.reads++
	query.Keys[0] = "incident"
	now := time.Now().UTC()
	return []opcontext.Record{{Key: "incident", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)}}, nil
}

var _ opcontext.Provider = (*mutatingProvider)(nil)

func TestReadReturnsSourceCaptureTimeAndComputedFreshness(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	provider := &fixedProvider{records: []opcontext.Record{
		{Key: "deployment", Source: "local-inventory", CapturedAt: now.Add(-time.Minute), FreshUntil: now.Add(time.Minute), Content: "version 4"},
		{Key: "incident", Source: "incident-cache", CapturedAt: now.Add(-time.Hour), FreshUntil: now.Add(-time.Minute), Content: "resolved"},
	}}
	service := opcontext.Service{Provider: provider, AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment", "incident"}}}
	snapshot, err := service.Read(context.Background(), opcontext.Query{Keys: []string{"deployment", "incident"}}, now)
	if err != nil {
		t.Fatalf("read operational context: %v", err)
	}
	if !snapshot.Available || !snapshot.CheckedAt.Equal(now) || len(snapshot.Records) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Records[0].Fresh || !snapshot.Records[1].Fresh || snapshot.Records[1].Source != "local-inventory" || !snapshot.Records[1].CapturedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("freshness-aware records = %#v", snapshot.Records)
	}
	if provider.reads != 1 {
		t.Fatalf("provider reads = %d, want 1", provider.reads)
	}
}

func TestReadWithoutProviderReportsUnavailableWithoutInventingRecords(t *testing.T) {
	now := time.Now().UTC()
	snapshot, err := (opcontext.Service{AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}}}).Read(context.Background(), opcontext.Query{Keys: []string{"deployment"}}, now)
	if err != nil {
		t.Fatalf("unconfigured provider: %v", err)
	}
	if snapshot.Available || len(snapshot.Records) != 0 {
		t.Fatalf("unconfigured provider fabricated context: %#v", snapshot)
	}
}

func TestReadRejectsInvalidProviderFreshnessAndIdentifiers(t *testing.T) {
	now := time.Now().UTC()
	cases := []opcontext.Record{
		{Key: "deployment", Source: "local", CapturedAt: now, FreshUntil: now.Add(-time.Second)},
		{Key: "*", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)},
		{Key: "deployment", Source: "", CapturedAt: now, FreshUntil: now.Add(time.Minute)},
	}
	for i, record := range cases {
		provider := &fixedProvider{records: []opcontext.Record{record}}
		if _, err := (opcontext.Service{Provider: provider, AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}}}).Read(context.Background(), opcontext.Query{Keys: []string{"deployment"}}, now); err == nil {
			t.Errorf("invalid provider record %d was accepted", i)
		}
	}
}

func TestOperationalContextProviderContractIsReadOnly(t *testing.T) {
	providerType := reflect.TypeOf((*opcontext.Provider)(nil)).Elem()
	if providerType.NumMethod() != 1 || providerType.Method(0).Name != "Read" {
		t.Fatalf("provider contract exposes operations beyond read: %v", providerType)
	}
}

func TestOperationalContextReadDoesNotDispatchMutationInvocation(t *testing.T) {
	now := time.Now().UTC()
	provider := &fixedProvider{records: []opcontext.Record{{Key: "deployment", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)}}}
	mutationAdapter := invocation.NewMockAdapter(true, "unexpected mutation")
	if _, err := (opcontext.Service{Provider: provider, AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}}}).Read(context.Background(), opcontext.Query{Keys: []string{"deployment"}}, now); err != nil {
		t.Fatal(err)
	}
	if provider.reads != 1 || len(mutationAdapter.Calls()) != 0 {
		t.Fatalf("operational read dispatched an invocation: reads=%d calls=%#v", provider.reads, mutationAdapter.Calls())
	}
}

func TestAccessPolicyDeniesUnlistedContextBeforeProviderRead(t *testing.T) {
	now := time.Now().UTC()
	provider := &fixedProvider{records: []opcontext.Record{{Key: "deployment", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)}}}
	service := opcontext.Service{
		Provider:     provider,
		AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}},
	}
	if _, err := service.Read(context.Background(), opcontext.Query{Keys: []string{"deployment", "incident"}}, now); !errors.Is(err, opcontext.ErrAccessDenied) {
		t.Fatalf("unauthorized read error = %v, want ErrAccessDenied", err)
	}
	if provider.reads != 0 {
		t.Fatalf("provider reads = %d after denied request, want 0", provider.reads)
	}
}

func TestOperationalContextReadRequiresIndependentAccessPolicy(t *testing.T) {
	now := time.Now().UTC()
	provider := &fixedProvider{records: []opcontext.Record{{Key: "deployment", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)}}}
	service := opcontext.Service{Provider: provider}
	if _, err := service.Read(context.Background(), opcontext.Query{Keys: []string{"deployment"}}, now); !errors.Is(err, opcontext.ErrAccessDenied) {
		t.Fatalf("read without access policy error = %v, want ErrAccessDenied", err)
	}
	if provider.reads != 0 {
		t.Fatalf("provider reads = %d without access policy, want 0", provider.reads)
	}
}

func TestOperationalContextReadRejectsUnrequestedProviderRecords(t *testing.T) {
	now := time.Now().UTC()
	provider := &fixedProvider{records: []opcontext.Record{
		{Key: "deployment", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)},
		{Key: "incident", Source: "local", CapturedAt: now, FreshUntil: now.Add(time.Minute)},
	}}
	service := opcontext.Service{Provider: provider, AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}}}
	if _, err := service.Read(context.Background(), opcontext.Query{Keys: []string{"deployment"}}, now); err == nil {
		t.Fatal("provider returned an unrequested key without an error")
	}
}

func TestProviderCannotMutateAuthorizedQueryToExpandContextRead(t *testing.T) {
	now := time.Now().UTC()
	provider := &mutatingProvider{}
	service := opcontext.Service{Provider: provider, AccessPolicy: opcontext.AccessPolicy{AllowedKeys: []string{"deployment"}}}
	query := opcontext.Query{Keys: []string{"deployment"}}
	if _, err := service.Read(context.Background(), query, now); err == nil {
		t.Fatal("provider changed the query and returned an unrequested key without an error")
	}
	if provider.reads != 1 || query.Keys[0] != "deployment" {
		t.Fatalf("provider reads=%d caller query=%#v; want one call and unchanged query", provider.reads, query)
	}
}
