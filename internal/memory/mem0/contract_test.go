package mem0_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
)

func TestPinnedOfflineContractFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Contract string          `json:"contract"`
		Empty    json.RawMessage `json:"emptySearch"`
	}
	if json.Unmarshal(b, &fixture) != nil || fixture.Contract != mem0.ContractVersion {
		t.Fatal("fixture pin mismatch")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/search" {
			t.Error("wrong OSS route")
		}
		w.Write(fixture.Empty)
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: fixture.Contract, AllowNetwork: true}, nil)
	if health, err := p.Health(context.Background()); err != nil || !health.Available {
		t.Fatalf("offline pinned probe: %v %v", health, err)
	}
}

// Independent fixture generation uses the documented wire envelope, not adapter helpers.
func wireRecord() map[string]any {
	r := memory.Record{ID: "neutral-fixture", Version: 18446744073709551615, Owner: inputRecord().Owner, Type: memory.TypeDecision, Layer: memory.LayerAtomic, State: memory.RecordActive, Source: "source", Evidence: []string{"proof"}}
	b, _ := json.Marshal(r)
	owner, _ := json.Marshal(r.Owner)
	partition := "am:" + base64.RawURLEncoding.EncodeToString(owner)
	return map[string]any{"id": "backend-fixture", "memory": "Fixture content", "user_id": partition, "score": 0.7, "metadata": map[string]any{"am_record_id": "neutral-fixture", "am_owner_partition": partition, "am_record": string(b)}}
}
func TestRemoteEnvelopeValidationAndLosslessVersions(t *testing.T) {
	cases := []struct {
		name  string
		alter func(map[string]any)
		want  error
	}{
		{"uint64 lossless", func(map[string]any) {}, nil},
		{"foreign owner", func(r map[string]any) { r["user_id"] = "foreign" }, memory.ErrOwnershipDenied},
		{"wrong neutral ID", func(r map[string]any) { r["metadata"].(map[string]any)["am_record_id"] = "wrong" }, memory.ErrInvalidInput},
		{"unknown type", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "DECISION", "UNKNOWN", 1)
		}, memory.ErrInvalidInput},
		{"unknown state", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "ACTIVE", "UNKNOWN", 1)
		}, memory.ErrInvalidInput},
		{"unknown layer", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "ATOMIC", "UNKNOWN", 1)
		}, memory.ErrInvalidInput},
		{"overflow version", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "18446744073709551615", "18446744073709551616", 1)
		}, memory.ErrInvalidInput},
		{"fractional version", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "18446744073709551615", "1.5", 1)
		}, memory.ErrInvalidInput},
		{"negative version", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "18446744073709551615", "-1", 1)
		}, memory.ErrInvalidInput},
		{"unpaired surrogate envelope", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), "source", `\ud800`, 1)
		}, memory.ErrInvalidInput},
		{"content mirror", func(r map[string]any) {
			m := r["metadata"].(map[string]any)
			m["am_record"] = strings.Replace(m["am_record"].(string), `"content":""`, `"content":"second copy"`, 1)
		}, memory.ErrInvalidInput},
		{"missing semantic score", func(r map[string]any) { delete(r, "score") }, memory.ErrInvalidInput},
		{"unnormalized semantic score", func(r map[string]any) { r["score"] = 2.0 }, memory.ErrInvalidInput},
		{"invalid backend path", func(r map[string]any) { r["id"] = "../foreign" }, memory.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := wireRecord()
			tc.alter(rr)
			data, _ := json.Marshal(map[string]any{"results": []any{rr}})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
			defer server.Close()
			p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
			matches, err := p.RecallScored(context.Background(), memory.Query{Owner: inputRecord().Owner, Text: "fixture"})
			if tc.want != nil {
				if !errors.Is(err, tc.want) || len(matches) != 0 {
					t.Fatalf("corrupt output escaped: %v %v", matches, err)
				}
				return
			}
			if err != nil || len(matches) != 1 || matches[0].Record.Version != 18446744073709551615 {
				t.Fatalf("lossless counter: %v %v", matches, err)
			}
		})
	}
}
func TestBoundedAndCorruptResponsesNeverExposeRawDiagnostics(t *testing.T) {
	for name, data := range map[string][]byte{"oversized": []byte(strings.Repeat("private-server-body", 100000)), "invalid UTF8": []byte{'"', 0xff, '"'}, "surrogate": []byte(`{"results":[],"private":"\ud800"}`), "null results": []byte(`{"results":null}`)} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
			defer server.Close()
			p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
			_, err := p.Recall(context.Background(), memory.Query{Owner: inputRecord().Owner, Text: "fixture"})
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe response accepted: %v", err)
			}
		})
	}
}
func TestAmbiguousNeutralLookupFailsBeforeLifecycleMutation(t *testing.T) {
	mutations := 0
	rr := wireRecord()
	data, _ := json.Marshal(map[string]any{"results": []any{rr, rr}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/search" {
			mutations++
		}
		w.Write(data)
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	if err := p.Remove(context.Background(), inputRecord().Owner, "neutral-fixture"); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("ambiguous lookup: %v", err)
	}
	if mutations != 0 {
		t.Fatal("partial lifecycle mutation")
	}
}

func TestHTTPServiceFailureCannotMasqueradeAsNotFound(t *testing.T) {
	rr := wireRecord()
	list, _ := json.Marshal(map[string]any{"results": []any{rr}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Write(list)
			return
		}
		w.WriteHeader(503)
		w.Write([]byte("private backend error"))
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	got, err := p.Get(context.Background(), inputRecord().Owner, "neutral-fixture")
	if !errors.Is(err, memory.ErrUnavailable) || errors.Is(err, memory.ErrNotFound) || got.ID != "" {
		t.Fatalf("503 misclassified: %v %v", got, err)
	}
}
func TestOwnerPartitionsRemainDistinct(t *testing.T) {
	owner := inputRecord().Owner
	for _, foreign := range []memory.Owner{
		{Kind: memory.OwnerProject, ProjectID: "project-B"},
		{Kind: memory.OwnerUser, UserID: owner.ProjectID},
		{Kind: memory.OwnerAgent, ProjectID: owner.ProjectID, AgentID: "agent-A"},
		{Kind: memory.OwnerSession, ProjectID: owner.ProjectID, SessionID: "session-A"},
	} {
		server := fixture(t)
		p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
		if _, err := p.Remember(context.Background(), inputRecord()); err != nil {
			t.Fatal(err)
		}
		records, err := p.Recall(context.Background(), memory.Query{Owner: foreign, Text: "Go"})
		if err != nil || len(records) != 0 {
			t.Fatalf("cross-partition result: %v %v", records, err)
		}
		server.Close()
	}
}
