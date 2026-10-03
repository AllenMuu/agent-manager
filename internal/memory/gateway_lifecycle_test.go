package memory_test

import (
	"context"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"testing"
)

type lifecycleReadProbe struct {
	caps     memory.StructuredCapabilities
	accesses int
}

func (p *lifecycleReadProbe) Capabilities() memory.StructuredCapabilities { return p.caps }
func (p *lifecycleReadProbe) Health(context.Context) (memory.HealthStatus, error) {
	p.accesses++
	return memory.HealthStatus{}, memory.ErrAuthentication
}
func (p *lifecycleReadProbe) Get(context.Context, memory.Owner, memory.RecordID) (memory.Record, error) {
	p.accesses++
	return memory.Record{}, memory.ErrUnavailable
}

type lifecycleMethodsProbe struct{ *lifecycleReadProbe }

func (p lifecycleMethodsProbe) Update(context.Context, memory.UpdateRequest) (memory.Record, error) {
	p.accesses++
	return memory.Record{}, memory.ErrUnavailable
}
func (p lifecycleMethodsProbe) Supersede(context.Context, memory.UpdateRequest) (memory.Record, error) {
	p.accesses++
	return memory.Record{}, memory.ErrUnavailable
}
func (p lifecycleMethodsProbe) Forget(context.Context, memory.MutationRequest) (memory.Record, error) {
	p.accesses++
	return memory.Record{}, memory.ErrUnavailable
}

func TestGatewayUnsupportedLifecyclePreviewUsesInterfaceAndGuaranteeBeforeReads(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	for _, operation := range []memory.Operation{memory.OperationUpdate, memory.OperationSupersede, memory.OperationForget} {
		variants := []string{"declaration-without-interface", "false-operation-declaration"}
		if operation == memory.OperationUpdate {
			variants = append(variants, "no-conditional-update")
		}
		if operation == memory.OperationSupersede {
			variants = append(variants, "no-atomic-supersede")
		}
		for _, variant := range variants {
			t.Run(string(operation)+"/"+variant, func(t *testing.T) {
				caps := memory.StructuredCapabilities{Get: true, Update: true, ConditionalUpdate: true, Supersede: true, AtomicSupersede: true, Forget: true}
				base := &lifecycleReadProbe{caps: caps}
				var provider memory.StructuredProvider = lifecycleMethodsProbe{base}
				switch variant {
				case "declaration-without-interface":
					provider = base
				case "no-conditional-update":
					base.caps.ConditionalUpdate = false
				case "no-atomic-supersede":
					base.caps.AtomicSupersede = false
				case "false-operation-declaration":
					switch operation {
					case memory.OperationUpdate:
						base.caps.Update = false
					case memory.OperationSupersede:
						base.caps.Supersede = false
					case memory.OperationForget:
						base.caps.Forget = false
					}
				}
				gateway, err := memory.NewGateway(provider, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
				if err != nil {
					t.Fatal(err)
				}
				record := memory.NewRecord{Owner: owner}
				if operation != memory.OperationForget {
					record.Type = memory.TypeFact
					record.Content = "inert"
				}
				preview, err := gateway.Preview(context.Background(), memory.Mutation{Operation: operation, Record: record, ID: "neutral-id", ExpectedVersion: 1})
				if !errors.Is(err, memory.ErrUnsupported) || preview.IntentID() != "" {
					t.Fatalf("unsupported preview: %v %v", preview, err)
				}
				switch operation {
				case memory.OperationUpdate:
					_, err = memory.Update(context.Background(), provider, memory.UpdateRequest{Owner: owner, ID: "neutral-id", ExpectedVersion: 1, Record: record})
				case memory.OperationSupersede:
					_, err = memory.Supersede(context.Background(), provider, memory.UpdateRequest{Owner: owner, ID: "neutral-id", ExpectedVersion: 1, Record: record})
				case memory.OperationForget:
					_, err = memory.Forget(context.Background(), provider, memory.MutationRequest{Owner: owner, ID: "neutral-id", ExpectedVersion: 1})
				}
				if !errors.Is(err, memory.ErrUnsupported) {
					t.Fatalf("preview/dispatch guarantee mismatch: %v", err)
				}
				if base.accesses != 0 {
					t.Fatalf("%d provider read/mutation accesses", base.accesses)
				}
			})
		}
	}
}
func TestLifecyclePreviewAuthorizesAndValidatesBeforeProviderCapabilities(t *testing.T) {
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	gateway, err := memory.NewGateway(forbiddenProvider{}, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []memory.Operation{memory.OperationUpdate, memory.OperationSupersede, memory.OperationForget} {
		record := memory.NewRecord{Owner: owner}
		if operation != memory.OperationForget {
			record.Type = memory.TypeFact
			record.Content = "inert"
		}
		mutation := memory.Mutation{Operation: operation, Record: record, ID: "neutral-id", ExpectedVersion: 1}
		foreign := mutation
		foreign.Record.Owner.UserID = "foreign"
		if _, err := gateway.Preview(context.Background(), foreign); !errors.Is(err, memory.ErrOwnershipDenied) {
			t.Fatalf("wrong owner: %v", err)
		}
		invalid := mutation
		invalid.ExpectedVersion = 0
		if _, err := gateway.Preview(context.Background(), invalid); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("invalid version: %v", err)
		}
		invalid = mutation
		invalid.OperationID = "invalid operation id"
		if _, err := gateway.Preview(context.Background(), invalid); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("invalid operation ID: %v", err)
		}
	}
}
