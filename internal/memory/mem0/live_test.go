package mem0_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
)

// The explicit harness reads a task-owned private service environment file only
// at the transport boundary. Ordinary tests skip this and never start a service.
type liveSecrets struct{ path string }

func (s liveSecrets) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	if ctx.Err() != nil {
		return "", memory.ErrCanceled
	}
	if ref.Kind != "file" || ref.Name != s.path {
		return "", memory.ErrAuthentication
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return "", memory.ErrAuthentication
	}
	var values map[string]string
	if json.Unmarshal(b, &values) != nil {
		return "", memory.ErrAuthentication
	}
	return values["ADMIN_API_KEY"], nil
}
func TestExplicitLiveSmoke(t *testing.T) {
	if os.Getenv("AM_MEM0_LIVE") != "1" {
		t.Skip("explicit real-service smoke not enabled")
	}
	proofPath := os.Getenv("AM_MEM0_LIVE_PROOF")
	evidencePath := os.Getenv("AM_MEM0_LIVE_EVIDENCE")
	secretPath := os.Getenv("AM_MEM0_LIVE_SECRET_FILE")
	if proofPath == "" || evidencePath == "" || secretPath == "" {
		t.Fatal("explicit version/environment proof, evidence path and opaque secret file are required")
	}
	b, err := os.ReadFile(proofPath)
	if err != nil {
		t.Fatal("version/environment proof unavailable")
	}
	var proof struct {
		Version     string `json:"version"`
		Commit      string `json:"commit"`
		Python      string `json:"python"`
		Environment string `json:"environment"`
	}
	if json.Unmarshal(b, &proof) != nil || proof.Version != "2.2.1" || proof.Commit != "94c3fe9f238f3dbf29c9ce98643bd71eb13077cd" || proof.Python == "" || proof.Environment == "" {
		t.Fatal("selected live runtime pin/environment is not verified")
	}
	ref := memory.ConfigReference{Kind: "file", Name: secretPath}
	p, err := mem0.New(mem0.Config{Endpoint: os.Getenv("AM_MEM0_LIVE_ENDPOINT"), Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: 30 * time.Second, SecretReference: &ref}, liveSecrets{path: secretPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	health, err := p.Health(ctx)
	if err != nil || !health.Available {
		t.Fatalf("live probe: %v", err)
	}
	input := inputRecord()
	input.Owner.ProjectID = "issue22-live-" + time.Now().UTC().Format("20060102T150405.000000000")
	input.Content = "Issue 22 acceptance: project uses Go standard tests."
	created, err := memory.Remember(ctx, p, input)
	if err != nil {
		t.Fatalf("live canonical create: %v", err)
	}
	removed := false
	defer func() {
		if !removed {
			if err := memory.Remove(context.Background(), p, input.Owner, created.ID); err != nil {
				t.Errorf("live cleanup: %v", err)
			}
		}
	}()
	got, err := memory.Get(ctx, p, input.Owner, created.ID)
	if err != nil || !reflect.DeepEqual(got, created) {
		t.Fatalf("live canonical get: %+v %v", got, err)
	}
	recalled, err := p.RecallScored(ctx, memory.Query{Owner: input.Owner, Text: "Go standard tests"})
	if err != nil || len(recalled) != 1 || !reflect.DeepEqual(recalled[0].Record, created) {
		t.Fatalf("live canonical recall: %+v %v", recalled, err)
	}
	foreign := input.Owner
	foreign.ProjectID += "-foreign"
	if records, err := p.Recall(ctx, memory.Query{Owner: foreign, Text: "Go"}); err != nil || len(records) != 0 {
		t.Fatalf("live owner isolation: %v %v", records, err)
	}
	input.Content = "Issue 22 acceptance: run go test ./... before delivery."
	updated, err := memory.Replace(ctx, p, memory.ReplaceRequest{Owner: input.Owner, ID: created.ID, Record: input})
	if err != nil || updated.ID != created.ID || updated.Version != 2 || updated.Source != created.Source || !reflect.DeepEqual(updated.Evidence, created.Evidence) {
		t.Fatalf("live canonical replace: %+v %v", updated, err)
	}
	got, err = p.Get(ctx, input.Owner, created.ID)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("live post-replace get: %+v %v", got, err)
	}
	if err = memory.Remove(ctx, p, input.Owner, created.ID); err != nil {
		t.Fatalf("live remove: %v", err)
	}
	removed = true
	if records, err := p.Recall(ctx, memory.Query{Owner: input.Owner, Text: "Go tests"}); err != nil || len(records) != 0 {
		t.Fatalf("live forgotten ordinary recall: %v %v", records, err)
	}
	evidence := map[string]any{"kind": "real-service-Go-adapter", "contract": mem0.ContractVersion, "runtimeProof": proof, "runtimeAutomaticallyObserved": "unknown", "created": created, "updated": updated, "remoteScore": recalled[0].Score, "removedFromRecall": true, "health": health, "capabilities": p.Capabilities(), "testedAt": time.Now().UTC()}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.WriteFile(evidencePath, data, 0600); err != nil {
		t.Fatal("cannot save live evidence")
	}
}
