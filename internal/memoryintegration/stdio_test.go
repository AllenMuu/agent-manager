package memoryintegration_test

import (
	"context"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStdioRejectsOversizedFrameBeforeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "shared-project"}
	g, _ := memory.NewGateway(memory.NewInMemoryProvider(), memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan error, 1)
	go func() {
		done <- memoryintegration.Serve(ctx, g, memoryintegration.SessionConfig{Agent: "codex", Owner: owner}, server, server)
	}()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}` + "\n"
	if _, err := client.Write([]byte(initialize)); err != nil {
		t.Fatal(err)
	}
	type decoded struct {
		value map[string]any
		err   error
	}
	replies := make(chan decoded, 8)
	go func() {
		d := json.NewDecoder(client)
		for {
			var value map[string]any
			err := d.Decode(&value)
			replies <- decoded{value, err}
			if err != nil {
				return
			}
		}
	}()
	first := <-replies
	if first.err != nil || first.value["result"] == nil {
		t.Fatalf("negotiation failed: %v %v", first.value, first.err)
	}
	client.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	input := `{"jsonrpc":"2.0","id":2,"method":"ping","params":{"padding":"` + strings.Repeat("x", 1024*1024) + `"}}` + "\n"
	_, _ = client.Write([]byte(input))
	for {
		reply := <-replies
		if reply.err != nil {
			break
		}
		if reply.value["id"] == float64(2) {
			t.Fatalf("oversized MCP message dispatched: %v", reply.value)
		}
	}
}
