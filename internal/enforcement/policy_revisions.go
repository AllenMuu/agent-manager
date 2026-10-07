package enforcement

import (
	"context"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// PolicyMutation binds a single approved difference to a prepared runtime.
// Operation.Policy remains the original run authority; Base and Target are the
// separate immutable revisions used for this update.
type PolicyMutation struct {
	Operation  Operation       `json:"operation"`
	ID         string          `json:"id"`
	ProposalID string          `json:"proposal_id"`
	DecisionID string          `json:"decision_id"`
	ReceiptID  string          `json:"receipt_id"`
	Base       policy.Snapshot `json:"base"`
	Target     policy.Snapshot `json:"target"`
}
type PolicyReceipt struct {
	Mutation PolicyMutation `json:"mutation"`
	EventID  string         `json:"event_id"`
	State    string         `json:"state"`
}
type PolicyApplier interface {
	ApplyPolicy(context.Context, PolicyMutation) (PolicyReceipt, error)
}
type PolicyRevisionQuerier interface {
	QueryPolicy(context.Context, PolicyMutation) (PolicyReceipt, error)
}
