package memoryintegration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sync"
	"testing"
	"time"
)

func TestReadOnlyMCPExposesBoundedSharedRecordAndTruthfulStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	p := memory.NewInMemoryProvider()
	record, err := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: memory.TypeConstraint, Content: "Never execute skills", Source: "ADR0006", Evidence: []string{"issue23"}})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	server, err := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "pi", Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "not-runtime-proof"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if !names["memory_get"] || !names["memory_search"] || !names["memory_status"] || names["memory_confirm_weak"] || names["memory_preview_weak"] {
		t.Fatalf("read-only tool registry: %v", names)
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": owner, "id": record.ID}})
	if err != nil || result.IsError {
		t.Fatalf("shared read: %+v %v", result, err)
	}
	var got memory.Record
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got)
	if got.ID != record.ID || got.Content != "Never execute skills" || got.Source != "ADR0006" || len(got.Evidence) != 1 {
		t.Fatalf("canonical read: %+v", got)
	}
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatal(err)
	}
	var status struct {
		Write        bool   `json:"write"`
		Mechanism    string `json:"mechanism"`
		LiveIdentity string `json:"liveIdentity"`
	}
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &status)
	if status.Write || status.Mechanism != "stdio-mcp" || status.LiveIdentity != "unverified-client-claims" {
		t.Fatalf("truthful status: %+v", status)
	}
}

func TestMCPToolSchemasEncodeEmptyRequiredAsArray(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	g, err := memory.NewGateway(memory.NewInMemoryProvider(), memory.Access{
		ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}, AllowWeakWrites: true,
	}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "pi", Owner: owner, AllowWeakWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "pi-mcp-adapter", Version: "5.3.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	listed, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) < 4 {
		t.Fatalf("expected Memory tools with weak writes, got %d", len(listed.Tools))
	}
	statusFound := false
	for _, tool := range listed.Tools {
		encoded, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(wire["inputSchema"], &schema); err != nil {
			t.Fatalf("%s input schema: %v", tool.Name, err)
		}
		required, ok := schema["required"]
		if !ok || string(required) == "null" {
			t.Fatalf("%s required must be an array, got %s", tool.Name, required)
		}
		var fields []string
		if err := json.Unmarshal(required, &fields); err != nil {
			t.Fatalf("%s required must be an array: %s", tool.Name, required)
		}
		if tool.Name == "memory_status" && len(fields) != 0 {
			t.Fatalf("memory_status should require no arguments: %v", fields)
		}
		statusFound = statusFound || tool.Name == "memory_status"
	}
	if !statusFound {
		t.Fatal("memory_status tool missing")
	}
}

func TestMCPWriteRequiresExactPreviewOwnerAndSourceConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	p := memory.NewInMemoryProvider()
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}, AllowWeakWrites: true}, memory.RetrievalPolicy{})
	server, _ := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner, AllowWeakWrites: true})
	st, ct := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claimed-human", Version: "fiction"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	input := memory.NewRecord{Owner: owner, Type: memory.TypeConstraint, Content: "No skill execution", Source: "ADR0006", Evidence: []string{"issue23"}}
	preview, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_preview_weak", Arguments: map[string]any{"owner": owner, "operation": "add", "record": input}})
	if err != nil || preview.IsError {
		t.Fatalf("explicit write preview: %+v %v", preview, err)
	}
	var plan struct {
		IntentID string          `json:"intentId"`
		Plan     memory.WeakPlan `json:"plan"`
	}
	json.Unmarshal([]byte(preview.Content[0].(*mcp.TextContent).Text), &plan)
	if plan.IntentID == "" || plan.Plan.Semantics != memory.WeakWriteSemantics {
		t.Fatalf("hidden weak semantics: %+v", plan)
	}
	denied, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_confirm_weak", Arguments: map[string]any{"owner": owner, "intentId": plan.IntentID, "confirmed": true, "source": "forged"}})
	if err != nil || !denied.IsError {
		t.Fatalf("wrong source accepted: %+v %v", denied, err)
	}
	confirmed, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_confirm_weak", Arguments: map[string]any{"owner": owner, "intentId": plan.IntentID, "confirmed": true, "source": "ADR0006"}})
	if err != nil || confirmed.IsError {
		t.Fatalf("exact confirmation: %+v %v", confirmed, err)
	}
	var got memory.Record
	json.Unmarshal([]byte(confirmed.Content[0].(*mcp.TextContent).Text), &got)
	if got.Version != 1 || got.Owner != owner || got.Source != "ADR0006" {
		t.Fatalf("write attribution: %+v", got)
	}
	foreign := memory.Owner{Kind: memory.OwnerProject, ProjectID: "foreign-project"}
	denied, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": foreign, "id": got.ID}})
	if err != nil || !denied.IsError || denied.Content[0].(*mcp.TextContent).Text != "ownership-denied" {
		t.Fatalf("cross-project access accepted: %+v %v", denied, err)
	}
}

