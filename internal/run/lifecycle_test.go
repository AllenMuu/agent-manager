package run_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func lifecycleFixture(t *testing.T) (*run.Store, *enforcement.MockProvider, policy.Snapshot) {
	t.Helper()
	s, e := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if e != nil {
		t.Fatal(e)
	}
	p, e := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: lifecycle\nname: Lifecycle\ntools:\n  allow: [read]\n"))
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := policy.Resolve(p, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	return s, enforcement.NewMockProvider("offline"), snapshot
}
func TestPreparedRunPersistsNeutralLineage(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, e := run.NewCoordinator(s, p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	reopened, _ := run.NewStore(s.Root())
	got, e := reopened.Get(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != run.Prepared || got.ExternalRuntime.ProviderID != "offline" || got.ExternalRuntime.Handle == "" || got.ExternalRuntime.Generation == "" || len(got.ExternalRuntime.Operations) != 1 || got.ExternalRuntime.Operations[0].Outcome != "confirmed" || got.Policy.Hash != snapshot.Hash || got.Identity.Mode != identity.ModeAnonymous {
		t.Fatalf("prepared inspection: %#v", got)
	}
}

func TestUncertainStartBlocksGovernanceAfterRestart(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	p.FailNext("start", enforcement.ErrUnknown, false)
	if _, e = c.Start(context.Background(), r.ID); e == nil {
		t.Fatal("unknown start must be reported")
	}
	reopened, _ := run.NewStore(s.Root())
	manager, _ := run.NewManager(reopened)
	if _, _, _, e = manager.EvaluateAndRecord(r.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read", ActionID: "read"}, policy.BudgetState{}, time.Now().UTC()); e == nil {
		t.Fatal("uncertain start allowed governance execution")
	}
	recovered, _ := run.NewCoordinator(reopened, p)
	if _, e = recovered.Reconcile(context.Background(), r.ID); e == nil {
		t.Fatal("unknown outcome must remain unavailable")
	}
	pending, _ := reopened.Get(r.ID)
	if pending.ExecutionReady() || pending.Status != run.Prepared {
		t.Fatalf("unknown changed confirmed state: %#v", pending)
	}
	calls := p.Calls()
	p.SetOutcome(calls[len(calls)-1], "active")
	confirmed, e := recovered.Reconcile(context.Background(), r.ID)
	if e != nil || !confirmed.ExecutionReady() {
		t.Fatalf("reconciled start: %#v %v", confirmed, e)
	}
	if len(p.Calls()) != 2 || p.ResourceCount() != 1 {
		t.Fatal("recovery duplicated external runtime")
	}
}

type failConfirmationStore struct {
	*run.Store
	fail bool
}

func (s *failConfirmationStore) FinishLifecycle(id, op string, a *enforcement.Acknowledgement, outcome string, reconciled bool) (run.Record, error) {
	if s.fail {
		s.fail = false
		return run.Record{}, fmt.Errorf("confirmation storage unavailable")
	}
	return s.Store.FinishLifecycle(id, op, a, outcome, reconciled)
}
func TestPrepareConfirmationFailureRecoversOriginalHandle(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	failing := &failConfirmationStore{Store: s, fail: true}
	c, _ := run.NewCoordinator(failing, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e == nil || r.ID == "" {
		t.Fatal("missing recoverable failed prepare")
	}
	pending, _ := s.Get(r.ID)
	if pending.ExecutionReady() || pending.ExternalRuntime.Handle != "" || pending.ExternalRuntime.Operations[0].Outcome != "pending" {
		t.Fatalf("incorrect confirmation: %#v", pending)
	}
	reopened, _ := run.NewStore(s.Root())
	restarted, _ := run.NewCoordinator(reopened, p)
	got, e := restarted.Reconcile(context.Background(), r.ID)
	if e != nil || got.ExternalRuntime.Handle != "mock-"+r.ID || !got.ExternalRuntime.Operations[0].Reconciled {
		t.Fatalf("recovery: %#v %v", got, e)
	}
	if len(p.Calls()) != 1 || p.ResourceCount() != 1 {
		t.Fatal("prepare recovery created another runtime")
	}
}
func TestTerminationUnknownAndRefusedKeepConfirmedState(t *testing.T) {
	for _, fault := range []error{context.DeadlineExceeded, enforcement.ErrRefused} {
		t.Run(fault.Error(), func(t *testing.T) {
			s, p, snapshot := lifecycleFixture(t)
			c, _ := run.NewCoordinator(s, p)
			r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = c.Start(context.Background(), r.ID); e != nil {
				t.Fatal(e)
			}
			p.FailNext("terminate", fault, false)
			if _, e = c.Terminate(context.Background(), r.ID); e == nil {
				t.Fatal("unacknowledged termination succeeded")
			}
			got, _ := s.Get(r.ID)
			last := got.ExternalRuntime.Operations[len(got.ExternalRuntime.Operations)-1]
			want := "unknown"
			if errors.Is(fault, enforcement.ErrRefused) {
				want = "failed"
			}
			if got.Status != run.Active || last.Outcome != want || got.TerminationReason != "" {
				t.Fatalf("unconfirmed termination: %#v", got)
			}
		})
	}
}

type requiredOnlyProvider struct{ delegate *enforcement.MockProvider }

func (p requiredOnlyProvider) ID() string                           { return p.delegate.ID() }
func (p requiredOnlyProvider) Declaration() enforcement.Declaration { return p.delegate.Declaration() }
func (p requiredOnlyProvider) Prepare(c context.Context, o enforcement.Operation) (enforcement.Acknowledgement, error) {
	return p.delegate.Prepare(c, o)
}
func (p requiredOnlyProvider) Start(c context.Context, o enforcement.Operation) (enforcement.Acknowledgement, error) {
	return p.delegate.Start(c, o)
}
func (p requiredOnlyProvider) Terminate(c context.Context, o enforcement.Operation) (enforcement.Acknowledgement, error) {
	return p.delegate.Terminate(c, o)
}
func TestUnsupportedPauseAndQueryStayExplicit(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, requiredOnlyProvider{p})
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Start(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Pause(context.Background(), r.ID); !errors.Is(e, enforcement.ErrUnsupported) {
		t.Fatalf("pause: %v", e)
	}
	got, _ := s.Get(r.ID)
	if got.Status != run.Active || len(got.ExternalRuntime.Operations) != 2 {
		t.Fatal("unsupported pause manufactured confirmation")
	}
	p.FailNext("terminate", enforcement.ErrUnknown, false)
	c.Terminate(context.Background(), r.ID)
	restarted, _ := run.NewCoordinator(s, requiredOnlyProvider{p})
	if _, e = restarted.Reconcile(context.Background(), r.ID); !errors.Is(e, enforcement.ErrUnsupported) {
		t.Fatalf("query: %v", e)
	}
	if _, e = restarted.Start(context.Background(), r.ID); e == nil {
		t.Fatal("uncertain mutation was blindly retried")
	}
}
func TestManagedTerminationPreservesSupportedReason(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	c.Start(context.Background(), r.ID)
	m, _ := run.NewManager(s)
	m.SetCoordinator(c)
	got, e := m.Kill(context.Background(), r.ID, run.TerminationBudgetExhausted, time.Now().UTC())
	if e != nil || got.Status != run.Terminated || got.TerminationReason != run.TerminationBudgetExhausted {
		t.Fatalf("managed confirmed reason: %#v %v", got, e)
	}
}

func TestConcurrentCoordinatorsLaunchOnlyOnce(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	other, _ := run.NewStore(s.Root())
	second, _ := run.NewCoordinator(other, p)
	ready := make(chan struct{})
	errs := make(chan error, 2)
	for _, coordinator := range []*run.Coordinator{c, second} {
		go func(coordinator *run.Coordinator) {
			<-ready
			_, e := coordinator.Start(context.Background(), r.ID)
			errs <- e
		}(coordinator)
	}
	close(ready)
	success := 0
	for n := 0; n < 2; n++ {
		if <-errs == nil {
			success++
		}
	}
	if success != 1 || len(p.Calls()) != 2 || p.ResourceCount() != 1 {
		t.Fatalf("duplicate launch: successes=%d operations=%d resources=%d", success, len(p.Calls()), p.ResourceCount())
	}
}

type failIntentStore struct{ *run.Store }

func (s failIntentStore) BeginLifecycle(string, string, string) (run.Record, error) {
	return run.Record{}, fmt.Errorf("intent storage unavailable")
}
func TestIntentFailureHasNoExternalSideEffect(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	failing, _ := run.NewCoordinator(failIntentStore{s}, p)
	if _, e = failing.Start(context.Background(), r.ID); e == nil {
		t.Fatal("intent failure ignored")
	}
	if len(p.Calls()) != 1 {
		t.Fatal("side effect before durable intent")
	}
	got, _ := s.Get(r.ID)
	if got.Status != run.Prepared || len(got.ExternalRuntime.Operations) != 1 {
		t.Fatal("failed intent changed run")
	}
}

type corruptReceiptProvider struct {
	*enforcement.MockProvider
	corrupt func(*enforcement.Acknowledgement)
}

func (p corruptReceiptProvider) Start(ctx context.Context, op enforcement.Operation) (enforcement.Acknowledgement, error) {
	a, e := p.MockProvider.Start(ctx, op)
	p.corrupt(&a)
	return a, e
}
func TestForeignMalformedAndStaleAcknowledgementsFailClosed(t *testing.T) {
	cases := map[string]func(*enforcement.Acknowledgement){"run": func(a *enforcement.Acknowledgement) { a.Operation.RunID = "foreign" }, "provider": func(a *enforcement.Acknowledgement) { a.Operation.ProviderID = "foreign" }, "operation": func(a *enforcement.Acknowledgement) { a.Operation.ID = "foreign" }, "generation": func(a *enforcement.Acknowledgement) { a.Operation.Generation = "stale" }, "handle": func(a *enforcement.Acknowledgement) { a.Operation.Handle = "secret\nvalue" }, "state": func(a *enforcement.Acknowledgement) { a.State = "terminated" }, "policy": func(a *enforcement.Acknowledgement) { a.Operation.Policy.Hash = "sha256:" + strings.Repeat("a", 64) }, "identity": func(a *enforcement.Acknowledgement) {
		a.Operation.Identity = identity.Selection{Mode: identity.ModeLegacyAnonymous}
	}}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			s, p, snapshot := lifecycleFixture(t)
			c, _ := run.NewCoordinator(s, corruptReceiptProvider{p, corrupt})
			r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = c.Start(context.Background(), r.ID); e == nil {
				t.Fatal("invalid receipt accepted")
			}
			got, _ := s.Get(r.ID)
			if got.ExecutionReady() || got.Status != run.Prepared || got.ExternalRuntime.Operations[1].Outcome != "unknown" || got.Policy.Hash != snapshot.Hash {
				t.Fatalf("foreign receipt changed authority: %#v", got)
			}
			restored, _ := run.NewCoordinator(s, p)
			got, e = restored.Reconcile(context.Background(), r.ID)
			if e != nil || !got.ExecutionReady() {
				t.Fatalf("original receipt recovery: %#v %v", got, e)
			}
		})
	}
}
func TestLocalManagerCannotSimulateExternalTerminationAfterRestart(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, p)
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	c.Start(context.Background(), r.ID)
	reopened, _ := run.NewStore(s.Root())
	m, _ := run.NewManager(reopened)
	if _, e = m.Kill(context.Background(), r.ID, run.TerminationOperatorRequested, time.Now().UTC()); e == nil {
		t.Fatal("external runtime locally terminated")
	}
	got, _ := reopened.Get(r.ID)
	if got.Status != run.Active {
		t.Fatal("unconfirmed external termination")
	}
}
func TestProviderFailureNeverLeaksRawDiagnostics(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	failing := &failConfirmationStore{Store: s, fail: true}
	c, _ := run.NewCoordinator(failing, p)
	p.FailNext("prepare", errors.New("token=raw-provider-secret"), false)
	_, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e == nil || strings.Contains(e.Error(), "raw-provider-secret") {
		t.Fatalf("unsafe provider diagnostic: %v", e)
	}
}

