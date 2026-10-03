package mem0_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/mem0"
)

func TestCanonicalCreateGet(t *testing.T) {
	var stored map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/memories":
			var req struct {
				User     string         `json:"user_id"`
				Infer    bool           `json:"infer"`
				Metadata map[string]any `json:"metadata"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Infer || req.User == "" || len(req.Messages) != 1 {
				t.Error("create must be scoped, literal and singular")
			}
			stored = map[string]any{"id": "backend-only", "memory": req.Messages[0].Content, "metadata": req.Metadata, "user_id": req.User}
			json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "backend-only", "memory": req.Messages[0].Content, "event": "ADD"}}})
		case r.Method == "POST" && r.URL.Path == "/search":
			var req struct {
				Filters map[string]string `json:"filters"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Filters["user_id"] != stored["user_id"] || req.Filters["am_record_id"] == "" {
				t.Error("missing exact owner/neutral-ID filters")
			}
			json.NewEncoder(w).Encode(map[string]any{"results": []any{stored}})
		case r.Method == "GET" && r.URL.Path == "/memories/backend-only":
			json.NewEncoder(w).Encode(stored)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p, err := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := memory.NewRecord{Owner: memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-1"}, Type: memory.TypeDecision, Content: "Use Go tests", Source: "design.md", Evidence: []string{"issue22"}, Layer: memory.LayerAtomic}
	created, err := p.Remember(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "backend-only" || created.ID == "" {
		t.Fatal("backend ID escaped canonical boundary")
	}
	got, err := p.Get(context.Background(), input.Owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created, got) || got.Version != 1 || got.Source != input.Source || !reflect.DeepEqual(got.Evidence, input.Evidence) {
		t.Fatalf("canonical data lost: %+v", got)
	}
}

// Fixture follows server/main.py at the pinned commit; it is entirely local.
func fixture(t *testing.T) *httptest.Server {
	t.Helper()
	var record map[string]any
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if r.Method != "GET" {
			json.NewDecoder(r.Body).Decode(&req)
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/memories":
			messages := req["messages"].([]any)
			text := messages[0].(map[string]any)["content"]
			record = map[string]any{"id": "backend", "memory": text, "metadata": req["metadata"], "user_id": req["user_id"], "score": 0.7}
			json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "backend", "event": "ADD"}}})
		case r.Method == "POST" && r.URL.Path == "/search":
			filters := req["filters"].(map[string]any)
			matches := []any{}
			if record != nil && filters["user_id"] == record["user_id"] && (filters["am_record_id"] == nil || filters["am_record_id"] == record["metadata"].(map[string]any)["am_record_id"]) {
				matches = append(matches, record)
			}
			json.NewEncoder(w).Encode(map[string]any{"results": matches})
		case r.Method == "GET" && r.URL.Path == "/memories/backend":
			json.NewEncoder(w).Encode(record)
		case r.Method == "PUT" && r.URL.Path == "/memories/backend":
			record["memory"] = req["text"]
			record["metadata"] = req["metadata"]
			json.NewEncoder(w).Encode(map[string]any{"message": "Memory updated successfully!"})
		case r.Method == "DELETE" && r.URL.Path == "/memories/backend":
			record = nil
			json.NewEncoder(w).Encode(map[string]any{"message": "Memory deleted successfully"})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}
func inputRecord() memory.NewRecord {
	return memory.NewRecord{Owner: memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-A"}, Type: memory.TypeDecision, Content: "Use standard Go tests", Source: "design.md", Evidence: []string{"issue22"}, Layer: memory.LayerAtomic}
}
func TestRemoteCanonicalRoundTrip(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, err := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	input := inputRecord()
	r, err := p.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := p.Recall(ctx, memory.Query{Owner: input.Owner, Text: "Go tests"})
	if err != nil || len(matches) != 1 || !reflect.DeepEqual(matches[0], r) {
		t.Fatalf("recall: %+v %v", matches, err)
	}
	input.Content = "Run go test ./..."
	updated, err := memory.Replace(ctx, p, memory.ReplaceRequest{Owner: input.Owner, ID: r.ID, Record: input})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != r.ID || updated.Version != 2 || updated.Source != r.Source || !reflect.DeepEqual(updated.Evidence, r.Evidence) {
		t.Fatalf("replace: %+v", updated)
	}
	got, err := p.Get(ctx, input.Owner, r.ID)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("get after replace: %+v %v", got, err)
	}
	if err = memory.Remove(ctx, p, input.Owner, r.ID); err != nil {
		t.Fatal(err)
	}
	matches, err = p.Recall(ctx, memory.Query{Owner: input.Owner, Text: "Go"})
	if err != nil || len(matches) != 0 {
		t.Fatalf("forgotten in recall: %v %v", matches, err)
	}
}

