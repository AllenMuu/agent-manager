package cli_test

import (
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflinePreflightCLIReportsMissingCredentialAndInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	r := enforcement.Request{Version: "v1", Policy: policy.AgentPolicy{Version: "v1", Kind: "agent-policy", ID: "offline", Name: "Offline"}, Required: []enforcement.Dimension{enforcement.Credential}, Provider: enforcement.Declaration{Kind: "noop"}}
	data, _ := json.Marshal(r)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeCLI("policies", "preflight", path, "--json")
	var result enforcement.Result
	if err == nil || json.Unmarshal([]byte(out), &result) != nil || result.Accepted || result.Errors[0].Dimension != enforcement.Credential {
		t.Fatalf("missing credential output=%s err=%v", out, err)
	}
	out, err = executeCLI("policies", "inspect", path, "--json")
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Permissions[enforcement.Filesystem].State != "unknown" {
		t.Fatalf("inspect output=%s err=%v", out, err)
	}
}
