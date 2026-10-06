package enforcement

import (
	"context"
	"fmt"
	"sync"

	"github.com/AllenMuu/skill-manager/internal/policy"
)

// MockProvider is deterministic offline evidence, never a sandbox or model.
// Its receipts survive coordinator replacement while this fixture is retained.
type MockProvider struct {
	mu        sync.Mutex
	id        string
	receipts  map[string]Acknowledgement
	resources map[string]string
	calls     []Operation
	faults    map[string]mockFault
}
type mockFault struct {
	err    error
	commit bool
}

func NewMockProvider(id string) *MockProvider {
	return &MockProvider{id: id, receipts: map[string]Acknowledgement{}, resources: map[string]string{}, faults: map[string]mockFault{}}
}
func (m *MockProvider) ID() string { return m.id }
func (m *MockProvider) Declaration() Declaration {
	return Declaration{Name: m.id, Kind: "execution", Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true, policy.ControlRunTermination: true}, Controls: map[Dimension]Capability{Tool: {Support: "supported", Update: "unsupported", Verified: true}}}
}

// FailNext injects an external boundary fault, optionally after the mutation.
// A committed fault leaves a queryable receipt; an uncommitted unknown fault
// leaves the outcome unresolved until SetOutcome supplies explicit evidence.
func (m *MockProvider) FailNext(action string, err error, commit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.faults[action] = mockFault{err, commit}
}
func (m *MockProvider) Calls() []Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Operation(nil), m.calls...)
}
func (m *MockProvider) ResourceCount() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.resources) }
func (m *MockProvider) SetOutcome(op Operation, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receipts[op.ID] = Acknowledgement{op, state}
}
func (m *MockProvider) Query(ctx context.Context, op Operation) (Acknowledgement, error) {
	if err := ctx.Err(); err != nil {
		return Acknowledgement{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.receipts[op.ID]
	if !ok {
		return Acknowledgement{}, ErrUnknown
	}
	return a, nil
}
func (m *MockProvider) Prepare(ctx context.Context, op Operation) (Acknowledgement, error) {
	return m.apply(ctx, op, "prepared")
}
func (m *MockProvider) Start(ctx context.Context, op Operation) (Acknowledgement, error) {
	return m.apply(ctx, op, "active")
}
func (m *MockProvider) Terminate(ctx context.Context, op Operation) (Acknowledgement, error) {
	return m.apply(ctx, op, "terminated")
}
func (m *MockProvider) Pause(ctx context.Context, op Operation) (Acknowledgement, error) {
	return m.apply(ctx, op, "paused")
}
func (m *MockProvider) Resume(ctx context.Context, op Operation) (Acknowledgement, error) {
	return m.apply(ctx, op, "active")
}
func (m *MockProvider) apply(ctx context.Context, op Operation, state string) (Acknowledgement, error) {
	if err := ctx.Err(); err != nil {
		return Acknowledgement{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.receipts[op.ID]; ok {
		return old, nil
	}
	m.calls = append(m.calls, op)
	fault := m.faults[op.Action]
	delete(m.faults, op.Action)
	if fault.err != nil && !fault.commit {
		return Acknowledgement{}, fault.err
	}
	if op.Action == "prepare" {
		op.Handle = "mock-" + op.RunID
		op.Generation = "generation-1"
	} else if _, ok := m.resources[op.Handle]; !ok {
		return Acknowledgement{}, fmt.Errorf("%w: handle missing", ErrRefused)
	}
	m.resources[op.Handle] = state
	a := Acknowledgement{Operation: op, State: state}
	m.receipts[op.ID] = a
	if fault.err != nil {
		return Acknowledgement{}, fault.err
	}
	return a, nil
}