func TestUnsupportedStrongOperationsBeforeAnyRequest(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count++; w.WriteHeader(500) }))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: time.Second}, nil)
	c := p.Capabilities()
	if c.AtomicSupersede || c.ConditionalUpdate || c.Forget || c.History || c.Update || c.Supersede {
		t.Fatal("strong semantics overclaimed")
	}
	ctx := context.Background()
	if _, err := memory.Supersede(ctx, p, memory.UpdateRequest{}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := memory.Update(ctx, p, memory.UpdateRequest{}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := memory.Forget(ctx, p, memory.MutationRequest{}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d partial requests", count)
	}
}
func TestLostWriteIsUnknownAndNeverReplayed(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		time.Sleep(60 * time.Millisecond)
		w.WriteHeader(200)
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: 10 * time.Millisecond}, nil)
	_, err := p.Remember(context.Background(), inputRecord())
	if !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("expected uncertain outcome, got %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("%d writes", count.Load())
	}
}

type secretResolver struct {
	calls int
	value string
	err   error
}

func (s *secretResolver) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	s.calls++
	if ref.Kind != "env" || ref.Name != "MEM0_TEST_KEY" {
		return "", fmt.Errorf("wrong opaque reference")
	}
	return s.value, s.err
}
func TestAuthenticationFailureKeepsCredentialAtTransport(t *testing.T) {
	resolver := &secretResolver{value: "private-key-synthetic"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-API-Key") != resolver.value {
			t.Error("key not resolved at transport")
		}
		w.WriteHeader(401)
		w.Write([]byte(resolver.value))
	}))
	defer server.Close()
	ref := memory.ConfigReference{Kind: "env", Name: "MEM0_TEST_KEY"}
	p, err := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: time.Second, SecretReference: &ref}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 0 {
		t.Fatal("credential resolved during construction")
	}
	_, err = p.Remember(context.Background(), inputRecord())
	if !errors.Is(err, memory.ErrAuthentication) {
		t.Fatalf("expected authentication category: %v", err)
	}
	if memory.SafeError(err) != memory.ErrAuthentication {
		t.Fatal("safe diagnostics lost authentication category")
	}
	if requests != 1 || resolver.calls != 1 {
		t.Fatalf("requests/resolutions %d/%d", requests, resolver.calls)
	}
	if strings.Contains(fmt.Sprint(err), resolver.value) {
		t.Fatal("credential leaked")
	}
}

func TestExplicitOptInAndPinnedEndpoint(t *testing.T) {
	for _, c := range []mem0.Config{
		{Endpoint: "http://127.0.0.1:1", Contract: mem0.ContractVersion},
		{Endpoint: "http://127.0.0.1:1", AllowNetwork: true, Contract: "latest"},
		{Endpoint: "http://user:password@localhost:1", AllowNetwork: true, Contract: mem0.ContractVersion},
		{Endpoint: "http://external.example", AllowNetwork: true, Contract: mem0.ContractVersion},
		{Endpoint: "https://example.com?key=value", AllowNetwork: true, Contract: mem0.ContractVersion},
		{Endpoint: "https://example.com/v1", AllowNetwork: true, Contract: mem0.ContractVersion},
		{Endpoint: "https://example.com", AllowNetwork: true, Contract: mem0.ContractVersion, Timeout: -time.Second},
	} {
		if _, err := mem0.New(c, nil); err == nil {
			t.Fatalf("unsafe configuration accepted %+v", c)
		}
	}
}
func TestAuthenticatedRedirectIsNeverFollowed(t *testing.T) {
	var leaked atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.Write([]byte(`{"results":[]}`)) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	resolver := &secretResolver{value: "private-key-synthetic"}
	ref := memory.ConfigReference{Kind: "env", Name: "MEM0_TEST_KEY"}
	p, _ := mem0.New(mem0.Config{Endpoint: origin.URL, Contract: mem0.ContractVersion, AllowNetwork: true, SecretReference: &ref}, resolver)
	_, err := p.Remember(context.Background(), inputRecord())
	if err == nil || leaked.Load() != 0 {
		t.Fatalf("redirect forwarded: %d %v", leaked.Load(), err)
	}
}

