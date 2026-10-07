package run

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

const (
	ProposalApproved ProposalStatus = "approved"
	ProposalRejected ProposalStatus = "rejected"
)

var (
	ErrProposalCeiling           = errors.New("proposal difference exceeds delegation ceiling")
	ErrProposalExpired           = errors.New("proposal expired")
	ErrProposalDelegationExpired = errors.New("proposal delegation expired")
	ErrProposalStaleBase         = errors.New("proposal base revision is stale")
)
var ErrOperatorUnauthorized = errors.New("operator authority not established")

// OperatorDecisionRequest contains correlation, not caller identity claims.
type OperatorDecisionRequest struct {
	ProposalID     string         `json:"proposal_id"`
	RunID          string         `json:"run_id"`
	Decision       ProposalStatus `json:"decision"`
	DenialAuditID  string         `json:"denial_audit_id"`
	BasePolicyHash string         `json:"base_policy_hash"`
}

// OperatorReceipt is returned only by the trusted host-installed authority port.
// Labels in ordinary runtime requests cannot create this authority.
type OperatorReceipt struct {
	Actor          identity.ActorIdentity `json:"actor"`
	Authorized     bool                   `json:"authorized"`
	AuthorityID    string                 `json:"authority_id"`
	ReceiptID      string                 `json:"receipt_id"`
	ProposalID     string                 `json:"proposal_id"`
	RunID          string                 `json:"run_id"`
	Decision       ProposalStatus         `json:"decision"`
	DenialAuditID  string                 `json:"denial_audit_id"`
	BasePolicyHash string                 `json:"base_policy_hash"`
}

// OperatorAuthority is configured by the trusted host, which must establish an
// authorized human independently of runtime/caller labels. ResolveOperator runs
// inside the policy boundary and Store transaction; it must not re-enter Store.
// The same authority is queried again on use, so receipts are not bearer grants.
type OperatorAuthority interface {
	ResolveOperator(context.Context, OperatorDecisionRequest) (OperatorReceipt, error)
}

// CurrentPolicyBoundary holds the authoritative policy reference stable until
// apply returns. Lock order is policy boundary, then Store transaction. Neither
// callback may re-enter Store. This is validation only, not revision application.
type CurrentPolicyBoundary interface {
	WithCurrentPolicy(context.Context, string, func(policy.Snapshot) error) error
}
type PermissionProposalDecider struct {
	store     *Store
	authority OperatorAuthority
	current   CurrentPolicyBoundary
	clock     func() time.Time
}

