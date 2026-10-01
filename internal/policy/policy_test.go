package policy_test

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

const validPolicy = `version: v1
kind: agent-policy
id: default-safe-policy
name: Default Safe Policy
tools:
  allow: [read_file, search, github.update_file]
  deny: [delete_repository]
approval:
  required_for: [destructive_write]
network:
  allowed_domains: [github.com, api.github.com]
  denied_domains: []
credentials:
  allowed_scopes: [github:read]
  denied_scopes: [production:admin]
subagents:
  max_concurrent: 3
  max_total: 10
budget:
  max_duration_seconds: 3600
  max_cost_usd: 5.0
  max_tool_calls: 200
termination:
  kill_on: [denied_tool_call, denied_domain_access]
enforcement:
  optional_controls: []
`

func TestLoadRejectsUnknownPolicyFieldsAndUnsupportedVersions(t *testing.T) {
	unknown := strings.Replace(validPolicy, "name: Default Safe Policy", "name: Default Safe Policy\nunknown_control: true", 1)
	if _, err := policy.Load(strings.NewReader(unknown)); err == nil {
		t.Fatal("Load accepted an unknown security-relevant field")
	}
	unsupported := strings.Replace(validPolicy, "version: v1", "version: v9", 1)
	if _, err := policy.Load(strings.NewReader(unsupported)); err == nil {
		t.Fatal("Load accepted an unsupported policy version")
	}
	if _, err := policy.Load(strings.NewReader(validPolicy + "---\n" + validPolicy)); err == nil {
		t.Fatal("Load accepted multiple YAML documents")
	}
}

func TestLoadAcceptsV2ExactActionIdentityRules(t *testing.T) {
	contents := strings.Replace(validPolicy, "version: v1", "version: v2", 1) + `identity:
  rules:
    - action_id: github.read
      actor_kinds: [human]
      roles: [operator]
      required_scopes: [github:read]
`
	configured, err := policy.Load(strings.NewReader(contents))
	if err != nil {
		t.Fatalf("Load rejected valid v2 identity rules: %v", err)
	}
	if configured.Version != policy.Version2 || configured.Identity == nil || len(configured.Identity.Rules) != 1 || configured.Identity.Rules[0].ActionID != "github.read" {
		t.Fatalf("loaded v2 identity policy = %#v", configured)
	}
}

