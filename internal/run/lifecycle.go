package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// Coordinator owns durable intent before mutation and accepts only exactly
// correlated provider receipts. Reconcile never repeats an uncertain mutation.
type Coordinator struct {
	store     LifecycleStore
	providers map[string]enforcement.Provider
}

func NewCoordinator(store LifecycleStore, providers ...enforcement.Provider) (*Coordinator, error) {
	if store == nil {
		return nil, errors.New("lifecycle store required")
	}
	c := &Coordinator{store: store, providers: map[string]enforcement.Provider{}}
	for _, p := range providers {
		if p == nil {
			return nil, errors.New("provider required")
		}
		if err := validateID("provider", p.ID()); err != nil {
			return nil, err
		}
		if c.providers[p.ID()] != nil {
			return nil, errors.New("duplicate provider identity")
		}
		c.providers[p.ID()] = p
	}
	return c, nil
}
func (c *Coordinator) Prepare(ctx context.Context, snapshot policy.Snapshot, project string, selection identity.Selection, request enforcement.Request) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if len(c.providers) != 1 {
		return Record{}, errors.New("prepare requires exactly one selected provider")
	}
	var p enforcement.Provider
	for _, selected := range c.providers {
		p = selected
	}
	declaration := p.Declaration()
	if declaration.Name != p.ID() {
		return Record{}, errors.New("selected provider declaration identity mismatch")
	}
	caps := map[policy.Control]bool{}
	for control, supported := range declaration.Governance {
		caps[control] = supported
	}
	if _, ok := p.(enforcement.PauseResumer); !ok {
		caps[policy.ControlApprovalPauseResume] = false
	}
	declaration.Governance = caps
	request.Policy, request.Provider = snapshot.Policy, declaration
	if !enforcement.Preflight(request).Accepted {
		return Record{}, errors.New("selected provider does not satisfy mandatory runtime preflight")
	}
	r, err := c.store.CreateManagedRun(snapshot, p.ID(), project, selection, declaration.Governance)
	if err != nil {
		return Record{}, err
	}
	return c.mutate(ctx, p, r)
}
func (c *Coordinator) Start(ctx context.Context, id string) (Record, error) {
	return c.transition(ctx, id, "start", "")
}
func (c *Coordinator) Pause(ctx context.Context, id string) (Record, error) {
	return c.transition(ctx, id, "pause", "")
}
func (c *Coordinator) Resume(ctx context.Context, id string) (Record, error) {
	return c.transition(ctx, id, "resume", "")
}
func (c *Coordinator) Terminate(ctx context.Context, id string) (Record, error) {
	return c.TerminateReason(ctx, id, TerminationOperatorRequested)
}
func (c *Coordinator) TerminateReason(ctx context.Context, id, reason string) (Record, error) {
	return c.transition(ctx, id, "terminate", reason)
}
func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("lifecycle context required")
	}
	return ctx.Err()
}
func (c *Coordinator) provider(r Record) (enforcement.Provider, error) {
	if r.ExternalRuntime == nil {
		return nil, errors.New("run has no managed provider")
	}
	p := c.providers[r.ExternalRuntime.ProviderID]
	if p == nil {
		return nil, errors.New("persisted provider unavailable; manual recovery required")
	}
	return p, nil
}
func (c *Coordinator) transition(ctx context.Context, id, action, reason string) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	r, err := c.store.Get(id)
	if err != nil {
		return Record{}, err
	}
	p, err := c.provider(r)
	if err != nil {
		return r, err
	}
	if action == "pause" || action == "resume" {
		if _, ok := p.(enforcement.PauseResumer); !ok {
			return r, enforcement.ErrUnsupported
		}
	}
	r, err = c.store.BeginLifecycle(id, action, reason)
	if err != nil {
		return r, err
	}
	return c.mutate(ctx, p, r)
}
func (c *Coordinator) mutate(ctx context.Context, p enforcement.Provider, r Record) (Record, error) {
	op := r.ExternalRuntime.Operations[len(r.ExternalRuntime.Operations)-1].Request
	request, err := copyOperation(op)
	if err != nil {
		return r, err
	}
	var ack enforcement.Acknowledgement
	switch op.Action {
	case "prepare":
		ack, err = p.Prepare(ctx, request)
	case "start":
		ack, err = p.Start(ctx, request)
	case "terminate":
		ack, err = p.Terminate(ctx, request)
	case "pause":
		ack, err = p.(enforcement.PauseResumer).Pause(ctx, request)
	case "resume":
		ack, err = p.(enforcement.PauseResumer).Resume(ctx, request)
	}
	return c.finish(r, op, ack, err, false)
}
func (c *Coordinator) finish(r Record, op enforcement.Operation, ack enforcement.Acknowledgement, providerErr error, reconciled bool) (Record, error) {
	outcome := "confirmed"
	var receipt *enforcement.Acknowledgement = &ack
	if providerErr != nil {
		outcome = "unknown"
		receipt = nil
		if errors.Is(providerErr, enforcement.ErrUnsupported) {
			outcome = "unsupported"
		}
		if errors.Is(providerErr, enforcement.ErrRefused) {
			outcome = "failed"
		}
	}
	if providerErr == nil {
		if err := validateAcknowledgement(op, ack); err != nil {
			providerErr = err
			outcome = "unknown"
			receipt = nil
		}
	}
	if providerErr != nil {
		providerErr = enforcement.ErrUnknown
		if outcome == "failed" {
			providerErr = enforcement.ErrRefused
		}
		if outcome == "unsupported" {
			providerErr = enforcement.ErrUnsupported
		}
	}
	updated, err := c.store.FinishLifecycle(r.ID, op.ID, receipt, outcome, reconciled)
	if err != nil {
		return r, errors.Join(providerErr, fmt.Errorf("persist lifecycle outcome; reconcile original operation: %w", err))
	}
	if providerErr != nil {
		return updated, providerErr
	}
	return updated, nil
}
func (c *Coordinator) Reconcile(ctx context.Context, id string) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	r, err := c.store.Get(id)
	if err != nil {
		return Record{}, err
	}
	p, err := c.provider(r)
	if err != nil {
		return r, err
	}
	op := r.ExternalRuntime.Operations[len(r.ExternalRuntime.Operations)-1]
	if op.Outcome == "confirmed" {
		return r, nil
	}
	querier, ok := p.(enforcement.Querier)
	if !ok {
		return r, enforcement.ErrUnsupported
	}
	request, err := copyOperation(op.Request)
	if err != nil {
		return r, err
	}
	ack, err := querier.Query(ctx, request)
	return c.finish(r, op.Request, ack, err, true)
}

// Separate provider-owned values from the coordinator's expected authority.
func copyOperation(op enforcement.Operation) (enforcement.Operation, error) {
	data, err := json.Marshal(op)
	if err != nil {
		return enforcement.Operation{}, errors.New("cannot copy lifecycle authority")
	}
	var copied enforcement.Operation
	if err = json.Unmarshal(data, &copied); err != nil {
		return enforcement.Operation{}, errors.New("cannot copy lifecycle authority")
	}
	return copied, nil
}
