// Package operation contains preview and reversible-operation primitives.
package operation

import "strings"

// Change is one filesystem change shown before an operation is confirmed.
type Change struct {
	Path   string
	Action string
	Detail string
}

// Plan is a preview of a guarded operation.
type Plan struct {
	// Version identifies the operation journal schema used by this plan.
	// Empty values are normalized to the current schema when recorded.
	Version string
	// ResourceKind identifies the managed resource affected by this plan.
	// Empty values are normalized to Skill for legacy callers.
	ResourceKind string
	Operation    string
	Changes      []Change
	Warnings     []string
}

// NewPlan creates a plan for the current Skill operation schema. It keeps
// existing Skill workflows explicit while allowing future resource kinds to
// construct plans with their own metadata.
func NewPlan(operation string) Plan {
	return Plan{Version: "v1", ResourceKind: "skill", Operation: operation}
}

// String renders a compact, human-readable preview.
func (p Plan) String() string {
	var b strings.Builder
	if p.Operation != "" {
		b.WriteString(p.Operation)
		b.WriteByte('\n')
	}
	for _, c := range p.Changes {
		b.WriteString(c.Action + ": " + c.Path)
		if c.Detail != "" {
			b.WriteString(" (" + c.Detail + ")")
		}
		b.WriteByte('\n')
	}
	for _, warning := range p.Warnings {
		b.WriteString("warning: " + warning + "\n")
	}
	return b.String()
}
