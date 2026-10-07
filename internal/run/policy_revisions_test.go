package run_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/governance"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

type revisionProvider struct {
	*enforcement.MockProvider
	receipt  enforcement.PolicyReceipt
	accepted bool
	failure  error
	wrong    bool
	alter    func(*enforcement.PolicyReceipt)
}

func (p *revisionProvider) Declaration() enforcement.Declaration {
	d := p.MockProvider.Declaration()
	d.Governance[policy.ControlNetworkRestriction] = true
	d.Controls[enforcement.Network] = enforcement.Capability{Support: "supported", Update: "live-update", Verified: true}
	return d
}
func (p *revisionProvider) ApplyPolicy(_ context.Context, op enforcement.PolicyMutation) (enforcement.PolicyReceipt, error) {
	p.receipt = enforcement.PolicyReceipt{Mutation: op, EventID: op.ID + "-confirmed", State: "applied"}
	if p.accepted {
		p.receipt.State = "accepted"
	}
	if p.wrong {
		p.receipt.Mutation.Target = p.receipt.Mutation.Base
	}
	if p.alter != nil {
		p.alter(&p.receipt)
	}
	if p.failure != nil {
		return enforcement.PolicyReceipt{}, p.failure
	}
	return p.receipt, nil
}
func (p *revisionProvider) QueryPolicy(context.Context, enforcement.PolicyMutation) (enforcement.PolicyReceipt, error) {
	return p.receipt, nil
}

type revisionFixture struct {
	store       *run.Store
	manager     *run.Manager
	coordinator *run.Coordinator
	provider    *revisionProvider
	decider     *run.PermissionProposalDecider
	proposal    run.PermissionProposal
	original    run.Record
	at          time.Time
}

