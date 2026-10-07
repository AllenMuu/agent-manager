package enforcement

import (
	"context"
	"encoding/json"
	"errors"
)

// ApplyPolicy and QueryPolicy are offline fixture contracts. They do not launch
// a model, contact a network, resolve credentials or install runtime protection.
func (m *MockProvider) ApplyPolicy(ctx context.Context, op PolicyMutation) (PolicyReceipt, error) {
	if err := ctx.Err(); err != nil {
		return PolicyReceipt{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.policyReceipts[op.ID]; ok {
		return copyMockPolicyReceipt(old)
	}
	if op.Target.Validate() != nil || op.Base.Validate() != nil || op.Operation.Policy.Validate() != nil || op.ID != op.Operation.ID || op.Operation.ProviderID != m.id || op.Operation.Action != "policy_update" || m.resources[op.Operation.Handle] != "paused" || op.Operation.Generation != "generation-1" || op.Operation.Handle != "mock-"+op.Operation.RunID || m.activePolicies[op.Operation.Handle].Hash != op.Base.Hash || !m.activePolicies[op.Operation.Handle].ResolvedAt.Equal(op.Base.ResolvedAt) {
		return PolicyReceipt{}, ErrRefused
	}
	copied, err := copyMockPolicyReceipt(PolicyReceipt{Mutation: op, EventID: op.ID + "-applied", State: "applied"})
	if err != nil {
		return PolicyReceipt{}, err
	}
	m.policyCalls = append(m.policyCalls, copied.Mutation)
	fault := m.faults["policy_update"]
	delete(m.faults, "policy_update")
	if fault.err != nil && !fault.commit {
		return PolicyReceipt{}, fault.err
	}
	m.policyReceipts[op.ID] = copied
	m.activePolicies[op.Operation.Handle] = copied.Mutation.Target
	if fault.err != nil {
		return PolicyReceipt{}, fault.err
	}
	return copyMockPolicyReceipt(copied)
}
func (m *MockProvider) QueryPolicy(ctx context.Context, op PolicyMutation) (PolicyReceipt, error) {
	if err := ctx.Err(); err != nil {
		return PolicyReceipt{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	receipt, ok := m.policyReceipts[op.ID]
	if !ok || m.activePolicies[op.Operation.Handle].Hash != receipt.Mutation.Target.Hash || !m.activePolicies[op.Operation.Handle].ResolvedAt.Equal(receipt.Mutation.Target.ResolvedAt) {
		return PolicyReceipt{}, ErrUnknown
	}
	return copyMockPolicyReceipt(receipt)
}
func (m *MockProvider) PolicyCalls() []PolicyMutation {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]PolicyMutation, 0, len(m.policyCalls))
	for _, op := range m.policyCalls {
		receipt, err := copyMockPolicyReceipt(PolicyReceipt{Mutation: op})
		if err == nil {
			result = append(result, receipt.Mutation)
		}
	}
	return result
}
func copyMockPolicyReceipt(receipt PolicyReceipt) (PolicyReceipt, error) {
	data, err := json.Marshal(receipt)
	if err != nil {
		return PolicyReceipt{}, errors.New("invalid mock policy receipt")
	}
	var copied PolicyReceipt
	if err = json.Unmarshal(data, &copied); err != nil {
		return PolicyReceipt{}, errors.New("invalid mock policy receipt")
	}
	return copied, nil
}

var _ PolicyApplier = (*MockProvider)(nil)
var _ PolicyRevisionQuerier = (*MockProvider)(nil)
