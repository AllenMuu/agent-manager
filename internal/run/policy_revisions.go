package run

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// PolicyRevisionHistory is additive. Policy on Record and lifecycle requests
// always retains the original authority, including after revision application.
type PolicyRevisionHistory struct {
	Events    []PolicyEventObservation `json:"events,omitempty"`
	Version   string                   `json:"version"`
	Desired   policy.Snapshot          `json:"desired"`
	Applied   policy.Snapshot          `json:"applied"`
	Mutations []PolicyRevisionMutation `json:"mutations"`
}
type PolicyRevisionMutation struct {
	Acceptance         *enforcement.PolicyReceipt `json:"acceptance,omitempty"`
	RetryAttempts      []PolicyRetry              `json:"retry_attempts,omitempty"`
	ConfirmationOrigin string                     `json:"confirmation_origin,omitempty"`
	ConfirmationTrust  string                     `json:"confirmation_trust,omitempty"`
	Retry              *PolicyRetry               `json:"retry,omitempty"`
	Request            enforcement.PolicyMutation `json:"request"`
	State              string                     `json:"state"`
	Confirmation       *enforcement.PolicyReceipt `json:"confirmation,omitempty"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}
type PolicyRevisionStore interface {
	BeginPolicyRevision(context.Context, string, *PermissionProposalDecider) (Record, bool, error)
	FinishPolicyRevision(string, string, *enforcement.PolicyReceipt, string) (Record, error)
}

func (r Record) AppliedPolicy() policy.Snapshot {
	if r.PolicyRevisions != nil {
		return r.PolicyRevisions.Applied
	}
	return r.Policy
}
func (r Record) policySnapshot(hash string, at time.Time) (policy.Snapshot, bool) {
	if r.Policy.Hash == hash && r.Policy.ResolvedAt.Equal(at) {
		return r.Policy, true
	}
	if r.PolicyRevisions != nil {
		for _, m := range r.PolicyRevisions.Mutations {
			if m.State == "applied" && m.Request.Target.Hash == hash && m.Request.Target.ResolvedAt.Equal(at) {
				return m.Request.Target, true
			}
		}
	}
	return policy.Snapshot{}, false
}
func (d *PermissionProposalDecider) validateUseLocked(ctx context.Context, p PermissionProposal, r Record, base policy.Snapshot, now time.Time) error {
	if p.Status != ProposalApproved {
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
	return validateLiveProposal(p, r, base, now)
}
func applyDifference(base policy.Snapshot, p PermissionProposal, at time.Time) (policy.Snapshot, error) {
	raw, err := json.Marshal(base.Policy)
	if err != nil {
		return policy.Snapshot{}, err
	}
	var next policy.AgentPolicy
	if err = json.Unmarshal(raw, &next); err != nil {
		return policy.Snapshot{}, err
	}
	d := p.Difference
	remove := func(v []string, key string) []string {
		return slices.DeleteFunc(v, func(s string) bool { return s == key })
	}
	switch d.Category {
	case policy.NetworkAccessRequested:
		if next.Network == nil {
			return policy.Snapshot{}, errors.New("missing network rules")
		}
		next.Network.DeniedDomains = remove(next.Network.DeniedDomains, d.Domain)
		if !slices.Contains(next.Network.AllowedDomains, d.Domain) {
			next.Network.AllowedDomains = append(next.Network.AllowedDomains, d.Domain)
		}
	case policy.CredentialAccessRequested:
		if next.Credentials == nil {
			return policy.Snapshot{}, errors.New("missing credential rules")
		}
		next.Credentials.DeniedScopes = remove(next.Credentials.DeniedScopes, d.CredentialScope)
		if !slices.Contains(next.Credentials.AllowedScopes, d.CredentialScope) {
			next.Credentials.AllowedScopes = append(next.Credentials.AllowedScopes, d.CredentialScope)
		}
	case policy.ToolCallRequested:
		next.Tools.Deny = remove(next.Tools.Deny, d.Tool)
		if !slices.Contains(next.Tools.Allow, d.Tool) {
			next.Tools.Allow = append(next.Tools.Allow, d.Tool)
		}
	default:
		return policy.Snapshot{}, errors.New("unsupported permission difference")
	}
	return policy.Resolve(next, at)
}
func (s *Store) BeginPolicyRevision(ctx context.Context, id string, d *PermissionProposalDecider) (Record, bool, error) {
	if d == nil || d.store != s {
		return Record{}, false, errors.New("revision requires this store's trusted decider")
	}
	p, err := s.GetPermissionProposal(id)
	if err != nil {
		return Record{}, false, err
	}
	var result Record
	created := false
	// An external current boundary holds its lock before the Store transaction.
	apply := func(external *policy.Snapshot) error {
		return s.update(func(db *database) error {
			p, ok := db.Proposals[id]
			if !ok {
				return errors.New("proposal not found")
			}
			r := db.Runs[p.RunID]
			result = r
			if r.PolicyRevisions != nil {
				for _, m := range r.PolicyRevisions.Mutations {
					if m.Request.ProposalID == id {
						return nil
					}
				}
				if !samePolicySnapshot(r.PolicyRevisions.Desired, r.PolicyRevisions.Applied) {
					return enforcement.ErrUnknown
				}
			}
			if r.ExternalRuntime == nil || r.Status != Paused || unresolved(r) {
				return errors.New("revision requires confirmed paused runtime")
			}
			base := r.AppliedPolicy()
			if external != nil && !samePolicySnapshot(base, *external) {
				return ErrProposalStaleBase
			}
			now := d.clock().UTC()
			if now.IsZero() || now.Before(p.CreatedAt) {
				return errors.New("invalid revision clock")
			}
			if err := d.validateUseLocked(ctx, p, r, base, now); err != nil {
				return err
			}
			target, err := applyDifference(base, p, now)
			if err != nil {
				return err
			}
			opID, err := newID("policy-op")
			if err != nil {
				return err
			}
			decision := p.Decisions[len(p.Decisions)-1]
			op := enforcement.PolicyMutation{Operation: lifecycleRequest(r, opID, "policy_update"), ID: opID, ProposalID: id, DecisionID: decision.ID, ReceiptID: decision.Operator.ReceiptID, Base: base, Target: target}
			if r.PolicyRevisions == nil {
				r.PolicyRevisions = &PolicyRevisionHistory{Version: "v1", Desired: base, Applied: base}
			}
			r.PolicyRevisions.Desired = target
			r.PolicyRevisions.Mutations = append(r.PolicyRevisions.Mutations, PolicyRevisionMutation{Request: op, State: "pending", CreatedAt: now, UpdatedAt: now})
			db.Runs[r.ID] = r
			result = r
			created = true
			return nil
		})
	}
	if d.current != nil {
		err = d.current.WithCurrentPolicy(ctx, p.RunID, func(base policy.Snapshot) error { return apply(&base) })
	} else {
		err = apply(nil)
	}
	return result, created, err
}
func validatePolicyReceipt(want enforcement.PolicyMutation, got enforcement.PolicyReceipt) error {
	if !samePolicyMutation(want, got.Mutation) || !safeID.MatchString(got.EventID) || (got.State != "applied" && got.State != "accepted") {
		return errors.New("policy receipt correlation mismatch")
	}
	return nil
}
func (s *Store) FinishPolicyRevision(id, opID string, receipt *enforcement.PolicyReceipt, state string) (Record, error) {
	var result Record
	err := s.update(func(db *database) error {
		r, ok := db.Runs[id]
		if !ok || r.PolicyRevisions == nil {
			return errors.New("revision not found")
		}
		h := r.PolicyRevisions
		idx := len(h.Mutations) - 1
		m := &h.Mutations[idx]
		if m.Request.ID != opID {
			return errors.New("stale revision operation")
		}
		if receipt != nil {
			if err := validatePolicyReceipt(m.Request, *receipt); err != nil {
				return err
			}
		}
		if m.State == "applied" {
			result = r
			return nil
		}
		if state != "pending" && state != "applied" && state != "failed" && state != "unknown" {
			return errors.New("invalid application state")
		}
		if state == "pending" && receipt != nil && receipt.State == "accepted" {
			m.Acceptance = receipt
		}
		if state == "applied" {
			if receipt == nil || receipt.State != "applied" {
				return errors.New("application requires exact receipt")
			}
			h.Applied = m.Request.Target
			m.Confirmation = receipt
			m.ConfirmationOrigin = "runtime"
			m.ConfirmationTrust = "established"
		}
		m.State = state
		m.UpdatedAt = time.Now().UTC()
		if m.UpdatedAt.Before(m.CreatedAt) {
			m.UpdatedAt = m.CreatedAt
		}
		db.Runs[id] = r
		result = r
		return nil
	})
	return result, err
}
func (c *Coordinator) ApplyPolicyRevision(ctx context.Context, id string, d *PermissionProposalDecider) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if d == nil {
		return Record{}, errors.New("trusted decider required")
	}
	store, ok := c.store.(PolicyRevisionStore)
	if !ok {
		return Record{}, enforcement.ErrUnsupported
	}
	p, err := d.store.GetPermissionProposal(id)
	if err != nil {
		return Record{}, err
	}
	r, err := c.store.Get(p.RunID)
	if err != nil {
		return r, err
	}
	provider, err := c.provider(r)
	if err != nil {
		return r, err
	}
	dimension := enforcement.Tool
	switch p.Difference.Category {
	case policy.NetworkAccessRequested:
		dimension = enforcement.Network
	case policy.CredentialAccessRequested:
		dimension = enforcement.Credential
	}
	capability := provider.Declaration().Controls[dimension]
	if capability.Support != "supported" || !capability.Verified || capability.Update != "live-update" {
		return r, enforcement.ErrUnsupported
	}
	applier, ok := provider.(enforcement.PolicyApplier)
	if !ok {
		return r, enforcement.ErrUnsupported
	}
	r, created, err := store.BeginPolicyRevision(ctx, id, d)
	if err != nil || !created {
		return r, err
	}
	m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1].Request
	request, err := clonePolicyMutation(m)
	if err != nil {
		return r, err
	}
	receipt, err := applier.ApplyPolicy(ctx, request)
	return c.finishPolicyRevision(r, m, receipt, err)
}
func (c *Coordinator) finishPolicyRevision(r Record, m enforcement.PolicyMutation, receipt enforcement.PolicyReceipt, providerErr error) (Record, error) {
	state := "applied"
	var evidence *enforcement.PolicyReceipt = &receipt
	if providerErr != nil || validatePolicyReceipt(m, receipt) != nil {
		state = "unknown"
		evidence = nil
		if (providerErr == nil && receipt.Mutation.ID != "") || errors.Is(providerErr, enforcement.ErrRefused) || errors.Is(providerErr, enforcement.ErrUnsupported) {
			state = "failed"
		}
		if errors.Is(providerErr, enforcement.ErrRefused) {
			providerErr = enforcement.ErrRefused
		} else if errors.Is(providerErr, enforcement.ErrUnsupported) {
			providerErr = enforcement.ErrUnsupported
		} else {
			providerErr = enforcement.ErrUnknown
		}
	} else if receipt.State == "accepted" {
		state = "pending"
	}
	updated, err := c.store.(PolicyRevisionStore).FinishPolicyRevision(r.ID, m.ID, evidence, state)
	if err != nil {
		return r, errors.Join(providerErr, err)
	}
	return updated, providerErr
}
func (c *Coordinator) ReconcilePolicyRevision(ctx context.Context, id string) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	r, err := c.store.Get(id)
	if err != nil {
		return r, err
	}
	if r.PolicyRevisions == nil {
		return r, errors.New("revision not found")
	}
	m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
	if m.State == "applied" {
		return r, nil
	}
	p, err := c.provider(r)
	if err != nil {
		return r, err
	}
	q, ok := p.(enforcement.PolicyRevisionQuerier)
	if !ok {
		return r, enforcement.ErrUnsupported
	}
	request, err := clonePolicyMutation(m.Request)
	if err != nil {
		return r, err
	}
	receipt, err := q.QueryPolicy(ctx, request)
	return c.finishPolicyRevision(r, m.Request, receipt, err)
}
func clonePolicyMutation(m enforcement.PolicyMutation) (enforcement.PolicyMutation, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	var result enforcement.PolicyMutation
	err = json.Unmarshal(raw, &result)
	return result, err
}
func validatePolicyRevisions(r Record) error {
	h := r.PolicyRevisions
	if h == nil {
		return nil
	}
	if h.Version != "v1" || len(h.Mutations) == 0 {
		return errors.New("invalid revision history")
	}
	base := r.Policy
	applied := r.Policy
	seen := map[string]bool{}
	for _, m := range h.Mutations {
		op := m.Request
		if seen[op.ID] || !safeID.MatchString(op.ID) || op.ID != op.Operation.ID || op.Operation.Action != "policy_update" || op.Operation.RunID != r.ID || r.ExternalRuntime == nil || op.Operation.ProviderID != r.ExternalRuntime.ProviderID || op.Operation.Handle != r.ExternalRuntime.Handle || op.Operation.Generation != r.ExternalRuntime.Generation || !samePolicySnapshot(op.Operation.Policy, r.Policy) || !reflect.DeepEqual(op.Operation.Identity, r.Identity) || !samePolicySnapshot(op.Base, base) || op.Target.Validate() != nil || op.Target.Hash == base.Hash || m.CreatedAt.IsZero() || m.UpdatedAt.Before(m.CreatedAt) {
			return errors.New("invalid policy mutation lineage")
		}
		seen[op.ID] = true
		if m.Acceptance != nil && (m.Acceptance.State != "accepted" || validatePolicyReceipt(op, *m.Acceptance) != nil) {
			return errors.New("invalid policy acceptance evidence")
		}
		switch m.State {
		case "applied":
			if !validPolicyOrigin(m.ConfirmationOrigin) || m.ConfirmationTrust != "established" || m.Confirmation == nil || m.Confirmation.State != "applied" || validatePolicyReceipt(op, *m.Confirmation) != nil {
				return errors.New("missing revision confirmation")
			}
			applied = op.Target
			base = op.Target
		case "pending", "failed", "unknown":
			if m.Confirmation != nil {
				return errors.New("unconfirmed revision has confirmation")
			}
		default:
			return errors.New("invalid policy mutation state")
		}
	}
	for _, event := range h.Events {
		if !identity.IsSafeReference(event.ID) || !identity.IsSafeReference(event.SourceID) || (event.EventID != "" && !identity.IsSafeReference(event.EventID)) || !validPolicyOrigin(event.Origin) || (event.Trust != "established" && event.Trust != "untrusted") {
			return errors.New("invalid policy event diagnostic")
		}
		switch event.Outcome {
		case "untrusted", "stale", "duplicate", "accepted", "applied":
		default:
			return errors.New("invalid policy event outcome")
		}
	}
	if !samePolicySnapshot(h.Desired, h.Mutations[len(h.Mutations)-1].Request.Target) || !samePolicySnapshot(h.Applied, applied) {
		return errors.New("invalid desired/applied revision")
	}
	return nil
}

func samePolicySnapshot(a, b policy.Snapshot) bool {
	return a.Validate() == nil && b.Validate() == nil && a.PolicyID == b.PolicyID && a.Version == b.Version && a.Hash == b.Hash && a.ResolvedAt.Equal(b.ResolvedAt)
}

func samePolicyMutation(a, b enforcement.PolicyMutation) bool {
	if !samePolicySnapshot(a.Base, b.Base) || !samePolicySnapshot(a.Target, b.Target) || !samePolicySnapshot(a.Operation.Policy, b.Operation.Policy) {
		return false
	}
	a.Base = b.Base
	a.Target = b.Target
	a.Operation.Policy = b.Operation.Policy
	return reflect.DeepEqual(a, b)
}

func auditResolvesPolicy(r Record, a AuditRecord) bool {
	snapshot, found := r.policySnapshot(a.PolicyHash, a.PolicyResolvedAt)
	return found && snapshot.PolicyID == a.PolicyID && snapshot.Version == a.PolicyVersion
}

func snapshotForAudit(r Record, a AuditRecord) policy.Snapshot {
	snapshot, _ := r.policySnapshot(a.PolicyHash, a.PolicyResolvedAt)
	return snapshot
}

func (r Record) policyRevisionBlocked() bool {
	if r.PolicyRevisions == nil {
		return false
	}
	m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
	return m.State != "applied" || m.Retry == nil || m.Retry.State != "succeeded"
}

func sameAuditPolicyReference(a, b AuditRecord) bool {
	return a.PolicyID == b.PolicyID && a.PolicyVersion == b.PolicyVersion && a.PolicyHash == b.PolicyHash && a.PolicyResolvedAt.Equal(b.PolicyResolvedAt)
}