func TestCancellationAndSafeHTTPErrorCategories(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{{401, memory.ErrAuthentication}, {403, memory.ErrAuthentication}, {409, memory.ErrConflict}, {404, memory.ErrNotFound}, {400, memory.ErrInvalidInput}, {503, memory.ErrUnavailable}}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte("sensitive-server-body"))
			}))
			defer server.Close()
			p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
			_, err := p.Get(context.Background(), inputRecord().Owner, "neutral")
			if !errors.Is(err, tc.want) || strings.Contains(fmt.Sprint(err), "sensitive") {
				t.Fatalf("status %d: %v", tc.status, err)
			}
		})
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write([]byte(`{"results":[]}`)) }))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Remember(ctx, inputRecord()); !errors.Is(err, memory.ErrCanceled) {
		t.Fatalf("pre-canceled write: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("request despite cancellation")
	}
}
func TestInvalidOwnerAndNeutralIDRejectedBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write([]byte(`{"results":[]}`)) }))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	for _, q := range []memory.Query{{Owner: memory.Owner{}}, {Owner: inputRecord().Owner, Text: string([]byte{0xff})}} {
		if _, err := p.Recall(context.Background(), q); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("invalid query: %v", err)
		}
	}
	if _, err := p.Get(context.Background(), inputRecord().Owner, ""); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("invalid ID: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("%d requests for invalid inputs", requests.Load())
	}
}

func TestScoredRecallUsesRemoteScore(t *testing.T) {
	server := fixture(t)
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	input := inputRecord()
	r, err := p.Remember(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := p.RecallScored(context.Background(), memory.Query{Owner: input.Owner, Text: "Go"})
	if err != nil || len(matches) != 1 || matches[0].Score != 0.7 || !reflect.DeepEqual(matches[0].Record, r) {
		t.Fatalf("semantic score lost: %v %v", matches, err)
	}
	if !p.Capabilities().ScoredRecall {
		t.Fatal("scored capability missing")
	}
}

func TestCreateDoesNotClaimProvenanceBackendDropped(t *testing.T) {
	var stored map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			meta := req["metadata"].(map[string]any)
			var envelope map[string]any
			json.Unmarshal([]byte(meta["am_record"].(string)), &envelope)
			delete(envelope, "source")
			b, _ := json.Marshal(envelope)
			meta["am_record"] = string(b)
			stored = map[string]any{"id": "backend", "memory": inputRecord().Content, "metadata": meta, "user_id": req["user_id"]}
			w.Write([]byte(`{"results":[{"id":"backend","event":"ADD"}]}`))
			return
		}
		json.NewEncoder(w).Encode(stored)
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	if _, err := p.Remember(context.Background(), inputRecord()); !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("dropped provenance claimed success: %v", err)
	}
}

func TestDeleteWithMalformedAcknowledgementIsUnknown(t *testing.T) {
	var stored map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/memories":
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			stored = map[string]any{"id": "backend", "memory": inputRecord().Content, "metadata": req["metadata"], "user_id": req["user_id"]}
			w.Write([]byte(`{"results":[{"id":"backend","event":"ADD"}]}`))
		case r.Method == "POST" && r.URL.Path == "/search":
			json.NewEncoder(w).Encode(map[string]any{"results": []any{stored}})
		case r.Method == "GET":
			json.NewEncoder(w).Encode(stored)
		case r.Method == "DELETE":
			w.Write([]byte(`{"detail":"misleading 200"}`))
		}
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	r, err := p.Remember(context.Background(), inputRecord())
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Remove(context.Background(), r.Owner, r.ID); !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("malformed acknowledgement claimed delete: %v", err)
	}
}

func TestMissingResultsIsNotSuccessfulEmptyRecall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"detail":"invalid contract"}`)) }))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true}, nil)
	if _, err := p.Recall(context.Background(), memory.Query{Owner: inputRecord().Owner, Text: "Go"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("invalid response treated as empty success: %v", err)
	}
}

type deadlineResolver struct{}

func (deadlineResolver) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func TestSecretResolutionReceivesBoundedContext(t *testing.T) {
	ref := memory.ConfigReference{Kind: "env", Name: "MEM0_TEST_KEY"}
	p, _ := mem0.New(mem0.Config{Endpoint: "http://127.0.0.1:1", Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: 5 * time.Millisecond, SecretReference: &ref}, deadlineResolver{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := p.Remember(ctx, inputRecord())
	if !errors.Is(err, memory.ErrCanceled) || time.Since(start) > 80*time.Millisecond {
		t.Fatalf("secret resolution unbounded/misclassified: %v (%v)", err, time.Since(start))
	}
}

func TestReadBodyCancellationIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	p, _ := mem0.New(mem0.Config{Endpoint: server.URL, Contract: mem0.ContractVersion, AllowNetwork: true, Timeout: 10 * time.Millisecond}, nil)
	_, err := p.Get(context.Background(), inputRecord().Owner, "neutral")
	if !errors.Is(err, memory.ErrCanceled) {
		t.Fatalf("read body cancellation: %v", err)
	}
}