func TestLoadRejectsInvalidIdentityRuleContracts(t *testing.T) {
	base := strings.Replace(validPolicy, "version: v1", "version: v2", 1)
	cases := []struct {
		name string
		body string
	}{
		{name: "identity section on v1", body: strings.Replace(validPolicy, "enforcement:", "identity: {}\nenforcement:", 1)},
		{name: "wildcard action", body: base + "identity:\n  rules:\n    - action_id: github.*\n      actor_kinds: [human]\n"},
		{name: "wildcard scope", body: base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n      required_scopes: [github:*]\n"},
		{name: "duplicate action", body: base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n    - action_id: github.read\n      actor_kinds: [agent]\n"},
		{name: "empty actor kinds", body: base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: []\n"},
		{name: "unknown rule field", body: base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n      trust_everything: true\n"},
		{name: "credential-like role", body: base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n      roles: [ghp_012345678901234567890123456789]\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := policy.Load(strings.NewReader(test.body)); err == nil {
				t.Fatal("Load accepted invalid identity rule contract")
			}
		})
	}
}

func TestPolicySnapshotHashIgnoresRuleListOrder(t *testing.T) {
	first, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	reordered := strings.Replace(validPolicy, "[read_file, search, github.update_file]", "[github.update_file, read_file, search]", 1)
	reordered = strings.Replace(reordered, "[github.com, api.github.com]", "[api.github.com, github.com]", 1)
	second, err := policy.Load(strings.NewReader(reordered))
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, err := policy.Resolve(first, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := policy.Resolve(second, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if firstSnapshot.Hash != secondSnapshot.Hash {
		t.Fatalf("equivalent policy hashes differ: %q != %q", firstSnapshot.Hash, secondSnapshot.Hash)
	}
}

func TestV2IdentityRulesUseCanonicalSnapshotHash(t *testing.T) {
	base := strings.Replace(validPolicy, "version: v1", "version: v2", 1)
	first := base + "identity:\n  rules:\n    - action_id: github.write\n      actor_kinds: [agent, human]\n      roles: [writer, operator]\n      required_scopes: [github:write, github:read]\n    - action_id: github.read\n      actor_kinds: [human]\n      roles: [operator]\n      required_scopes: [github:read]\n"
	second := base + "identity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n      roles: [operator]\n      required_scopes: [github:read]\n    - action_id: github.write\n      actor_kinds: [human, agent]\n      roles: [operator, writer]\n      required_scopes: [github:read, github:write]\n"
	loadSnapshot := func(contents string) policy.Snapshot {
		t.Helper()
		configured, err := policy.Load(strings.NewReader(contents))
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	firstSnapshot := loadSnapshot(first)
	secondSnapshot := loadSnapshot(second)
	if firstSnapshot.Hash != secondSnapshot.Hash {
		t.Fatalf("equivalent v2 identity policies hash differently: %q != %q", firstSnapshot.Hash, secondSnapshot.Hash)
	}

	changed := strings.Replace(second, "required_scopes: [github:read, github:write]", "required_scopes: [github:read]", 1)
	changedSnapshot := loadSnapshot(changed)
	if firstSnapshot.Hash == changedSnapshot.Hash {
		t.Fatal("changing an identity scope did not change the v2 policy hash")
	}
}

func TestV1PolicySnapshotHashRemainsStable(t *testing.T) {
	input := "version: v1\nkind: agent-policy\nid: stable-hash\nname: Stable Hash\ntools:\n  allow: [github.read]\n"
	configured, err := policy.Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:3134db935b2f01a18bc65e8728352b19861a23551d2d26fa3b84d4fcae15eb77"
	if snapshot.Hash != want {
		t.Fatalf("v1 snapshot hash = %q, want original v1 hash %q", snapshot.Hash, want)
	}
}

func identitySnapshot(t *testing.T, id, tools, approvalRules, identityRules string, now time.Time) policy.Snapshot {
	t.Helper()
	input := "version: v2\nkind: agent-policy\nid: " + id + "\nname: Identity Policy\ntools:\n  allow: [" + tools + "]\n"
	if approvalRules != "" {
		input += "approval:\n" + approvalRules + "\n"
	}
	input += "identity:\n  rules:\n" + identityRules
	configured, err := policy.Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, now)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestEvaluateBeforeWithIdentityRequiresExactActorAndDelegationMatch(t *testing.T) {
	now := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	rules := "    - action_id: github.read\n      actor_kinds: [human]\n      roles: [operator]\n      required_scopes: [github:read]\n" +
		"    - action_id: github.write\n      actor_kinds: [human]\n      roles: [operator]\n      required_scopes: [github:write]\n"
	snapshot := identitySnapshot(t, "identity-policy", "github.read, github.write", "  required_for: [destructive_write]", rules, now)
	engine := policy.Engine{}
	event := policy.Event{Category: policy.ToolCallRequested, Tool: "github.read", ActionID: "github.read"}
	selection := func(kind identity.ActorKind, roles, scopes []string, expiry time.Time) identity.Selection {
		actor := identity.ActorIdentity{ID: "allen", Kind: kind, Subject: "allen@local", Roles: roles}
		delegation := identity.Delegation{ID: "del-1", ActorID: actor.ID, RunID: "run-1", Scopes: scopes, ExpiresAt: expiry}
		return identity.NamedSelection(actor, delegation)
	}
	requirement := policy.IdentityRequirement{ActionID: "github.read", Required: true}
	cases := []struct {
		name     string
		identity identity.Selection
		want     policy.ReasonCode
	}{
		{name: "actor kind mismatch", identity: selection(identity.Agent, []string{"operator"}, []string{"github:read"}, now.Add(time.Hour)), want: policy.ReasonIdentityPolicyDenied},
		{name: "role mismatch", identity: selection(identity.Human, []string{"reader"}, []string{"github:read"}, now.Add(time.Hour)), want: policy.ReasonIdentityPolicyDenied},
		{name: "missing exact scope", identity: selection(identity.Human, []string{"operator"}, []string{"github"}, now.Add(time.Hour)), want: policy.ReasonDelegationScopeMissing},
		{name: "expired delegation", identity: selection(identity.Human, []string{"operator"}, []string{"github:read"}, now), want: policy.ReasonDelegationExpired},
		{name: "anonymous", identity: identity.AnonymousSelection(), want: policy.ReasonIdentityRequired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := engine.EvaluateBeforeWithIdentity(snapshot, event, policy.BudgetState{}, requirement, "run-1", test.identity, now)
			if decision.Outcome != policy.Deny || decision.ReasonCode != test.want {
				t.Fatalf("decision = %#v, want DENY/%s", decision, test.want)
			}
		})
	}
	valid := selection(identity.Human, []string{"operator"}, []string{"github:read"}, now.Add(time.Hour))
	allowed := engine.EvaluateBeforeWithIdentity(snapshot, event, policy.BudgetState{}, requirement, "run-1", valid, now)
	if allowed.Outcome != policy.Allow {
		t.Fatalf("matching identity decision = %#v, want ALLOW", allowed)
	}

	missingRule := requirement
	missingRule.ActionID = "github.delete"
	missingRuleEvent := event
	missingRuleEvent.ActionID = "github.delete"
	decision := engine.EvaluateBeforeWithIdentity(snapshot, missingRuleEvent, policy.BudgetState{}, missingRule, "run-1", valid, now)
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonIdentityPolicyRuleMissing {
		t.Fatalf("missing-rule decision = %#v", decision)
	}
	mismatchedAction := engine.EvaluateBeforeWithIdentity(snapshot, event, policy.BudgetState{}, missingRule, "run-1", valid, now)
	if mismatchedAction.Outcome != policy.Deny || mismatchedAction.ReasonCode != policy.ReasonIdentityPolicyDenied {
		t.Fatalf("mismatched action identity decision = %#v", mismatchedAction)
	}

	v1, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	v1Snapshot, _ := policy.Resolve(v1, now)
	decision = engine.EvaluateBeforeWithIdentity(v1Snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file", ActionID: "github.read"}, policy.BudgetState{}, requirement, "run-1", valid, now)
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonIdentityPolicyUnsupportedVersion {
		t.Fatalf("v1 identity decision = %#v", decision)
	}

	denied := engine.EvaluateBeforeWithIdentity(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "not_allowed"}, policy.BudgetState{}, requirement, "run-1", identity.AnonymousSelection(), now)
	if denied.Outcome != policy.Deny || denied.ReasonCode != policy.ReasonToolNotAllowlisted {
		t.Fatalf("ordinary policy deny was replaced by identity failure: %#v", denied)
	}
}

func TestIdentityGatePreservesApprovalAfterConditionsPass(t *testing.T) {
	now := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	rules := "    - action_id: github.write\n      actor_kinds: [human]\n      roles: [operator]\n      required_scopes: [github:write]\n"
	snapshot := identitySnapshot(t, "approval-identity", "github.write", "  required_for: [destructive_write]", rules, now)
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local", Roles: []string{"operator"}}
	delegation := identity.Delegation{ID: "del-1", ActorID: actor.ID, RunID: "run-1", Scopes: []string{"github:write"}, ExpiresAt: now.Add(time.Hour)}
	decision := (policy.Engine{}).EvaluateBeforeWithIdentity(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "github.write", ActionType: "destructive_write", ActionID: "github.write"}, policy.BudgetState{}, policy.IdentityRequirement{ActionID: "github.write", Required: true}, "run-1", identity.NamedSelection(actor, delegation), now)
	if decision.Outcome != policy.RequireApproval || decision.ReasonCode != policy.ReasonApprovalRequired {
		t.Fatalf("identity-checked approval decision = %#v", decision)
	}
}

func TestEvaluateToolDefaultDenyAndDenyPrecedence(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.Engine{}
	cases := []struct {
		name   string
		tool   string
		want   policy.Outcome
		reason policy.ReasonCode
	}{
		{name: "explicitly allowed", tool: "read_file", want: policy.Allow},
		{name: "not in allow list", tool: "open_shell", want: policy.Deny, reason: policy.ReasonToolNotAllowlisted},
		{name: "explicitly denied", tool: "delete_repository", want: policy.Deny, reason: policy.ReasonToolDenied},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := engine.EvaluateBefore(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: test.tool}, policy.BudgetState{})
			if decision.Outcome != test.want || decision.ReasonCode != test.reason {
				t.Fatalf("decision = %#v, want outcome %q reason %q", decision, test.want, test.reason)
			}
		})
	}
	overlapPolicy := strings.Replace(validPolicy, "allow: [read_file, search, github.update_file]", "allow: [read_file, search, github.update_file, delete_repository]", 1)
	overlap, err := policy.Load(strings.NewReader(overlapPolicy))
	if err != nil {
		t.Fatal(err)
	}
	overlapSnapshot, err := policy.Resolve(overlap, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	decision := engine.EvaluateBefore(overlapSnapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "delete_repository"}, policy.BudgetState{})
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonToolDenied {
		t.Fatalf("explicit deny did not win over allow: %#v", decision)
	}
}

func TestDecisionValidationRequiresStableOutcomeAndReason(t *testing.T) {
	for _, decision := range []policy.Decision{
		{Outcome: policy.Outcome("INVALID")},
		{Outcome: policy.Deny},
		{Outcome: policy.RequireApproval, ReasonCode: policy.ReasonCode("caller-text")},
	} {
		if err := decision.Validate(); err == nil {
			t.Errorf("invalid decision was accepted: %#v", decision)
		}
	}
	if err := (policy.Decision{Outcome: policy.Deny, ReasonCode: policy.ReasonToolNotAllowlisted}).Validate(); err != nil {
		t.Fatalf("valid deny decision rejected: %v", err)
	}
}

func TestEvaluateApprovalAndExactNetworkCredentialRules(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.Engine{}
	approval := engine.EvaluateBefore(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write"}, policy.BudgetState{})
	if approval.Outcome != policy.RequireApproval || approval.ReasonCode != policy.ReasonApprovalRequired {
		t.Fatalf("approval decision = %#v", approval)
	}
	for _, test := range []struct {
		name    string
		event   policy.Event
		outcome policy.Outcome
		reason  policy.ReasonCode
	}{
		{name: "exact network host", event: policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.github.com"}, outcome: policy.Allow},
		{name: "unlisted subdomain", event: policy.Event{Category: policy.NetworkAccessRequested, Domain: "docs.github.com"}, outcome: policy.Deny, reason: policy.ReasonDomainNotAllowed},
		{name: "denied credential", event: policy.Event{Category: policy.CredentialAccessRequested, CredentialScope: "production:admin"}, outcome: policy.Deny, reason: policy.ReasonCredentialDenied},
		{name: "unlisted credential", event: policy.Event{Category: policy.CredentialAccessRequested, CredentialScope: "github:write"}, outcome: policy.Deny, reason: policy.ReasonCredentialNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := engine.EvaluateBefore(snapshot, test.event, policy.BudgetState{})
			if decision.Outcome != test.outcome || decision.ReasonCode != test.reason {
				t.Fatalf("decision = %#v, want outcome %q reason %q", decision, test.outcome, test.reason)
			}
		})
	}
	decision := engine.EvaluateBefore(snapshot, policy.Event{Category: policy.NetworkAccessRequested, Domain: "github.com"}, policy.BudgetState{})
	if decision.Outcome != policy.Allow {
		t.Fatalf("explicitly allowed domain = %#v", decision)
	}
	deniedPolicy := strings.Replace(validPolicy, "denied_domains: []", "denied_domains: [api.github.com]", 1)
	deniedConfig, err := policy.Load(strings.NewReader(deniedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	deniedSnapshot, err := policy.Resolve(deniedConfig, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	decision = engine.EvaluateBefore(deniedSnapshot, policy.Event{Category: policy.NetworkAccessRequested, Domain: "api.github.com"}, policy.BudgetState{})
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonDomainDenied {
		t.Fatalf("explicit network deny did not win over allow: %#v", decision)
	}
	deniedCredentials := strings.Replace(validPolicy, "denied_scopes: [production:admin]", "denied_scopes: [github:read]", 1)
	credentialConfig, err := policy.Load(strings.NewReader(deniedCredentials))
	if err != nil {
		t.Fatal(err)
	}
	credentialSnapshot, err := policy.Resolve(credentialConfig, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	decision = engine.EvaluateBefore(credentialSnapshot, policy.Event{Category: policy.CredentialAccessRequested, CredentialScope: "github:read"}, policy.BudgetState{})
	if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonCredentialDenied {
		t.Fatalf("explicit credential deny did not win over allow: %#v", decision)
	}
}

func TestEveryConfiguredApprovalActionReturnsStableReason(t *testing.T) {
	for _, action := range []string{"destructive_write", "external_publish", "credential_escalation"} {
		t.Run(action, func(t *testing.T) {
			configuredYAML := strings.Replace(validPolicy, "required_for: [destructive_write]", "required_for: ["+action+"]", 1)
			configured, err := policy.Load(strings.NewReader(configuredYAML))
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
			if err != nil {
				t.Fatal(err)
			}
			decision := (policy.Engine{}).EvaluateBefore(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: action}, policy.BudgetState{})
			if decision.Outcome != policy.RequireApproval || decision.ReasonCode != policy.ReasonApprovalRequired {
				t.Fatalf("approval decision = %#v", decision)
			}
		})
	}
}

func TestEvaluationIsDeterministicForSnapshotAndNormalizedEvent(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	event := policy.Event{Category: policy.ToolCallRequested, Tool: "unknown_tool", ActionType: "inspect"}
	engine := policy.Engine{}
	want := engine.EvaluateBefore(snapshot, event, policy.BudgetState{})
	for i := 0; i < 25; i++ {
		if got := engine.EvaluateBefore(snapshot, event, policy.BudgetState{}); !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d decision = %#v, want %#v", i, got, want)
		}
	}
}

func TestEvaluateDurationCostToolCallAndSubagentBudgets(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.Engine{}
	cases := []struct {
		name   string
		event  policy.Event
		state  policy.BudgetState
		reason policy.ReasonCode
	}{
		{name: "time", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{Elapsed: time.Hour}, reason: policy.ReasonDurationBudgetExceeded},
		{name: "time over limit", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{Elapsed: time.Hour + time.Second}, reason: policy.ReasonDurationBudgetExceeded},
		{name: "tool calls", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{ToolCalls: 200}, reason: policy.ReasonToolCallBudgetExceeded},
		{name: "tool calls over limit", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{ToolCalls: 201}, reason: policy.ReasonToolCallBudgetExceeded},
		{name: "cost", event: policy.Event{Category: policy.BudgetUpdated}, state: policy.BudgetState{CostUSD: 5.0}, reason: policy.ReasonCostBudgetExceeded},
		{name: "cost over limit", event: policy.Event{Category: policy.BudgetUpdated}, state: policy.BudgetState{CostUSD: 5.01}, reason: policy.ReasonCostBudgetExceeded},
		{name: "concurrent agents", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{ConcurrentSubagents: 3}, reason: policy.ReasonConcurrentSubagentsExceeded},
		{name: "concurrent agents over limit", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{ConcurrentSubagents: 4}, reason: policy.ReasonConcurrentSubagentsExceeded},
		{name: "total agents", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{TotalSubagents: 10}, reason: policy.ReasonTotalSubagentsExceeded},
		{name: "total agents over limit", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{TotalSubagents: 11}, reason: policy.ReasonTotalSubagentsExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := engine.EvaluateBefore(snapshot, test.event, test.state)
			if decision.Outcome != policy.Deny || decision.ReasonCode != test.reason {
				t.Fatalf("decision = %#v, want deny reason %q", decision, test.reason)
			}
		})
	}
	for _, test := range []struct {
		name  string
		event policy.Event
		state policy.BudgetState
	}{
		{name: "below duration", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{Elapsed: time.Hour - time.Nanosecond}},
		{name: "below cost", event: policy.Event{Category: policy.BudgetUpdated}, state: policy.BudgetState{CostUSD: 4.99}},
		{name: "below tool calls", event: policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state: policy.BudgetState{ToolCalls: 199}},
		{name: "below concurrent agents", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{ConcurrentSubagents: 2}},
		{name: "below total agents", event: policy.Event{Category: policy.SubAgentSpawnRequested}, state: policy.BudgetState{TotalSubagents: 9}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := engine.EvaluateBefore(snapshot, test.event, test.state)
			if decision.Outcome != policy.Allow {
				t.Fatalf("under-budget decision = %#v", decision)
			}
		})
	}
}

func TestEvaluateRejectsInvalidBudgetState(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []policy.BudgetState{{CostUSD: math.NaN()}, {CostUSD: -1}, {Elapsed: -time.Second}, {ToolCalls: -1}, {ConcurrentSubagents: -1}, {TotalSubagents: -1}} {
		decision := (policy.Engine{}).EvaluateBefore(snapshot, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, state)
		if decision.Outcome != policy.Deny || decision.ReasonCode != policy.ReasonInvalidBudgetState {
			t.Errorf("invalid state %#v produced %#v", state, decision)
		}
	}
}

func TestCheckCapabilitiesFailsMandatoryAndWarnsOptional(t *testing.T) {
	yaml := strings.Replace(validPolicy, "optional_controls: []", "optional_controls: [network_restriction]", 1)
	configured, err := policy.Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := map[policy.Control]bool{
		policy.ControlToolInterception:    true,
		policy.ControlApprovalPauseResume: true,
		policy.ControlDurationBudget:      true,
		policy.ControlCostBudget:          true,
		policy.ControlToolCallBudget:      true,
		policy.ControlSubagentLimits:      true,
		policy.ControlNetworkRestriction:  true,
		policy.ControlCredentialScope:     true,
		policy.ControlRunTermination:      true,
		policy.ControlRuntimeEvents:       true,
	}
	capabilities[policy.ControlToolInterception] = false
	capabilities[policy.ControlNetworkRestriction] = false
	report := policy.CheckCapabilities(configured, capabilities)
	if report.Ready {
		t.Fatal("preflight allowed an unsupported mandatory tool control")
	}
	if len(report.Missing) != 1 || report.Missing[0] != policy.ControlToolInterception {
		t.Fatalf("missing controls = %#v", report.Missing)
	}
	if len(report.Warnings) != 1 || report.Warnings[0] != policy.ControlNetworkRestriction {
		t.Fatalf("optional warnings = %#v", report.Warnings)
	}
}

func TestEvaluateAfterReportsCompletedDeniedToolAsViolation(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	evaluation := (policy.Engine{}).EvaluateAfter(snapshot, policy.Event{
		Category:       policy.ToolCallCompleted,
		Tool:           "open_shell",
		RequestAuditID: "evt-requested",
	}, policy.BudgetState{})
	if len(evaluation.Violations) != 1 || evaluation.Violations[0] != policy.EventUnexpectedToolCall {
		t.Fatalf("violations = %#v", evaluation.Violations)
	}
	if !evaluation.TerminationRequested {
		t.Fatal("evaluation did not request termination for a denied_tool_call trigger")
	}
}

func TestUnexpectedTerminationTriggerRequestsRunControl(t *testing.T) {
	configured, err := policy.Load(strings.NewReader(strings.Replace(validPolicy, "denied_domain_access]", "denied_domain_access, unexpected_termination]", 1)))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	evaluation := (policy.Engine{}).EvaluateAfter(snapshot, policy.Event{Category: policy.EventUnexpectedTermination}, policy.BudgetState{})
	if !evaluation.TerminationRequested {
		t.Fatal("unexpected termination trigger did not request run control")
	}
	if len(evaluation.Violations) != 1 || evaluation.Violations[0] != policy.EventUnexpectedTermination {
		t.Fatalf("violations = %#v", evaluation.Violations)
	}
}

func TestCompletedDeniedNetworkAccessRetainsTerminationTrigger(t *testing.T) {
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: network-guard\nname: Network Guard\ntools:\n  allow: [read_file]\nnetwork:\n  allowed_domains: [example.com]\ntermination:\n  kill_on: [denied_domain_access]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	evaluation := (policy.Engine{}).EvaluateAfter(snapshot, policy.Event{
		Category: policy.NetworkAccessCompleted, Domain: "blocked.example", RequestAuditID: "evt-requested",
		ObservedDecision: policy.Deny, ReasonCode: policy.ReasonDomainNotAllowed,
	}, policy.BudgetState{})
	if !evaluation.TerminationRequested {
		t.Fatal("completed denied network access lost its configured termination trigger")
	}
	if len(evaluation.Violations) != 1 || evaluation.Violations[0] != policy.EventUnexpectedNetwork {
		t.Fatalf("violations = %#v", evaluation.Violations)
	}
}

func TestAllBehavioralAnomalyEventCategoriesValidate(t *testing.T) {
	for _, event := range []policy.Event{
		{Category: policy.EventUnexpectedToolCall, Tool: "unknown_tool"},
		{Category: policy.EventUnexpectedNetwork, Domain: "unexpected.example"},
		{Category: policy.EventCredentialAccess, CredentialScope: "prod:read"},
		{Category: policy.EventRetryStorm},
		{Category: policy.EventProgressStall},
		{Category: policy.EventSubagentSpawnSpike},
		{Category: policy.EventBudgetExhaustion},
		{Category: policy.EventPolicyViolation},
		{Category: policy.EventUnexpectedTermination},
	} {
		if err := event.Validate(); err != nil {
			t.Errorf("event category %s failed validation: %v", event.Category, err)
		}
	}
}
