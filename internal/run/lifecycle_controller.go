package run

import (
	"context"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// SetCoordinator reconnects providers by persisted identity after restart.
// Historical local-only controller registration remains independent.
func (m *Manager) SetCoordinator(c *Coordinator) { m.mu.Lock(); defer m.mu.Unlock(); m.coordinator = c }

type lifecycleController struct{ coordinator *Coordinator }

func (c lifecycleController) Capabilities() map[policy.Control]bool {
	return map[policy.Control]bool{policy.ControlApprovalPauseResume: true, policy.ControlRunTermination: true}
}
func (c lifecycleController) PauseForApproval(ctx context.Context, id, approval string) (bool, error) {
	r, err := c.coordinator.Pause(ctx, id)
	return err == nil && r.Status == Paused, err
}
func (c lifecycleController) ResolveApproval(ctx context.Context, id, approval string, approved bool) (bool, error) {
	r, err := c.coordinator.Resume(ctx, id)
	return err == nil && r.ExecutionReady(), err
}
func (c lifecycleController) Kill(ctx context.Context, id, reason string) (bool, error) {

	r, err := c.coordinator.TerminateReason(ctx, id, reason)
	return err == nil && r.Status == Terminated, err
}

// Compile-time boundary contracts intentionally omit future policy application
// and event retrieval until those separately scoped capabilities exist.
var _ enforcement.PauseResumer = (*enforcement.MockProvider)(nil)