func newRevisionFixture(t *testing.T) revisionFixture {
	return newRevisionFixtureFor(t, "version: v1\nkind: agent-policy\nid: revision-test\nname: Revision test\ntools:\n  allow: [read]\nnetwork:\n  allowed_domains: [existing.example]\n", policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect", TraceID: "trace-network"}, "network:api.example")
}
func newRevisionFixtureFor(t *testing.T, yaml string, event policy.Event, scope string) revisionFixture {
	t.Helper()
	ctx := context.Background()
	at := time.Now().UTC()
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := run.NewManager(store)
	provider := &revisionProvider{MockProvider: enforcement.NewMockProvider("offline")}
	coordinator, err := run.NewCoordinator(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := policy.Resolve(p, at)
	actor := identity.ActorIdentity{ID: "agent-1", Kind: identity.Agent, Subject: "worker-1"}
	delegation := identity.Delegation{ID: "delegation-1", ActorID: actor.ID, Scopes: strings.Split(scope, ","), ExpiresAt: at.Add(time.Hour)}
	record, err := coordinator.Prepare(ctx, snapshot, t.TempDir(), identity.NamedSelection(actor, delegation), enforcement.Request{Version: "v1"})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	record, err = coordinator.Start(ctx, record.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	decision, _, denial, err := manager.EvaluateAndRecord(record.ID, event, policy.BudgetState{}, at.Add(time.Second))
	if err != nil || decision.Outcome != policy.Deny {
		t.Fatalf("deny: %v %v", decision, err)
	}
	proposal, err := manager.RequestPermissionProposal(run.PermissionProposalRequest{RunID: record.ID, DenialAuditID: denial.ID, Difference: run.PermissionDifference{Category: event.Category, Domain: event.Domain, ActionID: event.ActionID, ActionType: event.ActionType, Tool: event.Tool, CredentialScope: event.CredentialScope}, ExpiresAt: at.Add(10 * time.Minute)}, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	authority := &operatorFixture{actor: identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
	decider, _ := run.NewPermissionProposalDecider(store, authority, nil, func() time.Time { return at.Add(3 * time.Second) })
	proposal, err = decider.Decide(ctx, proposal.ID, run.ProposalApproved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.Pause(ctx, record.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	return revisionFixture{store, manager, coordinator, provider, decider, proposal, record, at}
}
func TestRevisionRetainsOriginalPolicyAndHistoricalAudit(t *testing.T) {
	f := newRevisionFixture(t)
	updated, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Policy, f.original.Policy) || len(updated.PolicyRevisions.Mutations) != 1 {
		t.Fatalf("immutable history: %#v", updated)
	}
	target := updated.AppliedPolicy()
	if target.Hash == updated.Policy.Hash || !reflect.DeepEqual(target.Policy.Network.AllowedDomains, []string{"api.example", "existing.example"}) {
		t.Fatalf("revision: %#v", target)
	}
	events, err := f.store.Events(updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].PolicyHash != f.original.Policy.Hash {
		t.Fatal("old audit rewritten")
	}
	_, _, newer, err := f.manager.EvaluateAndRecord(updated.ID, policy.Event{Category: policy.BudgetUpdated}, policy.BudgetState{}, f.at.Add(4*time.Second))
	if err != nil || newer.PolicyHash != target.Hash {
		t.Fatalf("new audit revision: %v %v", newer.PolicyHash, err)
	}
	reopened, _ := run.NewStore(f.store.Root())
	got, err := reopened.Get(updated.ID)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("restart: %v", err)
	}
	if err = f.decider.ValidateUse(context.Background(), f.proposal.ID); err != run.ErrProposalStaleBase {
		t.Fatalf("old base still eligible: %v", err)
	}
}

func TestProviderAcceptanceLeavesAppliedAndExecutionPaused(t *testing.T) {
	f := newRevisionFixture(t)
	f.provider.accepted = true
	updated, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PolicyRevisions.Desired.Hash == updated.Policy.Hash || updated.AppliedPolicy().Hash != updated.Policy.Hash || updated.PolicyRevisions.Mutations[0].State != "pending" || updated.Status != run.Paused || updated.ExecutionReady() {
		t.Fatalf("accepted is not applied: %#v", updated)
	}
	if _, err = f.coordinator.Resume(context.Background(), updated.ID); err == nil {
		t.Fatal("pending revision resumed")
	}
	reopened, _ := run.NewStore(f.store.Root())
	if _, err = reopened.BeginLifecycle(updated.ID, "resume", ""); err == nil {
		t.Fatal("direct store bypass after restart")
	}
}

type exactRetryAdapter struct {
	failPrepare  error
	failDispatch error
	effects      int
	event        policy.Event
}
type exactPreparedRetry struct {
	adapter *exactRetryAdapter
	event   policy.Event
}

func (a *exactRetryAdapter) Prepare(_ context.Context, event policy.Event, _ invocation.InvocationContext) (run.PreparedPolicyRetry, error) {
	if a.failPrepare != nil {
		return nil, a.failPrepare
	}
	return &exactPreparedRetry{a, event}, nil
}
func (p *exactPreparedRetry) Dispatch(context.Context) error {
	if p.adapter.failDispatch != nil {
		return p.adapter.failDispatch
	}
	p.adapter.effects++
	p.adapter.event = p.event
	return nil
}
func TestConfirmedApplicationNeedsNewlyAuthorizedExactRetry(t *testing.T) {
	f := newRevisionFixture(t)
	adapter := &exactRetryAdapter{}
	if err := f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter); err != nil {
		t.Fatal(err)
	}
	updated, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AppliedPolicy().Hash != updated.PolicyRevisions.Desired.Hash || updated.Status != run.Paused || updated.ExecutionReady() {
		t.Fatal("confirmation resumed without retry")
	}
	if _, err = f.coordinator.Resume(context.Background(), updated.ID); err == nil {
		t.Fatal("ordinary resume bypassed retry")
	}
	retried, err := f.coordinator.RetryPolicyAction(context.Background(), updated.ID, policy.BudgetState{})
	if err != nil {
		t.Fatal(err)
	}
	if !retried.ExecutionReady() || adapter.effects != 1 || adapter.event.Category != policy.NetworkAccessRequested || adapter.event.Domain != "api.example" || adapter.event.ActionID != "network.connect" {
		t.Fatalf("exact retry: %#v %#v", retried, adapter)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), updated.ID, policy.BudgetState{}); err == nil {
		t.Fatal("retry reused")
	}
	if adapter.effects != 1 {
		t.Fatal("duplicate external action")
	}
}

func TestWrongAcknowledgementAndTimeoutKeepRetryPaused(t *testing.T) {
	for _, test := range []struct {
		name    string
		wrong   bool
		failure error
		state   string
	}{{"wrong_revision", true, nil, "failed"}, {"timeout", false, context.DeadlineExceeded, "unknown"}} {
		t.Run(test.name, func(t *testing.T) {
			f := newRevisionFixture(t)
			f.provider.wrong = test.wrong
			f.provider.failure = test.failure
			updated, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
			if err == nil {
				t.Fatal("bad acknowledgement accepted")
			}
			if updated.PolicyRevisions.Mutations[0].State != test.state || updated.AppliedPolicy().Hash != updated.Policy.Hash || updated.Status != run.Paused {
				t.Fatalf("failed evidence: %#v", updated.PolicyRevisions)
			}
			if _, err = f.coordinator.Resume(context.Background(), updated.ID); err == nil {
				t.Fatal("failed update resumed")
			}
			f.provider.receipt = enforcement.PolicyReceipt{Mutation: updated.PolicyRevisions.Mutations[0].Request, EventID: "reconciled-exact", State: "applied"}
			reconciled, err := f.coordinator.ReconcilePolicyRevision(context.Background(), updated.ID)
			if err != nil || reconciled.AppliedPolicy().Hash != updated.PolicyRevisions.Desired.Hash || reconciled.ExecutionReady() {
				t.Fatalf("reconcile: %#v %v", reconciled, err)
			}
		})
	}
}

func TestInspectPermissionChangeReconstructsFullDenialToRetryChain(t *testing.T) {
	f := newRevisionFixture(t)
	adapter := &exactRetryAdapter{}
	if err := f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter); err != nil {
		t.Fatal(err)
	}
	applied, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err != nil {
		t.Fatal(err)
	}
	chain, err := f.store.InspectPolicyRevision(applied.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 1 {
		t.Fatal("missing revision chain")
	}
	entry := chain[0]
	if entry.Denial.ID != f.proposal.Denial.ID || entry.Proposal.ID != f.proposal.ID || entry.Decision.Operator.Actor.ID != "human-1" || entry.Mutation.Request.ProposalID != entry.Proposal.ID || entry.Mutation.Request.DecisionID != entry.Decision.ID || entry.Mutation.Confirmation.Mutation.ID != entry.Mutation.Request.ID || entry.RetryRequest.PolicyHash != applied.AppliedPolicy().Hash || entry.RetryCompletion.RequestAuditID != entry.RetryRequest.ID || entry.Mutation.Retry.RequestAuditID != entry.RetryRequest.ID || entry.Mutation.Retry.CompletionAuditID != entry.RetryCompletion.ID || entry.Desired.Hash != applied.AppliedPolicy().Hash || entry.Applied.Hash != entry.Desired.Hash {
		t.Fatalf("incomplete chain: %#v", entry)
	}
}

type policyEventFixture struct{ established run.EstablishedPolicyEvent }

func (a *policyEventFixture) EstablishPolicyEvent(_ context.Context, _ run.PolicyEvent) (run.EstablishedPolicyEvent, error) {
	return a.established, nil
}
func TestDuplicateStaleAndUntrustedPolicyEventsNeverDispatch(t *testing.T) {
	f := newRevisionFixture(t)
	f.provider.accepted = true
	events := &policyEventFixture{}
	retry := &exactRetryAdapter{}
	if err := f.coordinator.ConfigurePolicyRevisions(f.decider, events, retry); err != nil {
		t.Fatal(err)
	}
	pending, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	m := pending.PolicyRevisions.Mutations[0].Request
	receipt := enforcement.PolicyReceipt{Mutation: m, EventID: "ack-one", State: "applied"}
	events.established = run.EstablishedPolicyEvent{Receipt: receipt, Origin: "infrastructure", Trust: "untrusted", SourceID: "offline-events"}
	raw := run.PolicyEvent{ID: "raw-one", Payload: []byte(`{"origin":"infrastructure","trusted":true}`)}
	got, err := f.coordinator.ReceivePolicyEvent(context.Background(), pending.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AppliedPolicy().Hash != pending.Policy.Hash {
		t.Fatal("raw metadata granted authority")
	}
	events.established.Trust = "established"
	got, err = f.coordinator.ReceivePolicyEvent(context.Background(), pending.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AppliedPolicy().Hash != m.Target.Hash {
		t.Fatal("trusted receipt not applied")
	}
	got, err = f.coordinator.ReceivePolicyEvent(context.Background(), pending.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	events.established.Receipt.EventID = "ack-stale"
	events.established.Receipt.Mutation.Operation.Generation = "old-generation"
	got, err = f.coordinator.ReceivePolicyEvent(context.Background(), pending.ID, run.PolicyEvent{ID: "raw-stale"})
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyRevisions.Events[len(got.PolicyRevisions.Events)-1].Outcome != "stale" || retry.effects != 0 || got.ExecutionReady() {
		t.Fatalf("stale event dispatched: %#v", got.PolicyRevisions.Events)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), pending.ID, policy.BudgetState{}); err != nil {
		t.Fatal(err)
	}
	if retry.effects != 1 {
		t.Fatal("confirmed retry effect missing")
	}
}

func TestManagedNetworkPolicyPreparationPreservesValidatedSnapshot(t *testing.T) {
	f := newRevisionFixture(t)
	if f.original.Status != run.Active || f.original.Policy.Validate() != nil || f.original.Policy.Hash != f.original.ExternalRuntime.Operations[0].Request.Policy.Hash || f.original.ExternalRuntime.Operations[0].Outcome != "confirmed" {
		t.Fatal("valid canonical network snapshot failed preparation")
	}
}

func TestRetryPreflightAndNotDispatchedDoNotConsumeActualAction(t *testing.T) {
	f := newRevisionFixture(t)
	adapter := &exactRetryAdapter{failPrepare: context.Canceled}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
	applied, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err == nil {
		t.Fatal("prepare failure ignored")
	}
	got, _ := f.store.Get(applied.ID)
	if got.PolicyRevisions.Mutations[0].Retry != nil {
		t.Fatal("preflight consumed retry")
	}
	adapter.failPrepare = nil
	adapter.failDispatch = &invocation.NotDispatchedError{Cause: context.Canceled}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err == nil {
		t.Fatal("not dispatched ignored")
	}
	adapter.failDispatch = nil
	retried, err := f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{})
	if err != nil || !retried.ExecutionReady() || adapter.effects != 1 {
		t.Fatalf("safe revalidated retry: %v effects=%d", err, adapter.effects)
	}
}

type revisionFailureStore struct {
	*run.Store
	failIntent          bool
	failConfirmation    bool
	failRetryCompletion bool
}

func (s *revisionFailureStore) BeginPolicyRevision(c context.Context, id string, d *run.PermissionProposalDecider) (run.Record, bool, error) {
	if s.failIntent {
		return run.Record{}, false, errors.New("offline intent fault")
	}
	return s.Store.BeginPolicyRevision(c, id, d)
}
func (s *revisionFailureStore) FinishPolicyRevision(id, op string, receipt *enforcement.PolicyReceipt, state string) (run.Record, error) {
	if s.failConfirmation {
		s.failConfirmation = false
		return run.Record{}, errors.New("offline persistence fault")
	}
	return s.Store.FinishPolicyRevision(id, op, receipt, state)
}
func (s *revisionFailureStore) FinishPolicyRetry(id, retry, state string) (run.Record, error) {
	if s.failRetryCompletion {
		return run.Record{}, errors.New("offline completion persistence fault")
	}
	return s.Store.FinishPolicyRetry(id, retry, state)
}
func TestRevisionIntentAndConfirmationPersistenceFailuresNeverBlindlyReplay(t *testing.T) {
	f := newRevisionFixture(t)
	store := &revisionFailureStore{Store: f.store, failIntent: true}
	coordinator, _ := run.NewCoordinator(store, f.provider)
	if _, err := coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider); err == nil {
		t.Fatal("intent failure ignored")
	}
	if f.provider.receipt.EventID != "" {
		t.Fatal("provider update before durable intent")
	}
	store.failIntent = false
	store.failConfirmation = true
	if _, err := coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider); err == nil {
		t.Fatal("confirmation failure ignored")
	}
	reopened, _ := run.NewStore(f.store.Root())
	got, _ := reopened.Get(f.original.ID)
	if got.AppliedPolicy().Hash != got.Policy.Hash || got.ExecutionReady() {
		t.Fatal("failed local confirmation advanced applied")
	}
	recovered, err := coordinator.ReconcilePolicyRevision(context.Background(), got.ID)
	if err != nil || recovered.AppliedPolicy().Hash != recovered.PolicyRevisions.Desired.Hash {
		t.Fatalf("exact reconcile: %v", err)
	}
}
func TestUnknownRetryCompletionCannotDispatchAgainAfterRestart(t *testing.T) {
	f := newRevisionFixture(t)
	store := &revisionFailureStore{Store: f.store, failRetryCompletion: true}
	coordinator, _ := run.NewCoordinator(store, f.provider)
	adapter := &exactRetryAdapter{}
	_ = coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
	applied, err := coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err == nil {
		t.Fatal("completion failure ignored")
	}
	reopened, _ := run.NewStore(f.store.Root())
	restarted, _ := run.NewCoordinator(reopened, f.provider)
	d, _ := run.NewPermissionProposalDecider(reopened, &operatorFixture{actor: identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}, authorized: true}, nil, func() time.Time { return f.at.Add(4 * time.Second) })
	_ = restarted.ConfigurePolicyRevisions(d, nil, adapter)
	if _, err = restarted.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err == nil {
		t.Fatal("unknown dispatch replayed")
	}
	if adapter.effects != 1 {
		t.Fatal("external retry repeated")
	}
	got, _ := reopened.Get(applied.ID)
	if got.ExecutionReady() {
		t.Fatal("unknown dispatch ready")
	}
}
func TestRetryRevalidatesAuthorityExpiryAndBudgetAfterPreparation(t *testing.T) {
	for _, kind := range []string{"revoked", "expired", "budget"} {
		t.Run(kind, func(t *testing.T) {
			f := newRevisionFixture(t)
			adapter := &exactRetryAdapter{}
			_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
			applied, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
			if err != nil {
				t.Fatal(err)
			}
			authority := &operatorFixture{actor: identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}, authorized: kind != "revoked"}
			clock := f.at.Add(4 * time.Second)
			if kind == "expired" {
				clock = f.at.Add(time.Hour)
			}
			d, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return clock })
			coordinator, _ := run.NewCoordinator(f.store, f.provider)
			_ = coordinator.ConfigurePolicyRevisions(d, nil, adapter)
			budget := policy.BudgetState{}
			if kind == "budget" {
				budget.CostUSD = -1
			}
			if _, err = coordinator.RetryPolicyAction(context.Background(), applied.ID, budget); err == nil {
				t.Fatal("invalid authority/budget resumed")
			}
			if adapter.effects != 0 {
				t.Fatal("unauthorized retry effect")
			}
			got, _ := f.store.Get(applied.ID)
			if got.Status != run.Paused || got.PolicyRevisions.Mutations[0].Retry != nil {
				t.Fatal("bad retry authorized resume")
			}
		})
	}
}

