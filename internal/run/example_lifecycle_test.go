package run_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

// This executable public-boundary example is offline fixture evidence.
func ExampleCoordinator() {
	root, err := os.MkdirTemp("", "agent-manager-lifecycle-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	store, err := run.NewStore(root)
	if err != nil {
		panic(err)
	}
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: example\nname: Example\ntools:\n  allow: [read]\n"))
	if err != nil {
		panic(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		panic(err)
	}
	provider := enforcement.NewMockProvider("offline")
	coordinator, err := run.NewCoordinator(store, provider)
	if err != nil {
		panic(err)
	}
	prepared, err := coordinator.Prepare(context.Background(), snapshot, root, identity.AnonymousSelection(), enforcement.Request{Version: "v1"})
	if err != nil {
		panic(err)
	}
	fmt.Println(prepared.Status, prepared.ExecutionReady())
	started, err := coordinator.Start(context.Background(), prepared.ID)
	if err != nil {
		panic(err)
	}
	fmt.Println(started.Status, started.ExecutionReady())
	provider.FailNext("terminate", enforcement.ErrUnknown, true)
	if _, err = coordinator.Terminate(context.Background(), started.ID); err == nil {
		panic("unknown termination succeeded")
	}
	reopened, err := run.NewStore(root)
	if err != nil {
		panic(err)
	}
	restarted, err := run.NewCoordinator(reopened, provider)
	if err != nil {
		panic(err)
	}
	recovered, err := restarted.Reconcile(context.Background(), started.ID)
	if err != nil {
		panic(err)
	}
	fmt.Println(recovered.Status, recovered.ExternalRuntime.Operations[2].Reconciled)
	fmt.Println("offline resources:", provider.ResourceCount())
	// Output:
	// prepared false
	// active true
	// terminated true
	// offline resources: 1
}
