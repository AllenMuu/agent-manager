package run

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// ExternalRuntime keeps desired operations separate from provider-observed state.
// Each operation is also its lifecycle audit evidence, bound to fixed authority.
type ExternalRuntime struct {
	ProviderID    string               `json:"provider_id"`
	Handle        string               `json:"handle,omitempty"`
	Generation    string               `json:"generation,omitempty"`
	ObservedState string               `json:"observed_state,omitempty"`
	Operations    []LifecycleOperation `json:"operations"`
}
type LifecycleOperation struct {
	ID         string                `json:"id"`
	Request    enforcement.Operation `json:"request"`
	Outcome    string                `json:"outcome"`
	Reason     string                `json:"reason,omitempty"`
	Reconciled bool                  `json:"reconciled,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

// LifecycleStore permits public persistence-failure tests without private hooks.
// Mutators must atomically compare pending correlations with shared durable state.
type LifecycleStore interface {
	Get(string) (Record, error)
	CreateManagedRun(policy.Snapshot, string, string, identity.Selection, map[policy.Control]bool) (Record, error)
	BeginLifecycle(string, string, string) (Record, error)
	FinishLifecycle(string, string, *enforcement.Acknowledgement, string, bool) (Record, error)
}

func (s *Store) CreateManagedRun(snapshot policy.Snapshot, provider, project string, selection identity.Selection, caps map[policy.Control]bool) (Record, error) {
	if err := validateID("provider", provider); err != nil {
		return Record{}, err
	}
	id, err := newID("op")
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	external := &ExternalRuntime{ProviderID: provider, Operations: []LifecycleOperation{{ID: id, Outcome: "pending", CreatedAt: now, UpdatedAt: now}}}
	record, _, err := s.create(snapshot, provider, project, selection, caps, now, external)
	return record, err
}
func lifecycleRequest(r Record, id, action string) enforcement.Operation {
	return enforcement.Operation{ProviderID: r.ExternalRuntime.ProviderID, RunID: r.ID, ID: id, Action: action, Handle: r.ExternalRuntime.Handle, Generation: r.ExternalRuntime.Generation, Policy: r.Policy, Identity: r.Identity}
}
func unresolved(r Record) bool {
	if r.ExternalRuntime == nil {
		return false
	}
	for _, op := range r.ExternalRuntime.Operations {
		if op.Outcome == "pending" || op.Outcome == "unknown" {
			return true
		}
	}
	return false
}

// ExecutionReady survives coordinator restart; unconfirmed mutations block calls.
func (r Record) ExecutionReady() bool {
	return r.Status == Active && (r.ExternalRuntime == nil || (r.ExternalRuntime.ObservedState == "active" && !unresolved(r)))
}
func (s *Store) BeginLifecycle(id, action, reason string) (Record, error) {
	var result Record
	err := s.update(func(db *database) error {
		r, ok := db.Runs[id]
		if !ok || r.ExternalRuntime == nil {
			return errors.New("managed run not found")
		}
		if unresolved(r) {
			return enforcement.ErrUnknown
		}
		allowed := action == "start" && r.Status == Prepared || action == "pause" && r.Status == Active || action == "resume" && r.Status == Paused || action == "terminate" && (r.Status == Active || r.Status == Paused || r.Status == Prepared)
		if !allowed {
			return errors.New("lifecycle transition is invalid for confirmed run state")
		}
		if action == "terminate" && !validTerminationReason(reason) {
			return errors.New("invalid termination reason")
		}
		if action != "terminate" && reason != "" {
			return errors.New("reason only applies to termination")
		}
		opID, err := newID("op")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r.ExternalRuntime.Operations = append(r.ExternalRuntime.Operations, LifecycleOperation{ID: opID, Request: lifecycleRequest(r, opID, action), Outcome: "pending", CreatedAt: now, UpdatedAt: now})
		r.ExternalRuntime.Operations[len(r.ExternalRuntime.Operations)-1].Request.TerminationReason = reason
		r.UpdatedAt = now
		db.Runs[id] = r
		result = r
		return nil
	})
	return result, err
}
func (s *Store) FinishLifecycle(id, opID string, ack *enforcement.Acknowledgement, outcome string, reconciled bool) (Record, error) {
	var result Record
	err := s.update(func(db *database) error {
		r, ok := db.Runs[id]
		if !ok || r.ExternalRuntime == nil {
			return errors.New("managed run not found")
		}
		ops := r.ExternalRuntime.Operations
		index := len(ops) - 1
		if index < 0 || ops[index].ID != opID {
			return errors.New("stale lifecycle correlation")
		}
		op := &ops[index]
		if op.Outcome == "confirmed" {
			if ack == nil {
				return errors.New("operation already confirmed")
			}
			if err := validateAcknowledgement(op.Request, *ack); err != nil {
				return err
			}
			result = r
			return nil
		}
		if outcome != "confirmed" && outcome != "unknown" && outcome != "failed" && outcome != "unsupported" {
			return errors.New("invalid lifecycle outcome")
		}
		now := time.Now().UTC()
		if ack != nil {
			if outcome != "confirmed" {
				return errors.New("acknowledgement requires confirmed outcome")
			}
			if err := validateAcknowledgement(op.Request, *ack); err != nil {
				return err
			}
			r.ExternalRuntime.Handle, r.ExternalRuntime.Generation, r.ExternalRuntime.ObservedState = ack.Operation.Handle, ack.Operation.Generation, ack.State
			r.Status = Status(ack.State)
			if r.Status == Terminated {
				r.TerminationReason = op.Request.TerminationReason
			}
			if op.Request.Action == "start" || op.Request.Action == "terminate" {
				category := policy.AgentRunStarted
				if op.Request.Action == "terminate" {
					category = policy.AgentRunTerminated
				}
				audit, err := auditFor(r, policy.Event{Category: category, RunID: id, Timestamp: now}, policy.Decision{Outcome: policy.Allow}, r.TerminationReason)
				if err != nil {
					return err
				}
				db.Events = append(db.Events, audit)
			}
		} else if outcome == "confirmed" {
			return errors.New("confirmation requires provider acknowledgement")
		}
		op.Outcome, op.UpdatedAt, op.Reconciled = outcome, now, op.Reconciled || reconciled
		if outcome == "unknown" {
			op.Reason = "outcome_unknown"
		} else if outcome == "unsupported" {
			op.Reason = "capability_unsupported"
		} else if outcome == "failed" {
			op.Reason = "provider_refused"
		} else {
			op.Reason = ""
		}
		r.UpdatedAt = now
		db.Runs[id] = r
		result = r
		return nil
	})
	return result, err
}
func validateAcknowledgement(want enforcement.Operation, ack enforcement.Acknowledgement) error {
	got := ack.Operation
	if got.ProviderID != want.ProviderID || got.RunID != want.RunID || got.ID != want.ID || got.Action != want.Action || got.TerminationReason != want.TerminationReason || !reflect.DeepEqual(got.Policy, want.Policy) || !reflect.DeepEqual(got.Identity, want.Identity) {
		return errors.New("provider acknowledgement authority or correlation mismatch")
	}
	if err := validateID("handle", got.Handle); err != nil {
		return errors.New("unsafe provider handle")
	}
	if err := validateID("generation", got.Generation); err != nil {
		return errors.New("unsafe provider generation")
	}
	if want.Action != "prepare" && (got.Handle != want.Handle || got.Generation != want.Generation) {
		return errors.New("stale provider handle or generation")
	}
	expected := map[string]string{"prepare": "prepared", "start": "active", "pause": "paused", "resume": "active", "terminate": "terminated"}[want.Action]
	if expected == "" || ack.State != expected {
		return errors.New("provider acknowledgement state mismatch")
	}
	return nil
}
func validateExternal(r Record) error {
	e := r.ExternalRuntime
	if e == nil {
		if r.Status == Pending || r.Status == Prepared {
			return errors.New("local run cannot have managed preparation status")
		}
		return nil
	}
	if err := validateID("provider", e.ProviderID); err != nil {
		return err
	}
	if e.ProviderID != r.Runtime || len(e.Operations) == 0 {
		return errors.New("invalid external lineage")
	}
	if (e.Handle == "") != (e.Generation == "") {
		return errors.New("incomplete external handle lineage")
	}
	if e.Handle != "" {
		if err := validateID("handle", e.Handle); err != nil {
			return err
		}
		if err := validateID("generation", e.Generation); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, op := range e.Operations {
		if err := validateID("operation", op.ID); err != nil {
			return err
		}
		if seen[op.ID] || op.ID != op.Request.ID || op.CreatedAt.IsZero() || op.UpdatedAt.Before(op.CreatedAt) {
			return errors.New("invalid lifecycle operation")
		}
		seen[op.ID] = true
		if op.Request.ProviderID != e.ProviderID || op.Request.RunID != r.ID || !reflect.DeepEqual(op.Request.Policy, r.Policy) || !reflect.DeepEqual(op.Request.Identity, r.Identity) {
			return errors.New("lifecycle authority differs from immutable run")
		}
		if op.Outcome != "pending" && op.Outcome != "unknown" && op.Outcome != "failed" && op.Outcome != "confirmed" && op.Outcome != "unsupported" {
			return errors.New("invalid lifecycle operation outcome")
		}
		if i < len(e.Operations)-1 && (op.Outcome == "pending" || op.Outcome == "unknown") {
			return errors.New("unresolved lifecycle operation was superseded")
		}
		if op.Reason != "" && op.Reason != "outcome_unknown" && op.Reason != "provider_refused" && op.Reason != "capability_unsupported" {
			return errors.New("unsafe lifecycle reason")
		}
		if _, ok := map[string]bool{"prepare": true, "start": true, "pause": true, "resume": true, "terminate": true}[op.Request.Action]; !ok {
			return fmt.Errorf("invalid lifecycle action")
		}
	}
	if e.ObservedState != "" && e.ObservedState != string(r.Status) {
		return errors.New("run status differs from observed external state")
	}
	if e.ObservedState == "" && r.Status != Pending {
		return errors.New("unconfirmed managed run has confirmed status")
	}
	return nil
}