func TestConcurrentStoresApplyAndRetryExactActionAtMostOnce(t *testing.T) {
	f := newRevisionFixture(t)
	reopened, _ := run.NewStore(f.store.Root())
	authority := &operatorFixture{actor: identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
	otherDecider, _ := run.NewPermissionProposalDecider(reopened, authority, nil, func() time.Time { return f.at.Add(4 * time.Second) })
	other, _ := run.NewCoordinator(reopened, f.provider)
	var wg sync.WaitGroup
	for _, entry := range []struct {
		c *run.Coordinator
		d *run.PermissionProposalDecider
	}{{f.coordinator, f.decider}, {other, otherDecider}} {
		wg.Add(1)
		go func(c *run.Coordinator, d *run.PermissionProposalDecider) {
			defer wg.Done()
			if _, err := c.ApplyPolicyRevision(context.Background(), f.proposal.ID, d); err != nil {
				t.Error(err)
			}
		}(entry.c, entry.d)
	}
	wg.Wait()
	got, err := f.store.Get(f.original.ID)
	if err != nil || len(got.PolicyRevisions.Mutations) != 1 {
		t.Fatal("duplicate update intents")
	}
	adapter := &exactRetryAdapter{}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
	_ = other.ConfigurePolicyRevisions(otherDecider, nil, adapter)
	for _, c := range []*run.Coordinator{f.coordinator, other} {
		wg.Add(1)
		go func(c *run.Coordinator) {
			defer wg.Done()
			_, _ = c.RetryPolicyAction(context.Background(), got.ID, policy.BudgetState{})
		}(c)
	}
	wg.Wait()
	if adapter.effects != 1 {
		t.Fatalf("retry effects=%d", adapter.effects)
	}
}

func TestToolRevisionRetryRequiresSeparateSingleUseActionApproval(t *testing.T) {
	yaml := "version: v2\nkind: agent-policy\nid: tool-revision\nname: Tool revision\ntools:\n  allow: [read]\n  deny: [write]\napproval:\n  required_for: [destructive_write]\nidentity:\n  rules:\n    - action_id: tool.write\n      actor_kinds: [agent]\n      required_scopes: [tool:write]\n"
	event := policy.Event{Category: policy.ToolCallRequested, ActionID: "tool.write", ActionType: "destructive_write", Tool: "write", TraceID: "tool-trace"}
	f := newRevisionFixtureFor(t, yaml, event, "tool:write")
	adapter := invocation.NewMockAdapter(true, "done")
	invoker := governance.Invoker{Runs: f.manager, Adapter: adapter}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, invoker)
	applied, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}); err == nil {
		t.Fatal("policy approval bypassed one-action approval")
	}
	if len(adapter.Calls()) != 0 {
		t.Fatal("unapproved tool dispatch")
	}
	approval, err := f.coordinator.RequestPolicyRetryApproval(context.Background(), applied.ID, policy.BudgetState{})
	if err != nil {
		t.Fatal(err)
	}
	approver := identity.ActorIdentity{ID: "action-human", Kind: identity.Human, Subject: "action-operator"}
	if _, err = f.store.DecideApproval(approval.ID, run.ApprovalApproved, "reviewed", approver, f.at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}, approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	calls := adapter.Calls()
	if len(calls) != 1 || calls[0].Context.ApprovalID != approval.ID || calls[0].Context.PolicySnapshotHash != applied.AppliedPolicy().Hash {
		t.Fatalf("dual authority context: %#v", calls)
	}
	consumed, err := f.store.GetApproval(approval.ID)
	if err != nil || consumed.ConsumedByAuditID != retried.PolicyRevisions.Mutations[0].Retry.RequestAuditID {
		t.Fatal("approval/revision retry not atomically correlated")
	}
	chain, err := f.store.InspectPolicyRevision(applied.ID)
	if err != nil || chain[0].RetryCompletion.ApproverID != approver.ID || chain[0].RetryCompletion.ApprovalID != approval.ID || chain[0].RetryCompletion.RequestAuditID != consumed.ConsumedByAuditID {
		t.Fatalf("completion lineage: %v", err)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{}, approval.ID); err == nil {
		t.Fatal("two authority grants reused")
	}
	if len(adapter.Calls()) != 1 {
		t.Fatal("approved tool repeated")
	}
}

