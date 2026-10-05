package enforcement_test

import (
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"testing"
)

func request() enforcement.Request {
	return enforcement.Request{Version: "v1", Policy: policy.AgentPolicy{Version: "v1", Kind: "agent-policy", ID: "offline", Name: "Offline", Tools: policy.ToolRules{Deny: []string{"send"}}}}
}
func TestInspectIndirectPermissions(t *testing.T) {
	r := request()
	r.Contributions = []enforcement.Contribution{{Provider: "local", Permissions: map[enforcement.Dimension]enforcement.Permission{enforcement.Process: {State: "known", Allow: []string{"shell"}}, enforcement.Network: {State: "known", Allow: []string{"api.example:443"}}, enforcement.Credential: {State: "known", Allow: []string{"ref:mail/send"}}}}}
	v, err := enforcement.ResolvePermissions(r)
	if err != nil {
		t.Fatal(err)
	}
	if v[enforcement.Tool].Policy.Deny[0] != "send" || v[enforcement.Process].Contributions[0].Permission.Allow[0] != "shell" || v[enforcement.Network].Contributions[0].Permission.Allow[0] != "api.example:443" || v[enforcement.Credential].Contributions[0].Permission.Allow[0] != "ref:mail/send" || v[enforcement.Filesystem].State != "unknown" {
		t.Fatalf("independent view: %+v", v)
	}
}

