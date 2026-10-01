package invocation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/AllenMuu/skill-manager/internal/identity"
)

var (
	actionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_.:/-]{0,127}$`)
	toolIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
)

type UnsupportedPropagationError struct{ ActionID string }

func (e *UnsupportedPropagationError) Error() string {
	return fmt.Sprintf("invocation adapter cannot preserve required context for action %q", e.ActionID)
}

type NotDispatchedError struct{ Cause error }

func (e *NotDispatchedError) Error() string {
	return fmt.Sprintf("invocation was not dispatched: %v", e.Cause)
}

func (e *NotDispatchedError) Unwrap() error { return e.Cause }

// PreparedDispatch contains a validated request and the adapter capabilities
// observed during preflight. Call Invoke only after any required authorization
// has been durably claimed.
type PreparedDispatch struct {
	adapter      InvocationAdapter
	request      Request
	callContext  InvocationContext
	capabilities Capabilities
}

func PrepareDispatch(adapter InvocationAdapter, request Request, expected Lineage, callContext InvocationContext) (PreparedDispatch, error) {
	if adapter == nil {
		return PreparedDispatch{}, errors.New("invocation adapter is required")
	}
	if !actionIDPattern.MatchString(request.ActionID) || !toolIDPattern.MatchString(request.Tool) || identity.IsCredentialLike(request.ActionID) || identity.IsCredentialLike(request.Tool) {
		return PreparedDispatch{}, errors.New("invocation request has an invalid action or tool identifier")
	}
	if err := callContext.ValidateFor(expected); err != nil {
		return PreparedDispatch{}, fmt.Errorf("validate invocation lineage: %w", err)
	}
	capabilities := adapter.Capabilities()
	if request.RequireContextPropagation && !capabilities.PreservesContext {
		return PreparedDispatch{}, &UnsupportedPropagationError{ActionID: request.ActionID}
	}
	return PreparedDispatch{adapter: adapter, request: request, callContext: callContext, capabilities: capabilities}, nil
}

func (prepared PreparedDispatch) Invoke(ctx context.Context) (Result, error) {
	if prepared.adapter == nil {
		return Result{}, errors.New("prepared invocation adapter is required")
	}
	if ctx == nil {
		return Result{}, errors.New("call context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, &NotDispatchedError{Cause: err}
	}
	result, err := prepared.adapter.Invoke(ctx, prepared.request, prepared.callContext)
	if err != nil {
		return Result{}, err
	}
	if !prepared.request.RequireContextPropagation && !prepared.capabilities.PreservesContext {
		result.Warnings = append(result.Warnings, "INVOCATION_CONTEXT_PROPAGATION_UNSUPPORTED")
	}
	return result, nil
}

func Dispatch(ctx context.Context, adapter InvocationAdapter, request Request, expected Lineage, callContext InvocationContext) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("call context is required")
	}
	prepared, err := PrepareDispatch(adapter, request, expected, callContext)
	if err != nil {
		return Result{}, err
	}
	return prepared.Invoke(ctx)
}

type InvocationCall struct {
	Request Request
	Context InvocationContext
}

// MockAdapter is a deterministic local adapter for validating the invocation
// contract. It never starts a runtime or contacts an external service.
type MockAdapter struct {
	PreservesContext bool
	Output           string
	mu               sync.Mutex
	calls            []InvocationCall
}

func NewMockAdapter(preservesContext bool, output string) *MockAdapter {
	return &MockAdapter{PreservesContext: preservesContext, Output: output}
}

func (m *MockAdapter) Capabilities() Capabilities {
	return Capabilities{PreservesContext: m.PreservesContext}
}

func (m *MockAdapter) Invoke(ctx context.Context, request Request, callContext InvocationContext) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("call context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	call := InvocationCall{Request: request}
	if m.PreservesContext {
		call.Context = callContext
	}
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	return Result{Output: m.Output}, nil
}

func (m *MockAdapter) Calls() []InvocationCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]InvocationCall(nil), m.calls...)
}