type unavailableDeclaration struct{ *enforcement.MockProvider }

func (p unavailableDeclaration) Declaration() enforcement.Declaration {
	return enforcement.Declaration{Name: p.ID(), Kind: "directory"}
}
func TestCallerDeclarationsCannotUpgradeSelectedProvider(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, unavailableDeclaration{p})
	if _, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1", Provider: p.Declaration()}); e == nil {
		t.Fatal("caller upgraded unsupported provider")
	}
	items, e := s.List()
	if e != nil || len(items) != 0 || len(p.Calls()) != 0 {
		t.Fatalf("unsupported provider caused execution: %#v %v", items, e)
	}
}

type failPrepareIntentStore struct{ *run.Store }

func (s failPrepareIntentStore) CreateManagedRun(policy.Snapshot, string, string, identity.Selection, map[policy.Control]bool) (run.Record, error) {
	return run.Record{}, fmt.Errorf("prepare intent unavailable")
}
func TestPrepareIntentFailureHasNoExternalSideEffect(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(failPrepareIntentStore{s}, p)
	if _, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"}); e == nil {
		t.Fatal("failed intent ignored")
	}
	if len(p.Calls()) != 0 || p.ResourceCount() != 0 {
		t.Fatal("external prepare occurred without durable intent")
	}
}

