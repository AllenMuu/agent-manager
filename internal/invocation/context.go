// Package invocation defines correlation metadata and contracts for governed
// tool calls. Context values contain identifiers only, never credentials.
package invocation

import (
	"errors"
	"regexp"

	"github.com/AllenMuu/skill-manager/internal/identity"
)

var (
	correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@/-]{0,255}$`)
	traceIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	policyHashPattern    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Lineage struct {
	RunID              string `json:"run_id"`
	ActorID            string `json:"actor_id,omitempty"`
	DelegationID       string `json:"delegation_id,omitempty"`
	PolicySnapshotHash string `json:"policy_snapshot_hash"`
	ApprovalID         string `json:"approval_id,omitempty"`
}

func (l Lineage) Validate() error {
	if !safeCorrelationID(l.RunID) || !policyHashPattern.MatchString(l.PolicySnapshotHash) {
		return errors.New("invocation lineage requires a safe run id and policy snapshot hash")
	}
	if (l.ActorID == "") != (l.DelegationID == "") {
		return errors.New("invocation lineage must include both actor and delegation ids or neither")
	}
	for _, value := range []string{l.ActorID, l.DelegationID, l.ApprovalID} {
		if value != "" && !safeCorrelationID(value) {
			return errors.New("invocation lineage contains an invalid correlation id")
		}
	}
	return nil
}

type InvocationContext struct {
	RunID              string `json:"run_id"`
	ActorID            string `json:"actor_id,omitempty"`
	DelegationID       string `json:"delegation_id,omitempty"`
	PolicySnapshotHash string `json:"policy_snapshot_hash"`
	ApprovalID         string `json:"approval_id,omitempty"`
	TraceID            string `json:"trace_id"`
}

func NewContext(lineage Lineage, traceID string) (InvocationContext, error) {
	if err := lineage.Validate(); err != nil {
		return InvocationContext{}, err
	}
	context := InvocationContext{
		RunID: lineage.RunID, ActorID: lineage.ActorID, DelegationID: lineage.DelegationID,
		PolicySnapshotHash: lineage.PolicySnapshotHash, ApprovalID: lineage.ApprovalID, TraceID: traceID,
	}
	if err := context.Validate(); err != nil {
		return InvocationContext{}, err
	}
	return context, nil
}

func (c InvocationContext) Validate() error {
	if err := c.lineage().Validate(); err != nil {
		return err
	}
	if !traceIDPattern.MatchString(c.TraceID) || identity.IsCredentialLike(c.TraceID) {
		return errors.New("invocation context requires a safe trace id")
	}
	return nil
}

func (c InvocationContext) ValidateFor(expected Lineage) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	if c.lineage() != expected {
		return errors.New("invocation context does not match the authorized lineage")
	}
	return nil
}

func (c InvocationContext) lineage() Lineage {
	return Lineage{RunID: c.RunID, ActorID: c.ActorID, DelegationID: c.DelegationID, PolicySnapshotHash: c.PolicySnapshotHash, ApprovalID: c.ApprovalID}
}

func safeCorrelationID(value string) bool {
	return correlationIDPattern.MatchString(value) && !identity.IsCredentialLike(value)
}