func TestMCPRequiresInitializedNotificationBeforeToolAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	g, _ := memory.NewGateway(memory.NewInMemoryProvider(), memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	server, _ := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	conn, err := ct.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	init, _ := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`))
	if err = conn.Write(ctx, init); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	conn.Write(ctx, list)
	for {
		message, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := jsonrpc.EncodeMessage(message)
		var response map[string]any
		json.Unmarshal(b, &response)
		if response["id"] == float64(2) {
			if response["error"] == nil {
				t.Fatalf("tools exposed before initialized notification: %s", b)
			}
			break
		}
	}
}

func TestSessionEventsUseTrustedAgentAndNeverMirrorCanonicalContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	p := memory.NewInMemoryProvider()
	r, _ := p.Remember(ctx, memory.NewRecord{Owner: owner, Type: memory.TypeConstraint, Content: "canonical-content-never-in-events", Source: "ADR0006"})
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	var mu sync.Mutex
	var events []memoryintegration.Event
	server, _ := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner, Observer: func(e memoryintegration.Event) { mu.Lock(); events = append(events, e); mu.Unlock() }})
	st, ct := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "pi-forged-human", Version: "999"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	got, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": owner, "id": r.ID}})
	if err != nil || got.IsError {
		t.Fatal(err)
	}
	mu.Lock()
	b, _ := json.Marshal(events)
	mu.Unlock()
	if !bytes.Contains(b, []byte(`"agent":"codex"`)) || bytes.Contains(b, []byte("pi-forged-human")) || bytes.Contains(b, []byte("canonical-content-never-in-events")) || !bytes.Contains(b, []byte(`"tool":"memory_get"`)) {
		t.Fatalf("unsafe or missing observation: %s", b)
	}
}

func TestMCPStatusDoesNotInventWriteAuthorityFromSessionLabels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	g, _ := memory.NewGateway(memory.NewInMemoryProvider(), memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	server, _ := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner, AllowWeakWrites: true})
	st, ct := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	response, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_status", Arguments: map[string]any{}})
	if err != nil || response.IsError {
		t.Fatal(err)
	}
	var status memoryintegration.MechanismStatus
	json.Unmarshal([]byte(response.Content[0].(*mcp.TextContent).Text), &status)
	if status.Write {
		t.Fatalf("session labels invented authority: %+v", status)
	}
}

func TestMCPStartupOwnerCannotBeBroadenedByGatewayOrClientClaims(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	foreign := memory.Owner{Kind: memory.OwnerProject, ProjectID: "other-project"}
	p := memory.NewInMemoryProvider()
	r, _ := p.Remember(ctx, memory.NewRecord{Owner: foreign, Type: memory.TypeConstraint, Content: "foreign", Source: "other"})
	g, _ := memory.NewGateway(p, memory.Access{ReadOwners: []memory.Owner{owner, foreign}}, memory.RetrievalPolicy{})
	server, _ := memoryintegration.NewServer(g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner})
	st, ct := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "trusted-human-owner", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	response, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": foreign, "id": r.ID}})
	if err != nil || !response.IsError || response.Content[0].(*mcp.TextContent).Text != "ownership-denied" {
		t.Fatalf("startup owner broadened: %+v %v", response, err)
	}
}

func TestPiMCPUsesTrustedAgentAttribution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	var mu sync.Mutex
	var events []memoryintegration.Event
	gateway, err := memory.NewGateway(memory.NewInMemoryProvider(), memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := memoryintegration.NewServer(
		gateway,
		memoryintegration.SessionConfig{Agent: "pi", Owner: owner, Observer: func(event memoryintegration.Event) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}},
	)
	if err != nil {
		t.Fatalf("Pi MCP session rejected: %v", err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "pi-mcp-adapter", Version: "5.3.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "memory_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("Pi MCP status: result=%+v err=%v", result, err)
	}
	var status memoryintegration.MechanismStatus
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &status); err != nil {
		t.Fatal(err)
	}
	if status.Agent != "pi" || status.LiveIdentity != "unverified-client-claims" {
		t.Fatalf("Pi capability status: %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 || events[len(events)-1].Agent != "pi" || events[len(events)-1].Tool != "memory_status" {
		t.Fatalf("Pi runtime event attribution: %+v", events)
	}
}
