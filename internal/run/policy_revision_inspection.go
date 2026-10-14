package run

import (
	"errors"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

type PolicyRevisionInspection struct {
	ActionApproval  *Approval                  `json:"action_approval,omitempty"`
	Denial          AuditRecord                `json:"denial"`
	Proposal        PermissionProposal         `json:"proposal"`
	Decision        PermissionProposalDecision `json:"decision"`
	Mutation        PolicyRevisionMutation     `json:"mutation"`
	Desired         policy.Snapshot            `json:"desired"`
	Applied         policy.Snapshot            `json:"applied"`
	RetryRequest    *AuditRecord               `json:"retry_request,omitempty"`
	RetryCompletion *AuditRecord               `json:"retry_completion,omitempty"`
}

func (s *Store) InspectPolicyRevision(id string) ([]PolicyRevisionInspection, error) {
	db, err := s.read()
	if err != nil {
		return nil, err
	}
	r, ok := db.Runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	if r.PolicyRevisions == nil {
		return nil, nil
	}
	result := []PolicyRevisionInspection{}
	for _, m := range r.PolicyRevisions.Mutations {
		p := db.Proposals[m.Request.ProposalID]
		entry := PolicyRevisionInspection{Denial: p.Denial, Proposal: p, Decision: p.Decisions[len(p.Decisions)-1], Mutation: m, Desired: r.PolicyRevisions.Desired, Applied: r.AppliedPolicy()}
		if m.Retry != nil {
			if a, ok := db.Approvals[m.Retry.ApprovalID]; ok {
				entry.ActionApproval = &a
			}
			if a, ok := auditRecordByID(db.Events, m.Retry.RequestAuditID); ok {
				entry.RetryRequest = &a
			}
			if a, ok := auditRecordByID(db.Events, m.Retry.CompletionAuditID); ok {
				entry.RetryCompletion = &a
			}
		}
		result = append(result, entry)
	}
	return result, nil
}
func validateRevisionLineage(db database) error {
	for _, r := range db.Runs {
		if r.PolicyRevisions == nil {
			continue
		}
		for _, m := range r.PolicyRevisions.Mutations {
			p, ok := db.Proposals[m.Request.ProposalID]
			if !ok || p.RunID != r.ID || p.Status != ProposalApproved || len(p.Decisions) == 0 || !samePolicySnapshot(p.BasePolicy, m.Request.Base) {
				return errors.New("policy mutation has no approved base proposal")
			}
			d := p.Decisions[len(p.Decisions)-1]
			if d.ID != m.Request.DecisionID || d.Operator == nil || d.Operator.ReceiptID != m.Request.ReceiptID {
				return errors.New("policy mutation has no trusted decision")
			}
			target, err := applyDifference(m.Request.Base, p, m.CreatedAt)
			if err != nil || !samePolicySnapshot(target, m.Request.Target) {
				return errors.New("policy mutation exceeds approved difference")
			}
			if m.Retry == nil {
				if len(m.RetryAttempts) > 0 {
					return errors.New("retry history has no current attempt")
				}
				continue
			}
			retries := append([]PolicyRetry(nil), m.RetryAttempts...)
			retries = append(retries, *m.Retry)
			for retryIndex := range retries {
				retry := &retries[retryIndex]
				if retryIndex < len(m.RetryAttempts) && retry.State != "not_dispatched" {
					return errors.New("consumed retry superseded")
				}
				if retry.ID != m.Request.ID+"-retry" || m.State != "applied" {
					return errors.New("retry has invalid mutation")
				}
				switch retry.State {
				case "resuming":
					if retry.RequestAuditID != "" || retry.CompletionAuditID != "" {
						return errors.New("resume invents dispatch")
					}
				case "dispatching", "succeeded", "unknown", "not_dispatched":
					a, ok := auditRecordByID(db.Events, retry.RequestAuditID)
					if !ok || a.RunID != r.ID || a.Category != p.Denial.Category || a.ActionID != p.Denial.ActionID || a.Tool != p.Denial.Tool || a.Domain != p.Denial.Domain || a.CredentialScope != p.Denial.CredentialScope || a.TraceID != p.Denial.TraceID || a.PolicyHash != m.Request.Target.Hash || (a.Decision != policy.Allow && !(a.Decision == policy.RequireApproval && retry.ApprovalID != "" && a.ApprovalID == retry.ApprovalID)) {
						return errors.New("retry has no exact authorized request")
					}
					if retry.State == "dispatching" {
						if retry.CompletionAuditID != "" {
							return errors.New("dispatch invents completion")
						}
					} else {
						completion, ok := auditRecordByID(db.Events, retry.CompletionAuditID)
						if retry.ApprovalID != "" && (completion.ApprovalID != retry.ApprovalID || completion.ApproverID == "") {
							return errors.New("retry completion lost action approval")
						}
						if a.Category == policy.ToolCallRequested || a.Category == policy.NetworkAccessRequested {
							want := policy.Outcome(InvocationSucceeded)
							if retry.State == "unknown" {
								want = policy.Outcome(InvocationFailed)
							}
							if retry.State == "not_dispatched" {
								want = policy.Outcome(InvocationBlocked)
							}
							if completion.Result != want {
								return errors.New("retry completion contradicts dispatch outcome")
							}
						}
						if !ok || completion.RunID != r.ID || completion.ActionID != a.ActionID || completion.PolicyHash != a.PolicyHash || ((a.Category == policy.ToolCallRequested || a.Category == policy.NetworkAccessRequested) && completion.RequestAuditID != a.ID) {
							return errors.New("retry has no exact completion")
						}
					}
				default:
					return errors.New("invalid retry state")
				}
			}
		}
	}
	return nil
}
