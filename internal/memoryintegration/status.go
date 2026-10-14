package memoryintegration

import "github.com/AllenMuu/skill-manager/internal/memory"

// ConfiguredStatus describes implemented fixture-tested operations and exact
// startup authority. It never fabricates a native runtime/version from labels.
func ConfiguredStatus(g *memory.Gateway, c SessionConfig) MechanismStatus {
	support := g.IntegrationCapabilities(c.Owner)
	status := MechanismStatus{Agent: c.Agent, Mechanism: "stdio-mcp", Read: support.Read, Search: support.Search, Write: c.AllowWeakWrites && (support.WeakAdd || support.WeakReplace), WeakAdd: c.AllowWeakWrites && support.WeakAdd, WeakReplace: c.AllowWeakWrites && support.WeakReplace, RetainedHistory: support.RetainedHistory, FixtureEvidence: "public MCP protocol tests; not native runtime acceptance", LiveIdentity: "unverified-client-claims", SessionObserved: map[string]int{}}
	if status.Write {
		status.WeakSemantics = memory.WeakWriteSemantics
	}
	return status
}