func TestOfflineMockPolicyUpdateReconcilesLostAcknowledgementWithoutReplay(t *testing.T) {
	f := newRevisionFixture(t)
	coordinator, _ := run.NewCoordinator(f.store, f.provider.MockProvider)
	f.provider.MockProvider.FailNext("policy_update", enforcement.ErrUnknown, true)
	pending, err := coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err == nil {
		t.Fatal("lost acknowledgement ignored")
	}
	if pending.ExecutionReady() || pending.AppliedPolicy().Hash != pending.Policy.Hash {
		t.Fatal("unpersisted mock update applied locally")
	}
	reconciled, err := coordinator.ReconcilePolicyRevision(context.Background(), pending.ID)
	if err != nil || reconciled.AppliedPolicy().Hash != reconciled.PolicyRevisions.Desired.Hash {
		t.Fatalf("mock exact reconciliation: %v", err)
	}
	if _, err = coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider); err != nil {
		t.Fatal(err)
	}
	if len(f.provider.MockProvider.PolicyCalls()) != 1 {
		t.Fatal("mock update replayed")
	}
}

func TestCredentialRevisionRetriesOnlyExactDelegatedScope(t *testing.T) {
	yaml := "version: v1\nkind: agent-policy\nid: credential-revision\nname: Credential revision\ntools:\n  allow: [read]\ncredentials:\n  allowed_scopes: [existing:read]\n"
	event := policy.Event{Category: policy.CredentialAccessRequested, ActionID: "credential.read", CredentialScope: "db:read", TraceID: "credential-trace"}
	f := newRevisionFixtureFor(t, yaml, event, "db:read")
	adapter := &exactRetryAdapter{}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
	applied, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := f.coordinator.RetryPolicyAction(context.Background(), applied.ID, policy.BudgetState{})
	if err != nil || !retried.ExecutionReady() || adapter.effects != 1 || adapter.event.CredentialScope != "db:read" {
		t.Fatalf("exact credential retry: %v", err)
	}
}
func TestPolicyReceiptAllCorrelationsAndCanonicalContentFailClosed(t *testing.T) {
	cases := map[string]func(*enforcement.PolicyReceipt){"run": func(a *enforcement.PolicyReceipt) { a.Mutation.Operation.RunID = "foreign" }, "handle": func(a *enforcement.PolicyReceipt) { a.Mutation.Operation.Handle = "foreign" }, "generation": func(a *enforcement.PolicyReceipt) { a.Mutation.Operation.Generation = "old" }, "operation": func(a *enforcement.PolicyReceipt) { a.Mutation.ID = "foreign" }, "base": func(a *enforcement.PolicyReceipt) { a.Mutation.Base.Hash = "sha256:" + strings.Repeat("a", 64) }, "content": func(a *enforcement.PolicyReceipt) {
		a.Mutation.Target.Policy.Network.AllowedDomains[0] = "evil.example"
	}}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRevisionFixture(t)
			f.provider.alter = alter
			got, err := f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider)
			if err == nil || got.AppliedPolicy().Hash != f.original.Policy.Hash || got.ExecutionReady() {
				t.Fatal("foreign/mutated receipt applied")
			}
			original, _ := f.store.Get(f.original.ID)
			if original.Policy.Hash != f.original.Policy.Hash || original.Policy.Validate() != nil {
				t.Fatal("provider aliased original authority")
			}
		})
	}
}

