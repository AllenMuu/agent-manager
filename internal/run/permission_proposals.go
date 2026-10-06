package run

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

type ProposalStatus string

const ProposalPending ProposalStatus = "pending"

// PermissionDifference describes one exact capability, never a wildcard or
// an entire replacement policy. All fields are bound to the persisted denial.
type PermissionDifference struct {
	Category        policy.EventCategory `json:"category"`
	ActionID        string               `json:"action_id"`
	ActionType      string               `json:"action_type,omitempty"`
	Tool            string               `json:"tool,omitempty"`
	Domain          string               `json:"domain,omitempty"`
	CredentialScope string               `json:"credential_scope,omitempty"`
}

// PermissionProposalRequest requests one denial-bound capability difference.
type PermissionProposalRequest struct {
	RunID         string
	DenialAuditID string
	Difference    PermissionDifference
	ExpiresAt     time.Time
}

// PermissionProposal is separate from one-action Approval. Its decision never
// changes a run snapshot or dispatches/resumes an action.
type PermissionProposal struct {
	ID         string                       `json:"id"`
	RunID      string                       `json:"run_id"`
	Denial     AuditRecord                  `json:"denial"`
	Identity   identity.Selection           `json:"identity"`
	BasePolicy policy.Snapshot              `json:"base_policy"`
	Difference PermissionDifference         `json:"difference"`
	CreatedAt  time.Time                    `json:"created_at"`
	ExpiresAt  time.Time                    `json:"expires_at"`
	Status     ProposalStatus               `json:"status"`
	Decisions  []PermissionProposalDecision `json:"decisions,omitempty"`
}

// RequestPermissionProposal verifies a persisted attributable permission denial
// and records an expiring proposal within the existing delegation ceiling.
func (m *Manager) RequestPermissionProposal(request PermissionProposalRequest, now time.Time) (PermissionProposal, error) {
	if now.IsZero() || !now.Before(request.ExpiresAt) {
		return PermissionProposal{}, errors.New("proposal expiry must be after creation")
	}
	id, err := newID("proposal")
	if err != nil {
		return PermissionProposal{}, err
	}
	var result PermissionProposal
	err = m.Store.update(func(db *database) error {
		record, ok := db.Runs[request.RunID]
		if !ok {
			return errors.New("proposal run not found")
		}
		denial, ok := auditRecordByID(db.Events, request.DenialAuditID)
		if !ok {
			return errors.New("persisted denial not found")
		}
		result = PermissionProposal{ID: id, RunID: record.ID, Denial: denial, Identity: record.Identity, BasePolicy: record.Policy, Difference: request.Difference, CreatedAt: now.UTC(), ExpiresAt: request.ExpiresAt.UTC(), Status: ProposalPending}
		if err := validatePermissionProposal(*db, result); err != nil {
			return err
		}
		if !now.Before(record.Identity.Delegation.ExpiresAt) {
			return ErrProposalDelegationExpired
		}
		if err := validateDifferenceCeiling(result, record, now); err != nil {
			return err
		}
		if db.Proposals == nil {
			db.Proposals = map[string]PermissionProposal{}
		}
		db.Proposals[id] = result
		return nil
	})
	return result, err
}

