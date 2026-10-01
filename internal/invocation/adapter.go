package invocation

import "context"

type Capabilities struct {
	PreservesContext bool
}

type Request struct {
	ActionID                  string
	Tool                      string
	RequireContextPropagation bool
}

type Result struct {
	Output   string
	Warnings []string
}

// InvocationAdapter is intentionally independent from filesystem resource
// placement adapters. Implementations must declare end-to-end context support.
type InvocationAdapter interface {
	Capabilities() Capabilities
	Invoke(context.Context, Request, InvocationContext) (Result, error)
}
