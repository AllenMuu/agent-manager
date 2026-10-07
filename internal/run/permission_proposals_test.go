package run_test

import (
	"context"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type proposalFixture struct {
	store      *run.Store
	manager    *run.Manager
	record     run.Record
	denial     run.AuditRecord
	at         time.Time
	difference run.PermissionDifference
}

func newProposalFixture(t *testing.T) proposalFixture {
	t.Helper()
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	s, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := run.NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: permission-test\nname: Permission test\ntools:\n  allow: [read]\nnetwork:\n  allowed_domains: [existing.example]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(p, at)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.ActorIdentity{ID: "agent-1", Kind: identity.Agent, Subject: "worker-1"}
	delegation := identity.Delegation{ID: "delegation-1", ActorID: actor.ID, Scopes: []string{"network:api.example"}, ExpiresAt: at.Add(time.Hour)}
	r, _, err := s.Start(snapshot, "offline", t.TempDir(), identity.NamedSelection(actor, delegation), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true}, at)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	event := policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect", TraceID: "trace-network"}
	d, _, audit, err := m.EvaluateAndRecord(r.ID, event, policy.BudgetState{}, at.Add(time.Second))
	if err != nil || d.Outcome != policy.Deny {
		t.Fatalf("denial: %v %v", d, err)
	}
	return proposalFixture{s, m, r, audit, at.Add(2 * time.Second), run.PermissionDifference{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect"}}
}
func (f proposalFixture) request() run.PermissionProposalRequest {
	return run.PermissionProposalRequest{RunID: f.record.ID, DenialAuditID: f.denial.ID, Difference: f.difference, ExpiresAt: f.at.Add(10 * time.Minute)}
}
func TestDeniedNetworkProposalPersistsExactLineageWithoutPolicyMutation(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := run.NewStore(f.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetPermissionProposal(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != run.ProposalPending || got.RunID != f.record.ID || !reflect.DeepEqual(got.Denial, f.denial) || !reflect.DeepEqual(got.Identity, f.record.Identity) || !reflect.DeepEqual(got.BasePolicy, f.record.Policy) || got.Difference != f.difference || !got.ExpiresAt.Equal(f.request().ExpiresAt) {
		t.Fatalf("proposal lineage: %#v", got)
	}
	unchanged, err := reopened.Get(f.record.ID)
	if err != nil || !reflect.DeepEqual(unchanged, f.record) {
		t.Fatalf("run mutated: %#v %v", unchanged, err)
	}
}

// Offline authority fixture: its result is chosen by the trusted test host.
// It models an external authority decision, not real human authentication.
type operatorFixture struct {
	actor      identity.ActorIdentity
	authorized bool
	alter      func(*run.OperatorReceipt)
}

func (o *operatorFixture) ResolveOperator(_ context.Context, request run.OperatorDecisionRequest) (run.OperatorReceipt, error) {
	receipt := run.OperatorReceipt{Actor: o.actor, Authorized: o.authorized, AuthorityID: "offline-test-host", ReceiptID: "receipt-1", ProposalID: request.ProposalID, RunID: request.RunID, Decision: request.Decision, DenialAuditID: request.DenialAuditID, BasePolicyHash: request.BasePolicyHash}
	if o.alter != nil {
		o.alter(&receipt)
	}
	return receipt, nil
}
func TestAgentHumanLabelsCannotEstablishProposalDecisionAuthority(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	spoof := identity.ActorIdentity{ID: "agent-1", Kind: identity.Human, Subject: "worker-1", Roles: []string{"approver"}}
	authority := &operatorFixture{actor: spoof, authorized: true}
	decider, err := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decider.Decide(context.Background(), p.ID, run.ProposalApproved); !errors.Is(err, run.ErrOperatorUnauthorized) {
		t.Fatalf("spoofed decision: %v", err)
	}
	got, err := f.store.GetPermissionProposal(p.ID)
	if err != nil || got.Status != run.ProposalPending {
		t.Fatalf("spoof approved: %#v %v", got, err)
	}
	unchanged, _ := f.store.Get(f.record.ID)
	if !reflect.DeepEqual(unchanged, f.record) {
		t.Fatal("spoof mutated run")
	}
}

func TestAuthorizedOperatorDecisionPersistsAuthorityAndOriginalProposal(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	human := identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "local-operator"}
	authority := &operatorFixture{actor: human, authorized: true}
	decider, err := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
	if err != nil {
		t.Fatal(err)
	}
	approved, err := decider.Decide(context.Background(), p.ID, run.ProposalApproved)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _ := run.NewStore(f.store.Root())
	got, err := reopened.GetPermissionProposal(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != run.ProposalApproved || got.Status != run.ProposalApproved || len(got.Decisions) != 1 {
		t.Fatalf("decision missing: %#v", got)
	}
	receipt := got.Decisions[0].Operator
	if receipt == nil || !reflect.DeepEqual(receipt.Actor, human) || receipt.ProposalID != p.ID || receipt.DenialAuditID != f.denial.ID || receipt.BasePolicyHash != f.record.Policy.Hash || got.Decisions[0].ID == "" {
		t.Fatalf("authority missing: %#v", got)
	}
	unchanged, _ := reopened.Get(f.record.ID)
	if !reflect.DeepEqual(unchanged, f.record) {
		t.Fatal("decision mutated policy or run")
	}
}

type currentPolicyFixture struct{ snapshot policy.Snapshot }

func (b *currentPolicyFixture) WithCurrentPolicy(_ context.Context, _ string, apply func(policy.Snapshot) error) error {
	return apply(b.snapshot)
}
func TestProposalDecisionAndUseRejectExpiredOrStaleAuthority(t *testing.T) {
	for _, phase := range []string{"decision", "use"} {
		for _, cause := range []string{"proposal", "delegation", "base"} {
			t.Run(phase+"/"+cause, func(t *testing.T) {
				f := newProposalFixture(t)
				request := f.request()
				if cause == "delegation" {
					request.ExpiresAt = f.at.Add(2 * time.Hour)
				}
				p, err := f.manager.RequestPermissionProposal(request, f.at)
				if err != nil {
					t.Fatal(err)
				}
				authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
				now := f.at.Add(time.Second)
				base := &currentPolicyFixture{snapshot: f.record.Policy}
				decider, err := run.NewPermissionProposalDecider(f.store, authority, base, func() time.Time { return now })
				if err != nil {
					t.Fatal(err)
				}
				if phase == "use" {
					if _, err = decider.Decide(context.Background(), p.ID, run.ProposalApproved); err != nil {
						t.Fatal(err)
					}
				}
				var expected error
				switch cause {
				case "proposal":
					now = request.ExpiresAt
					expected = run.ErrProposalExpired
				case "delegation":
					now = f.record.Identity.Delegation.ExpiresAt
					expected = run.ErrProposalDelegationExpired
				case "base":
					changed := f.record.Policy.Policy
					changed.Network = &policy.NetworkRules{AllowedDomains: []string{"elsewhere.example"}}
					base.snapshot, err = policy.Resolve(changed, f.at.Add(5*time.Second))
					if err != nil {
						t.Fatal(err)
					}
					expected = run.ErrProposalStaleBase
				}
				if phase == "decision" {
					_, err = decider.Decide(context.Background(), p.ID, run.ProposalApproved)
				} else {
					err = decider.ValidateUse(context.Background(), p.ID)
				}
				if !errors.Is(err, expected) {
					t.Fatalf("expected %v got %v", expected, err)
				}
				if phase == "decision" {
					got, e := f.store.GetPermissionProposal(p.ID)
					if e != nil || len(got.Decisions) != 1 || got.Decisions[0].Operator == nil || got.Decisions[0].Operator.Actor.ID != "operator-1" {
						t.Fatalf("authorized failed decision lost attribution: %#v %v", got, e)
					}
				}
				unchanged, _ := f.store.Get(f.record.ID)
				if !reflect.DeepEqual(unchanged, f.record) {
					t.Fatal("invalid authority mutated run")
				}
			})
		}
	}
}

func TestNetworkProposalCannotExceedExactDelegationCeiling(t *testing.T) {
	f := newProposalFixture(t)
	for _, domain := range []string{"sub.api.example", "ungranted.example"} {
		event := policy.Event{Category: policy.NetworkAccessRequested, Domain: domain, ActionID: "network.connect"}
		_, _, audit, err := f.manager.EvaluateAndRecord(f.record.ID, event, policy.BudgetState{}, f.at)
		if err != nil {
			t.Fatal(err)
		}
		request := f.request()
		request.DenialAuditID = audit.ID
		request.Difference.Domain = domain
		if _, err = f.manager.RequestPermissionProposal(request, f.at); !errors.Is(err, run.ErrProposalCeiling) {
			t.Fatalf("outside exact ceiling: %s %v", domain, err)
		}
	}
}

func credentialProposalFixture(t *testing.T, scope string) proposalFixture {
	t.Helper()
	f := newProposalFixture(t)
	p := f.record.Policy.Policy
	p.Credentials = &policy.CredentialRules{AllowedScopes: []string{"existing/read"}}
	snapshot, err := policy.Resolve(p, f.at)
	if err != nil {
		t.Fatal(err)
	}
	selection := identity.NamedSelection(*f.record.Identity.Actor, identity.Delegation{ID: "credential-delegation", ActorID: f.record.Identity.Actor.ID, Scopes: []string{"repo/write"}, ExpiresAt: f.at.Add(time.Hour)})
	r, _, err := f.store.Start(snapshot, "offline", t.TempDir(), selection, map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true, policy.ControlCredentialScope: true}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	f.record, err = f.store.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, f.denial, err = f.manager.EvaluateAndRecord(r.ID, policy.Event{Category: policy.CredentialAccessRequested, CredentialScope: scope, ActionID: "credential.read"}, policy.BudgetState{}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	f.difference = run.PermissionDifference{Category: policy.CredentialAccessRequested, CredentialScope: scope, ActionID: "credential.read"}
	return f
}
func TestCredentialProposalBindsExactSafeScopeAndCeiling(t *testing.T) {
	f := credentialProposalFixture(t, "repo/write")
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.store.GetPermissionProposal(p.ID)
	if err != nil || got.Denial.CredentialScope != "repo/write" || got.Difference.CredentialScope != "repo/write" {
		t.Fatalf("exact credential denial missing: %#v %v", got, err)
	}
	f = credentialProposalFixture(t, "repo/admin")
	if _, err = f.manager.RequestPermissionProposal(f.request(), f.at); !errors.Is(err, run.ErrProposalCeiling) {
		t.Fatalf("credential escalation: %v", err)
	}
}

func TestToolProposalRequiresProvenExactActionDelegationCeiling(t *testing.T) {
	for _, mode := range []string{"granted", "missing-scope", "empty-rule", "missing-rule"} {
		t.Run(mode, func(t *testing.T) {
			f := newProposalFixture(t)
			p := f.record.Policy.Policy
			p.Version = policy.Version2
			p.Identity = &policy.IdentityRules{Rules: []policy.IdentityRule{{ActionID: "repo.write", ActorKinds: []string{"agent"}, RequiredScopes: []string{"repo/write"}}}}
			if mode == "empty-rule" {
				p.Identity.Rules[0].RequiredScopes = nil
			}
			if mode == "missing-rule" {
				p.Identity.Rules[0].ActionID = "other.action"
			}
			snapshot, err := policy.Resolve(p, f.at)
			if err != nil {
				t.Fatal(err)
			}
			scopes := []string{"repo/write"}
			if mode == "missing-scope" {
				scopes = []string{"repo/read"}
			}
			selection := identity.NamedSelection(*f.record.Identity.Actor, identity.Delegation{ID: "tool-delegation", ActorID: f.record.Identity.Actor.ID, Scopes: scopes, ExpiresAt: f.at.Add(time.Hour)})
			r, _, err := f.store.Start(snapshot, "offline", t.TempDir(), selection, map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true}, f.at)
			if err != nil {
				t.Fatal(err)
			}
			f.record, _ = f.store.Get(r.ID)
			_, _, f.denial, err = f.manager.EvaluateAndRecordWithIdentity(r.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "repo.write", ActionID: "repo.write", ActionType: "write"}, policy.BudgetState{}, policy.IdentityRequirement{ActionID: "repo.write", Required: true}, f.at)
			if err != nil {
				t.Fatal(err)
			}
			f.difference = run.PermissionDifference{Category: policy.ToolCallRequested, Tool: "repo.write", ActionID: "repo.write", ActionType: "write"}
			proposal, err := f.manager.RequestPermissionProposal(f.request(), f.at)
			if mode == "granted" {
				if err != nil || proposal.Status != run.ProposalPending {
					t.Fatalf("delegated tool proposal: %#v %v", proposal, err)
				}
			} else if !errors.Is(err, run.ErrProposalCeiling) {
				t.Fatalf("unproven tool ceiling: %v", err)
			}
		})
	}
}

func TestProposalRejectionOrApprovalDoesNotAuthorizeRetryOrActionApproval(t *testing.T) {
	for _, status := range []run.ProposalStatus{run.ProposalRejected, run.ProposalApproved} {
		t.Run(string(status), func(t *testing.T) {
			f := newProposalFixture(t)
			p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
			if err != nil {
				t.Fatal(err)
			}
			authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
			decider, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
			got, err := decider.Decide(context.Background(), p.ID, status)
			if err != nil || got.Status != status {
				t.Fatalf("decision: %#v %v", got, err)
			}
			if _, err = f.store.GetApproval(p.ID); err == nil {
				t.Fatal("policy proposal became one-action approval")
			}
			approvals, err := f.store.ListApprovals(f.record.ID)
			if err != nil || len(approvals) != 0 {
				t.Fatalf("proposal changed action approvals: %#v %v", approvals, err)
			}
			retry := policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect"}
			decision, _, _, err := f.manager.EvaluateAndRecord(f.record.ID, retry, policy.BudgetState{}, f.at.Add(2*time.Second))
			if err != nil || decision.Outcome != policy.Deny {
				t.Fatalf("proposal authorized retry: %v %v", decision, err)
			}
			retry.ApprovalID = p.ID
			decision, _, _, err = f.manager.EvaluateAndRecord(f.record.ID, retry, policy.BudgetState{}, f.at.Add(3*time.Second))
			if err == nil && decision.Outcome != policy.Deny {
				t.Fatal("proposal id authorized action approval")
			}
			unchanged, _ := f.store.Get(f.record.ID)
			if !reflect.DeepEqual(unchanged, f.record) {
				t.Fatal("proposal changed run state or immutable policy")
			}
		})
	}
}

func TestProposalUseRechecksOriginalOperatorAuthority(t *testing.T) {
	for _, change := range []string{"revoked", "different-human", "same-id-different-subject"} {
		t.Run(change, func(t *testing.T) {
			f := newProposalFixture(t)
			p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
			if err != nil {
				t.Fatal(err)
			}
			authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
			decider, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
			if _, err = decider.Decide(context.Background(), p.ID, run.ProposalApproved); err != nil {
				t.Fatal(err)
			}
			if err = decider.ValidateUse(context.Background(), p.ID); err != nil {
				t.Fatalf("valid use eligibility: %v", err)
			}
			switch change {
			case "revoked":
				authority.authorized = false
			case "different-human":
				authority.actor.ID = "operator-2"
			case "same-id-different-subject":
				authority.actor.Subject = "another-subject"
			}
			if err = decider.ValidateUse(context.Background(), p.ID); !errors.Is(err, run.ErrOperatorUnauthorized) {
				t.Fatalf("changed authority authorized use: %v", err)
			}
		})
	}
}

func TestCanceledOperatorDecisionCannotApproveProposal(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
	decider, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = decider.Decide(ctx, p.ID, run.ProposalApproved); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decision: %v", err)
	}
	got, _ := f.store.GetPermissionProposal(p.ID)
	if got.Status != run.ProposalPending {
		t.Fatal("canceled decision approved")
	}
}

func TestProposalRejectsForeignOrDifferentDenialLineage(t *testing.T) {
	f := newProposalFixture(t)
	for _, dimension := range []string{"missing-audit", "foreign-run", "domain", "action", "category", "completion", "allow", "budget"} {
		t.Run(dimension, func(t *testing.T) {
			request := f.request()
			switch dimension {
			case "missing-audit":
				request.DenialAuditID = "evt-missing"
			case "foreign-run":
				other, _, err := f.store.Start(f.record.Policy, "offline", t.TempDir(), identity.NamedSelection(*f.record.Identity.Actor, identity.Delegation{ID: "other", ActorID: f.record.Identity.Actor.ID, Scopes: []string{"network:api.example"}, ExpiresAt: f.at.Add(time.Hour)}), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				request.RunID = other.ID
			case "domain":
				request.Difference.Domain = "other.example"
			case "action":
				request.Difference.ActionID = "network.other"
			case "category":
				request.Difference.Category = policy.ToolCallRequested
			case "completion":
				_, _, audit, err := f.manager.EvaluateAndRecord(f.record.ID, policy.Event{Category: policy.NetworkAccessCompleted, Domain: "api.example", ActionID: "network.connect", RequestAuditID: f.denial.ID}, policy.BudgetState{}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				request.DenialAuditID = audit.ID
			case "allow":
				_, _, audit, err := f.manager.EvaluateAndRecord(f.record.ID, policy.Event{Category: policy.NetworkAccessRequested, Domain: "existing.example", ActionID: "network.connect"}, policy.BudgetState{}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				request.DenialAuditID = audit.ID
				request.Difference.Domain = "existing.example"
			case "budget":
				p := f.record.Policy.Policy
				seconds := int64(1)
				p.Budget = &policy.BudgetLimits{MaxDurationSeconds: &seconds}
				snapshot, err := policy.Resolve(p, f.at)
				if err != nil {
					t.Fatal(err)
				}
				r, _, err := f.store.Start(snapshot, "offline", t.TempDir(), identity.NamedSelection(*f.record.Identity.Actor, identity.Delegation{ID: "budget", ActorID: f.record.Identity.Actor.ID, Scopes: []string{"network:api.example"}, ExpiresAt: f.at.Add(time.Hour)}), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true, policy.ControlDurationBudget: true}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				_, _, audit, err := f.manager.EvaluateAndRecord(r.ID, policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.example", ActionID: "network.connect"}, policy.BudgetState{Elapsed: time.Second}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				request.RunID = r.ID
				request.DenialAuditID = audit.ID
			}
			if _, err := f.manager.RequestPermissionProposal(request, f.at); err == nil {
				t.Fatal("unrelated denial accepted")
			}
		})
	}
}
func TestCredentialScopeAuditNeverPersistsSecretShapedValues(t *testing.T) {
	f := newProposalFixture(t)
	for _, scope := range []string{"api-key:abcdef", "secret:abcdef", "bearer:abcdef"} {
		_, _, _, err := f.manager.EvaluateAndRecord(f.record.ID, policy.Event{Category: policy.CredentialAccessRequested, CredentialScope: scope, ActionID: "credential.read"}, policy.BudgetState{}, f.at)
		if err == nil {
			t.Fatalf("secret-shaped credential scope accepted: %q", scope)
		}
	}
	events, err := f.store.Events(f.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.CredentialScope != "" {
			t.Fatalf("secret-shaped scope persisted: %#v", event)
		}
	}
}
func TestConcurrentProposalDecisionsAcrossStoresHaveOneTerminalDecision(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := run.NewStore(f.store.Root())
			if err != nil {
				results <- err
				return
			}
			authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true}
			d, err := run.NewPermissionProposalDecider(s, authority, nil, func() time.Time { return f.at.Add(time.Second) })
			if err != nil {
				results <- err
				return
			}
			_, err = d.Decide(context.Background(), p.ID, run.ProposalApproved)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	got, err := f.store.GetPermissionProposal(p.ID)
	if err != nil || successes != 1 || got.Status != run.ProposalApproved || len(got.Decisions) != 1 {
		t.Fatalf("concurrent decisions: successes=%d %#v %v", successes, got, err)
	}
}

func TestOperatorDecisionRejectsUnauthorizedOrUnboundAuthorityReceipts(t *testing.T) {
	for _, caseName := range []string{"unauthorized-human", "agent", "service", "proposal", "run", "denial", "base", "decision", "unsafe-receipt"} {
		t.Run(caseName, func(t *testing.T) {
			f := newProposalFixture(t)
			p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
			if err != nil {
				t.Fatal(err)
			}
			authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true, alter: func(receipt *run.OperatorReceipt) {
				switch caseName {
				case "unauthorized-human":
					receipt.Authorized = false
				case "agent":
					receipt.Actor.Kind = identity.Agent
				case "service":
					receipt.Actor.Kind = identity.Service
				case "proposal":
					receipt.ProposalID = "proposal-other"
				case "run":
					receipt.RunID = "run-other"
				case "denial":
					receipt.DenialAuditID = "evt-other"
				case "base":
					receipt.BasePolicyHash = "sha256:wrong"
				case "decision":
					receipt.Decision = run.ProposalRejected
				case "unsafe-receipt":
					receipt.ReceiptID = "token:secret"
				}
			}}
			d, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return f.at.Add(time.Second) })
			if _, err = d.Decide(context.Background(), p.ID, run.ProposalApproved); !errors.Is(err, run.ErrOperatorUnauthorized) {
				t.Fatalf("invalid receipt accepted: %v", err)
			}
			got, err := f.store.GetPermissionProposal(p.ID)
			if err != nil || got.Status != run.ProposalPending || len(got.Decisions) != 1 || got.Decisions[0].Operator != nil || got.Decisions[0].Outcome != "operator_unauthorized" {
				t.Fatalf("unauthorized attempt audit: %#v %v", got, err)
			}
		})
	}
}
func TestProposalRechecksClockAfterOperatorBoundaryReturns(t *testing.T) {
	f := newProposalFixture(t)
	request := f.request()
	p, err := f.manager.RequestPermissionProposal(request, f.at)
	if err != nil {
		t.Fatal(err)
	}
	now := f.at.Add(time.Second)
	authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true, alter: func(_ *run.OperatorReceipt) { now = request.ExpiresAt }}
	d, _ := run.NewPermissionProposalDecider(f.store, authority, nil, func() time.Time { return now })
	if _, err = d.Decide(context.Background(), p.ID, run.ProposalApproved); !errors.Is(err, run.ErrProposalExpired) {
		t.Fatalf("approval after boundary expiry: %v", err)
	}
}

type stableCurrentPolicyFixture struct {
	mu       sync.Mutex
	snapshot policy.Snapshot
}

func (b *stableCurrentPolicyFixture) WithCurrentPolicy(_ context.Context, _ string, apply func(policy.Snapshot) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return apply(b.snapshot)
}
func TestProposalDecisionResolvesOperatorInsideStableCurrentBaseBoundary(t *testing.T) {
	f := newProposalFixture(t)
	p, err := f.manager.RequestPermissionProposal(f.request(), f.at)
	if err != nil {
		t.Fatal(err)
	}
	base := &stableCurrentPolicyFixture{snapshot: f.record.Policy}
	authority := &operatorFixture{actor: identity.ActorIdentity{ID: "operator-1", Kind: identity.Human, Subject: "operator"}, authorized: true, alter: func(_ *run.OperatorReceipt) {
		if base.mu.TryLock() {
			base.mu.Unlock()
			t.Error("operator decision escaped stable current-base boundary")
		}
	}}
	d, _ := run.NewPermissionProposalDecider(f.store, authority, base, func() time.Time { return f.at.Add(time.Second) })
	if _, err = d.Decide(context.Background(), p.ID, run.ProposalApproved); err != nil {
		t.Fatal(err)
	}
	if err = d.ValidateUse(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
}
func TestNetworkProposalRejectsDestinationThatCannotFitCanonicalScope(t *testing.T) {
	f := newProposalFixture(t)
	domain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + ".example"
	_, _, audit, err := f.manager.EvaluateAndRecord(f.record.ID, policy.Event{Category: policy.NetworkAccessRequested, Domain: domain, ActionID: "network.connect"}, policy.BudgetState{}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	request := f.request()
	request.DenialAuditID = audit.ID
	request.Difference.Domain = domain
	if _, err = f.manager.RequestPermissionProposal(request, f.at); err == nil {
		t.Fatal("unrepresentable destination scope accepted")
	}
}