// NewPermissionProposalDecider must be called by a trusted delivery host. The
// authority is configuration, never selectable by runtime request fields.
func NewPermissionProposalDecider(s *Store, a OperatorAuthority, current CurrentPolicyBoundary, clock func() time.Time) (*PermissionProposalDecider, error) {
	if s == nil || a == nil {
		return nil, errors.New("store and trusted operator authority are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &PermissionProposalDecider{s, a, current, clock}, nil
}
func operatorRequest(p PermissionProposal, status ProposalStatus) OperatorDecisionRequest {
	return OperatorDecisionRequest{p.ID, p.RunID, status, p.Denial.ID, p.BasePolicy.Hash}
}
func validateOperator(receipt OperatorReceipt, p PermissionProposal, status ProposalStatus) error {
	if !receipt.Authorized || receipt.Actor.Validate() != nil || receipt.Actor.Kind != identity.Human || receipt.Actor.ID == p.Identity.Actor.ID || !identity.IsSafeReference(receipt.AuthorityID) || !identity.IsSafeReference(receipt.ReceiptID) || receipt.ProposalID != p.ID || receipt.RunID != p.RunID || receipt.Decision != status || receipt.DenialAuditID != p.Denial.ID || receipt.BasePolicyHash != p.BasePolicy.Hash {
		return ErrOperatorUnauthorized
	}
	return nil
}

// PermissionProposalDecision is a stable, proposal-bound audit of each attempt.
type PermissionProposalDecision struct {
	ID              string           `json:"id"`
	ProposalID      string           `json:"proposal_id"`
	RequestedStatus ProposalStatus   `json:"requested_status"`
	Outcome         string           `json:"outcome"`
	At              time.Time        `json:"at"`
	Operator        *OperatorReceipt `json:"operator,omitempty"`
}

func (d *PermissionProposalDecider) withCurrent(ctx context.Context, p PermissionProposal, apply func(policy.Snapshot) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.current != nil {
		return d.current.WithCurrentPolicy(ctx, p.RunID, apply)
	}
	// The default has no mutable applied revision. Read before entering update.
	r, err := d.store.Get(p.RunID)
	if err != nil {
		return err
	}
	return apply(r.AppliedPolicy())
}
func (d *PermissionProposalDecider) Decide(ctx context.Context, id string, status ProposalStatus) (PermissionProposal, error) {
	if status != ProposalApproved && status != ProposalRejected {
		return PermissionProposal{}, errors.New("invalid proposal decision")
	}
	p, err := d.store.GetPermissionProposal(id)
	if err != nil {
		return p, err
	}
	var decisionErr error
	err = d.withCurrent(ctx, p, func(base policy.Snapshot) error {
		return d.store.update(func(db *database) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, ok := db.Proposals[id]
			if !ok {
				return errors.New("permission proposal not found")
			}
			if current.Status != ProposalPending {
				return errors.New("proposal already decided")
			}
			if d.current == nil {
				base = db.Runs[current.RunID].AppliedPolicy()
			}
			receipt, authorityErr := d.authority.ResolveOperator(ctx, operatorRequest(current, status))
			if err := ctx.Err(); err != nil {
				return err
			}
			now := d.clock().UTC()
			if now.IsZero() || now.Before(current.CreatedAt) {
				return errors.New("invalid proposal decision clock")
			}
			decisionID, e := newID("proposal-decision")
			if e != nil {
				return e
			}
			audit := PermissionProposalDecision{ID: decisionID, ProposalID: id, RequestedStatus: status, At: now, Outcome: string(status)}
			if authorityErr != nil || validateOperator(receipt, current, status) != nil {
				decisionErr = ErrOperatorUnauthorized
				audit.Outcome = "operator_unauthorized"
			} else {
				receipt.Actor = receipt.Actor.Normalized()
				audit.Operator = &receipt
				decisionErr = validateLiveProposal(current, db.Runs[current.RunID], base, now)
				if !samePolicySnapshot(base, db.Runs[current.RunID].AppliedPolicy()) {
					decisionErr = ErrProposalStaleBase
				}
				if decisionErr != nil {
					audit.Outcome = decisionErr.Error()
				} else {
					current.Status = status
				}
			}
			current.Decisions = append(current.Decisions, audit)
			db.Proposals[id] = current
			p = current
			return nil
		})
	})
	if err != nil {
		return p, err
	}
	return p, decisionErr
}
func validateProposalDecisions(p PermissionProposal) error {
	terminal := ProposalPending
	lastAt := p.CreatedAt
	seen := map[string]bool{}
	for _, audit := range p.Decisions {
		if !safeID.MatchString(audit.ID) || seen[audit.ID] || audit.ProposalID != p.ID || (audit.RequestedStatus != ProposalApproved && audit.RequestedStatus != ProposalRejected) || audit.At.Before(lastAt) || terminal != ProposalPending {
			return errors.New("invalid proposal decision audit")
		}
		seen[audit.ID] = true
		lastAt = audit.At
		switch audit.Outcome {
		case "operator_unauthorized":
			if audit.Operator != nil {
				return errors.New("unauthorized decision invents authority")
			}
		case ErrProposalExpired.Error(), ErrProposalDelegationExpired.Error(), ErrProposalStaleBase.Error(), ErrProposalCeiling.Error():
			if audit.Operator == nil || validateOperator(*audit.Operator, p, audit.RequestedStatus) != nil {
				return errors.New("failed decision missing established operator")
			}
		case "approved", "rejected":
			if audit.Operator == nil || validateOperator(*audit.Operator, p, audit.RequestedStatus) != nil {
				return errors.New("approved decision missing authority")
			}
			if string(audit.RequestedStatus) != audit.Outcome {
				return errors.New("decision outcome mismatches request")
			}
			terminal = audit.RequestedStatus
		default:
			return errors.New("invalid proposal decision outcome")
		}
	}
	if p.Status != terminal {
		return errors.New("proposal status does not match decision audit")
	}
	return nil
}

func validateLiveProposal(p PermissionProposal, r Record, base policy.Snapshot, now time.Time) error {
	if !now.Before(p.ExpiresAt) {
		return ErrProposalExpired
	}
	if r.Identity.Mode != identity.ModeNamed || r.Identity.Delegation == nil || !now.Before(r.Identity.Delegation.ExpiresAt) {
		return ErrProposalDelegationExpired
	}
	if base.Validate() != nil || base.PolicyID != p.BasePolicy.PolicyID || base.Version != p.BasePolicy.Version || base.Hash != p.BasePolicy.Hash || !base.ResolvedAt.Equal(p.BasePolicy.ResolvedAt) {
		return ErrProposalStaleBase
	}
	return validateDifferenceCeiling(p, r, now)
}

// ValidateUse validates eligibility for a later confirmed revision application.
// Success does not apply a policy, authorize retry, consume an Approval or resume.
func (d *PermissionProposalDecider) ValidateUse(ctx context.Context, id string) error {
	p, err := d.store.GetPermissionProposal(id)
	if err != nil {
		return err
	}
	return d.withCurrent(ctx, p, func(base policy.Snapshot) error {
		return d.store.update(func(db *database) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			p, ok := db.Proposals[id]
			if !ok || p.Status != ProposalApproved {
				return errors.New("proposal is not approved")
			}
			receipt, err := d.authority.ResolveOperator(ctx, operatorRequest(p, ProposalApproved))
			if err != nil || validateOperator(receipt, p, ProposalApproved) != nil {
				return ErrOperatorUnauthorized
			}
			original := p.Decisions[len(p.Decisions)-1].Operator
			if original == nil || receipt.AuthorityID != original.AuthorityID || !reflect.DeepEqual(receipt.Actor.Normalized(), original.Actor.Normalized()) {
				return ErrOperatorUnauthorized
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.current == nil {
				base = db.Runs[p.RunID].AppliedPolicy()
			}
			now := d.clock().UTC()
			if now.IsZero() || now.Before(p.CreatedAt) {
				return errors.New("invalid proposal use clock")
			}
			if !samePolicySnapshot(base, db.Runs[p.RunID].AppliedPolicy()) {
				return ErrProposalStaleBase
			}
			return validateLiveProposal(p, db.Runs[p.RunID], base, now)
		})
	})
}