// GetPermissionProposal returns proposal lineage and its decision audit.
func (s *Store) GetPermissionProposal(id string) (PermissionProposal, error) {
	if err := validateID("proposal", id); err != nil {
		return PermissionProposal{}, err
	}
	db, err := s.read()
	if err != nil {
		return PermissionProposal{}, err
	}
	p, ok := db.Proposals[id]
	if !ok {
		return PermissionProposal{}, errors.New("permission proposal not found")
	}
	return p, nil
}
func validatePermissionProposal(db database, p PermissionProposal) error {
	r, ok := db.Runs[p.RunID]
	if !ok || !safeID.MatchString(p.ID) || p.CreatedAt.IsZero() || !p.CreatedAt.Before(p.ExpiresAt) || (p.Status != ProposalPending && p.Status != ProposalApproved && p.Status != ProposalRejected) {
		return errors.New("invalid permission proposal")
	}
	if r.Identity.Mode != identity.ModeNamed || !reflect.DeepEqual(p.Identity, r.Identity) || !reflect.DeepEqual(p.BasePolicy, r.Policy) {
		return errors.New("proposal authority does not match run")
	}
	a, ok := auditRecordByID(db.Events, p.Denial.ID)
	if !ok || !reflect.DeepEqual(a, p.Denial) || a.RunID != p.RunID || a.Decision != policy.Deny || a.ApprovalID != "" || a.RequestAuditID != "" || a.ActorID != r.Identity.Actor.ID || a.DelegationID != r.Identity.Delegation.ID || a.Timestamp.After(p.CreatedAt) {
		return errors.New("proposal requires its original run-bound denial")
	}
	if err := validateProposalDecisions(p); err != nil {
		return err
	}
	d := p.Difference
	if d.Category != a.Category || d.ActionID == "" || d.ActionID != a.ActionID || d.ActionType != a.ActionType || d.Tool != a.Tool || d.Domain != a.Domain || d.CredentialScope != a.CredentialScope {
		return errors.New("difference does not match denied action")
	}
	event := policy.Event{Category: d.Category, Domain: d.Domain, Tool: d.Tool, CredentialScope: d.CredentialScope, ActionID: d.ActionID, ActionType: d.ActionType}
	if err := event.Validate(); err != nil {
		return err
	}
	switch d.Category {
	case policy.NetworkAccessRequested:
		if d.Tool != "" || d.CredentialScope != "" || (a.ReasonCode != policy.ReasonDomainDenied && a.ReasonCode != policy.ReasonDomainNotAllowed) {
			return errors.New("denial is not a network permission denial")
		}
		if canonical, err := normalizeAuditDomain(d.Domain); err != nil || canonical != d.Domain {
			return errors.New("difference domain must be canonical")
		}
		if err := identity.ValidateScopes("network difference", []string{"network:" + d.Domain}); err != nil {
			return fmt.Errorf("invalid network difference: %w", err)
		}
	case policy.CredentialAccessRequested:
		if d.Tool != "" || d.Domain != "" || d.CredentialScope == "" || (a.ReasonCode != policy.ReasonCredentialDenied && a.ReasonCode != policy.ReasonCredentialNotAllowed) {
			return errors.New("denial is not an exact credential permission denial")
		}
		if err := identity.ValidateScopes("credential difference", []string{d.CredentialScope}); err != nil {
			return err
		}
	case policy.ToolCallRequested:
		if d.Domain != "" || d.CredentialScope != "" || (a.ReasonCode != policy.ReasonToolDenied && a.ReasonCode != policy.ReasonToolNotAllowlisted) {
			return errors.New("denial is not a tool permission denial")
		}
	default:
		return errors.New("unsupported permission difference")
	}

	return nil
}

func validateDifferenceCeiling(p PermissionProposal, r Record, now time.Time) error {
	if r.Identity.Mode != identity.ModeNamed || r.Identity.Delegation == nil {
		return ErrProposalCeiling
	}
	if p.Difference.Category == policy.ToolCallRequested {
		if r.Policy.Policy.Identity == nil {
			return ErrProposalCeiling
		}
		for _, rule := range r.Policy.Policy.Identity.Rules {
			if rule.ActionID != p.Difference.ActionID {
				continue
			}
			if len(rule.RequiredScopes) == 0 {
				return ErrProposalCeiling
			}
			kindMatches := false
			for _, kind := range rule.ActorKinds {
				if kind == string(r.Identity.Actor.Kind) {
					kindMatches = true
				}
			}
			roleMatches := len(rule.Roles) == 0
			for _, role := range rule.Roles {
				for _, actual := range r.Identity.Actor.Roles {
					if role == actual {
						roleMatches = true
					}
				}
			}
			if !kindMatches || !roleMatches {
				return ErrProposalCeiling
			}
			for _, scope := range rule.RequiredScopes {
				if !r.Identity.Delegation.HasScope(scope, now) {
					return ErrProposalCeiling
				}
			}
			return nil
		}
		return ErrProposalCeiling
	}
	if !r.Identity.Delegation.HasScope(differenceScope(p.Difference), now) {
		return ErrProposalCeiling
	}
	return nil
}
func differenceScope(d PermissionDifference) string {
	if d.Category == policy.CredentialAccessRequested {
		return d.CredentialScope
	}
	return "network:" + d.Domain
}