type mutatingRequestProvider struct{ *enforcement.MockProvider }

func (p mutatingRequestProvider) Start(ctx context.Context, op enforcement.Operation) (enforcement.Acknowledgement, error) {
	op.Policy.Policy.Tools.Allow[0] = "injected"
	return p.MockProvider.Start(ctx, op)
}
func TestProviderCannotAliasImmutableRunAuthority(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, mutatingRequestProvider{p})
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Start(context.Background(), r.ID); e == nil {
		t.Fatal("mutated authority accepted")
	}
	got, _ := s.Get(r.ID)
	if got.Policy.Policy.Tools.Allow[0] != "read" || got.ExternalRuntime.Operations[1].Outcome != "unknown" || got.ExecutionReady() {
		t.Fatalf("provider aliased expected authority: %#v", got)
	}
}

type unsupportedPauseProvider struct{ *enforcement.MockProvider }

func (p unsupportedPauseProvider) Pause(context.Context, enforcement.Operation) (enforcement.Acknowledgement, error) {
	return enforcement.Acknowledgement{}, enforcement.ErrUnsupported
}
func TestOptionalProviderRejectionRemainsExplicit(t *testing.T) {
	s, p, snapshot := lifecycleFixture(t)
	c, _ := run.NewCoordinator(s, unsupportedPauseProvider{p})
	r, e := c.Prepare(context.Background(), snapshot, t.TempDir(), identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Start(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Pause(context.Background(), r.ID); !errors.Is(e, enforcement.ErrUnsupported) {
		t.Fatalf("unsupported provider response hidden: %v", e)
	}
	got, _ := s.Get(r.ID)
	if got.Status != run.Active || got.ExternalRuntime.Operations[2].Outcome != "unsupported" {
		t.Fatalf("unsupported manufactured transition: %#v", got)
	}
}