func TestHistoricalActionApprovalCannotAuthorizeNewRevisionThroughDirectStore(t *testing.T) {
	yaml := "version: v2\nkind: agent-policy\nid: historical-approval\nname: Historical approval\ntools:\n  allow: [write]\nnetwork:\n  allowed_domains: [existing.example]\napproval:\n  required_for: [destructive_write]\nidentity:\n  rules:\n    - action_id: tool.write\n      actor_kinds: [agent]\n      required_scopes: [tool:write]\n"
	network := policy.Event{Category: policy.NetworkAccessRequested, ActionID: "network.connect", Domain: "api.example", TraceID: "network-trace"}
	f := newRevisionFixtureFor(t, yaml, network, "network:api.example,tool:write")
	f.manager.SetCoordinator(f.coordinator)
	if _, err := f.coordinator.Resume(context.Background(), f.original.ID); err != nil {
		t.Fatal(err)
	}
	tool := policy.Event{Category: policy.ToolCallRequested, ActionID: "tool.write", ActionType: "destructive_write", Tool: "write", TraceID: "old-tool-trace"}
	requirement := policy.IdentityRequirement{Required: true, ActionID: tool.ActionID}
	decision, _, request, err := f.manager.EvaluateAndRecordWithIdentity(f.original.ID, tool, policy.BudgetState{}, requirement, f.at.Add(4*time.Second))
	if err != nil || decision.Outcome != policy.RequireApproval {
		t.Fatalf("old request: %v", err)
	}
	approval, err := f.manager.RequestApproval(context.Background(), run.Approval{RunID: f.original.ID, RequestAuditID: request.ID, Category: tool.Category, Tool: tool.Tool, ActionType: tool.ActionType, ReasonCode: policy.ReasonApprovalRequired}, f.at.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.DecideApproval(approval.ID, run.ApprovalApproved, "approved", identity.ActorIdentity{ID: "action-human", Kind: identity.Human, Subject: "operator"}, f.at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	adapter := &exactRetryAdapter{}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, adapter)
	if _, err = f.coordinator.ApplyPolicyRevision(context.Background(), f.proposal.ID, f.decider); err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.RetryPolicyAction(context.Background(), f.original.ID, policy.BudgetState{}); err != nil {
		t.Fatal(err)
	}
	tool.ApprovalID = approval.ID
	_, _, dispatch, err := f.manager.EvaluateAndRecordWithIdentity(f.original.ID, tool, policy.BudgetState{}, requirement, f.at.Add(6*time.Second))
	if err == nil {
		_, err = f.store.ConsumeApprovalForInvocation(approval.ID, f.original.ID, dispatch.ID, f.at.Add(6*time.Second))
	}
	if err == nil {
		t.Fatal("historical action approval consumed for newest revision")
	}
	unconsumed, _ := f.store.GetApproval(approval.ID)
	if !unconsumed.ConsumedAt.IsZero() {
		t.Fatal("stale approval consumed")
	}
	if _, err = f.store.Events(f.original.ID); err != nil {
		t.Fatalf("historical audits unreadable: %v", err)
	}
}

type qualityChangingAdapter struct {
	before func()
	target *exactRetryAdapter
}

func (a qualityChangingAdapter) Prepare(_ context.Context, e policy.Event, _ invocation.InvocationContext) (run.PreparedPolicyRetry, error) {
	a.before()
	return &exactPreparedRetry{a.target, e}, nil
}
func TestQualityRetryDoesNotAuthorizeDifferentPreparedRevision(t *testing.T) {
	ctx := context.Background()
	f := newRevisionFixtureFor(t, "version: v1\nkind: agent-policy\nid: review-race\nname: Review race\ntools:\n  allow: [read]\nnetwork:\n  allowed_domains: [existing.example]\n", policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect", TraceID: "first-trace"}, "network:api.example,network:second.example")
	first := &exactRetryAdapter{}
	_ = f.coordinator.ConfigurePolicyRevisions(f.decider, nil, first)
	applied, err := f.coordinator.ApplyPolicyRevision(ctx, f.proposal.ID, f.decider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.RetryPolicyAction(ctx, applied.ID, policy.BudgetState{}); err != nil {
		t.Fatal(err)
	}
	actor := identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}
	d, _ := run.NewPermissionProposalDecider(f.store, &operatorFixture{actor: actor, authorized: true}, nil, func() time.Time { return f.at.Add(6 * time.Second) })
	outer, _ := run.NewCoordinator(f.store, f.provider)
	observed := &exactRetryAdapter{}
	hook := func() { qualityApplySecondRevision(t, f, d) }
	_ = outer.ConfigurePolicyRevisions(d, nil, qualityChangingAdapter{hook, observed})
	got, err := outer.RetryPolicyAction(ctx, applied.ID, policy.BudgetState{})
	if err == nil || observed.effects != 0 {
		chain, inspectErr := f.store.InspectPolicyRevision(applied.ID)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		t.Fatalf("stale prepared action dispatched: err=%v effects=%d dispatched=%s latest_retry_audit=%s latest_state=%s", err, observed.effects, observed.event.Domain, chain[1].RetryRequest.Domain, got.PolicyRevisions.Mutations[1].Retry.State)
	}
	qualityAssertStaleRetry(t, got, err, observed)
	qualityRetryFreshRevision(t, f, d)
}

func qualityApplySecondRevision(t *testing.T, f revisionFixture, d *run.PermissionProposalDecider) {
	t.Helper()
	ctx := context.Background()
	applied := f.original
	e := policy.Event{Category: policy.NetworkAccessRequested, Domain: "second.example", ActionID: "network.connect", TraceID: "second-trace"}
	verdict, _, denial, err := f.manager.EvaluateAndRecord(applied.ID, e, policy.BudgetState{}, f.at.Add(4*time.Second))
	if err != nil || verdict.Outcome != policy.Deny {
		t.Fatalf("second deny %v %v", verdict, err)
	}
	p, err := f.manager.RequestPermissionProposal(run.PermissionProposalRequest{RunID: applied.ID, DenialAuditID: denial.ID, Difference: run.PermissionDifference{Category: e.Category, Domain: e.Domain, ActionID: e.ActionID}, ExpiresAt: f.at.Add(10 * time.Minute)}, f.at.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Decide(ctx, p.ID, run.ProposalApproved); err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.Pause(ctx, applied.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.ApplyPolicyRevision(ctx, p.ID, d); err != nil {
		t.Fatal(err)
	}
}

// Another coordinator may finish the first retry after resume, then apply a new
// revision before the original coordinator claims dispatch.
type qualityDispatchStore struct {
	*run.Store
	before func()
}

func (s *qualityDispatchStore) BeginPolicyRetryDispatch(ctx context.Context, id string, budget policy.BudgetState, d *run.PermissionProposalDecider, binding run.PolicyRetryBinding) (run.Record, error) {
	s.before()
	return s.Store.BeginPolicyRetryDispatch(ctx, id, budget, d, binding)
}
func TestPolicyRetryRejectsRevisionChangedAfterResume(t *testing.T) {
	ctx := context.Background()
	f := newRevisionFixtureFor(t, "version: v1\nkind: agent-policy\nid: review-resume-race\nname: Review race\ntools:\n  allow: [read]\nnetwork:\n  allowed_domains: [existing.example]\n", policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect", TraceID: "first-trace"}, "network:api.example,network:second.example")
	actor := identity.ActorIdentity{ID: "human-1", Kind: identity.Human, Subject: "operator"}
	d, _ := run.NewPermissionProposalDecider(f.store, &operatorFixture{actor: actor, authorized: true}, nil, func() time.Time { return f.at.Add(6 * time.Second) })
	other := &exactRetryAdapter{}
	_ = f.coordinator.ConfigurePolicyRevisions(d, nil, other)
	if _, err := f.coordinator.ApplyPolicyRevision(ctx, f.proposal.ID, d); err != nil {
		t.Fatal(err)
	}
	wrapper := &qualityDispatchStore{Store: f.store}
	wrapper.before = func() {
		if _, err := f.coordinator.RetryPolicyAction(ctx, f.original.ID, policy.BudgetState{}); err != nil {
			t.Fatal(err)
		}
		qualityApplySecondRevision(t, f, d)
	}
	outer, _ := run.NewCoordinator(wrapper, f.provider)
	observed := &exactRetryAdapter{}
	_ = outer.ConfigurePolicyRevisions(d, nil, observed)
	got, err := outer.RetryPolicyAction(ctx, f.original.ID, policy.BudgetState{})
	qualityAssertStaleRetry(t, got, err, observed)
	if other.effects != 1 {
		t.Fatalf("other retry effects=%d", other.effects)
	}
	qualityRetryFreshRevision(t, f, d)
}
func qualityAssertStaleRetry(t *testing.T, got run.Record, err error, observed *exactRetryAdapter) {
	t.Helper()
	if !errors.Is(err, run.ErrPolicyRetryStalePreparation) || observed.effects != 0 || got.Status != run.Paused || got.PolicyRevisions.Mutations[1].Retry != nil {
		t.Fatalf("stale preparation changed runtime/retry: err=%v effects=%d record=%+v", err, observed.effects, got)
	}
}
func qualityRetryFreshRevision(t *testing.T, f revisionFixture, d *run.PermissionProposalDecider) {
	t.Helper()
	fresh, _ := run.NewCoordinator(f.store, f.provider)
	adapter := &exactRetryAdapter{}
	_ = fresh.ConfigurePolicyRevisions(d, nil, adapter)
	if _, err := fresh.RetryPolicyAction(context.Background(), f.original.ID, policy.BudgetState{}); err != nil {
		t.Fatal(err)
	}
	if adapter.effects != 1 || adapter.event.Domain != "second.example" {
		t.Fatalf("fresh retry: effects=%d event=%+v", adapter.effects, adapter.event)
	}
}