func TestMandatoryCredentialGapRejectsBeforeExecution(t *testing.T) {
	r := request()
	r.Required = []enforcement.Dimension{enforcement.Network, enforcement.Credential}
	r.Provider = enforcement.Declaration{Name: "fixture", Kind: "execution", Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}, Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Tool: {Support: "supported", Verified: true, Update: "unsupported"}, enforcement.Network: {Support: "supported", Verified: true, Update: "live-update"}}}
	result := enforcement.Preflight(r)
	if result.Accepted || len(result.Errors) != 1 || result.Errors[0].Dimension != enforcement.Credential {
		t.Fatalf("missing credential mediation: %+v", result)
	}
}
func TestOptionalGapWarns(t *testing.T) {
	r := request()
	r.Optional = []enforcement.Dimension{enforcement.Filesystem}
	r.Provider = enforcement.Declaration{Kind: "execution", Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Tool: {Support: "supported", Verified: true, Update: "unsupported"}}, Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}}
	result := enforcement.Preflight(r)
	if !result.Accepted || len(result.Warnings) != 1 || result.Warnings[0].Dimension != enforcement.Filesystem {
		t.Fatalf("optional gap: %+v", result)
	}
}
func TestStaticFilesystemDynamicNetwork(t *testing.T) {
	c, err := enforcement.DiscoverCapabilities(enforcement.Declaration{Name: "fixture", Kind: "execution", Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}, Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Tool: {Support: "supported", Verified: true, Update: "unsupported"}, enforcement.Network: {Support: "supported", Verified: true, Update: "live-update"}, enforcement.Filesystem: {Support: "supported", Verified: true, Update: "recreate-required"}}})
	if err != nil || c[enforcement.Network].Update != "live-update" || c[enforcement.Filesystem].Update != "recreate-required" || c[enforcement.Process].Update != "unsupported" {
		t.Fatalf("dimension update modes: %+v %v", c, err)
	}
}
func TestNoopAndDirectoryCannotClaimProtection(t *testing.T) {
	for _, kind := range []string{"noop", "directory"} {
		r := request()
		r.Required = []enforcement.Dimension{enforcement.Tool}
		r.Provider = enforcement.Declaration{Name: "execution", Kind: kind, Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Tool: {Support: "supported", Verified: true, Update: "live-update"}}}
		result := enforcement.Preflight(r)
		if result.Accepted || result.Controls[enforcement.Tool].Support != "unsupported" {
			t.Fatalf("%s falsely active: %+v", kind, result)
		}
	}
}
func TestOfflinePreflightNormalizesRepeatedChecks(t *testing.T) {
	r := request()
	r.Required = []enforcement.Dimension{enforcement.Credential, enforcement.Network, enforcement.Credential}
	r.Optional = []enforcement.Dimension{enforcement.Process, enforcement.Filesystem}
	first := enforcement.Preflight(r)
	r.Required = []enforcement.Dimension{enforcement.Network, enforcement.Credential}
	r.Optional = []enforcement.Dimension{enforcement.Filesystem, enforcement.Process}
	second := enforcement.Preflight(r)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || len(first.Errors) != 5 || len(first.Warnings) != 2 {
		t.Fatalf("offline normalized results differ: %s / %s", a, b)
	}
}
func TestLegacyGovernanceRequirementsAreNotSilentlyIgnored(t *testing.T) {
	r := request()
	r.Provider = enforcement.Declaration{Kind: "execution"}
	result := enforcement.Preflight(r)
	if result.Accepted {
		t.Fatal("legacy tool/runtime-event controls must still fail closed")
	}
}
func TestEmbeddedNetworkConstraintRequiresDimensionProtection(t *testing.T) {
	r := request()
	r.Policy.Network = &policy.NetworkRules{AllowedDomains: []string{"example.com"}}
	r.Provider = enforcement.Declaration{Kind: "execution", Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Tool: {Support: "supported", Verified: true, Update: "unsupported"}}, Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true}}
	result := enforcement.Preflight(r)
	if result.Accepted || len(result.Errors) != 1 || result.Errors[0].Dimension != enforcement.Network {
		t.Fatalf("legacy bool cannot prove network mediation: %+v", result)
	}
}
func TestInvalidDeclarationsFailClosed(t *testing.T) {
	for _, mutate := range []func(*enforcement.Request){
		func(r *enforcement.Request) {
			r.Permissions = map[enforcement.Dimension]enforcement.Permission{enforcement.Process: {State: "invented"}}
		},
		func(r *enforcement.Request) {
			r.Required = []enforcement.Dimension{enforcement.Network}
			r.Optional = []enforcement.Dimension{enforcement.Network}
		},
		func(r *enforcement.Request) {
			r.Provider = enforcement.Declaration{Kind: "execution", Controls: map[enforcement.Dimension]enforcement.Capability{enforcement.Network: {Support: "supported", Verified: true, Update: "hot-reload"}}}
		},
	} {
		r := request()
		mutate(&r)
		result := enforcement.Preflight(r)
		if result.Accepted || len(result.Errors) == 0 {
			t.Fatalf("invalid input accepted: %+v", r)
		}
	}
}
func TestLegacyMandatoryNetworkCannotBecomeOptionalDimension(t *testing.T) {
	r := request()
	r.Policy.Network = &policy.NetworkRules{AllowedDomains: []string{"example.com"}}
	r.Optional = []enforcement.Dimension{enforcement.Network}
	r.Provider = enforcement.Declaration{Kind: "execution", Governance: map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlNetworkRestriction: true}}
	if result := enforcement.Preflight(r); result.Accepted {
		t.Fatal("optional dimension downgraded mandatory policy network protection")
	}
}
func TestNoopDeclarationHasNoExecutionProtection(t *testing.T) {
	controls, err := enforcement.DiscoverCapabilities(enforcement.NoopDeclaration())
	if err != nil || controls[enforcement.Tool].Support != "unsupported" || controls[enforcement.Credential].Verified {
		t.Fatalf("noop: %+v %v", controls, err)
	}
}
func TestUnknownDimensionValidationIsDeterministic(t *testing.T) {
	r := request()
	r.Permissions = map[enforcement.Dimension]enforcement.Permission{"zzz": {}, "aaa": {}}
	result := enforcement.Preflight(r)
	if len(result.Errors) != 1 || result.Errors[0].Message != `unknown permission dimension "aaa"` || result.Errors[0].Dimension != "aaa" {
		t.Fatalf("stable invalid dimension: %+v", result.Errors)
	}
}
func TestProviderContributionsAndConflictsRemainAttributed(t *testing.T) {
	r := request()
	r.Contributions = []enforcement.Contribution{
		{Provider: "z-provider", Permissions: map[enforcement.Dimension]enforcement.Permission{enforcement.Tool: {State: "known", Allow: []string{"send"}}}},
		{Provider: "a-provider", Permissions: map[enforcement.Dimension]enforcement.Permission{enforcement.Filesystem: {State: "known", Allow: []string{"/workspace/report"}}, enforcement.Credential: {State: "unknown", Allow: []string{"ref:report/read"}}}},
	}
	before, _ := json.Marshal(r)
	first, err := enforcement.ResolvePermissions(r)
	if err != nil {
		t.Fatal(err)
	}
	if first[enforcement.Tool].State != "unknown" || first[enforcement.Tool].Policy.Deny[0] != "send" || first[enforcement.Tool].Contributions[1].Provider != "z-provider" || first[enforcement.Tool].Contributions[1].Permission.Allow[0] != "send" || first[enforcement.Filesystem].Contributions[0].Permission.Allow[0] != "/workspace/report" || first[enforcement.Credential].Contributions[0].Permission.State != "unknown" {
		t.Fatalf("source attribution lost: %+v", first)
	}
	after, _ := json.Marshal(r)
	if string(before) != string(after) {
		t.Fatal("inspection mutated input")
	}
	r.Contributions[0], r.Contributions[1] = r.Contributions[1], r.Contributions[0]
	second, err := enforcement.ResolvePermissions(r)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("provider ordering changed effective view")
	}
}
func TestStrictRequestParsing(t *testing.T) {
	for _, data := range []string{`{"unknown":true}`, `{} {}`, `{"version":7}`} {
		if _, err := enforcement.LoadRequest([]byte(data)); err == nil {
			t.Fatalf("invalid JSON accepted: %s", data)
		}
	}
}
