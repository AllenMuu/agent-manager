package enforcement

import (
	"context"
	"errors"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// Operation carries neutral, immutable run authority and exact correlation.
// Handles and generations are safe opaque identifiers, never credentials.
type Operation struct {
	TerminationReason string             `json:"termination_reason,omitempty"`
	ProviderID        string             `json:"provider_id"`
	RunID             string             `json:"run_id"`
	ID                string             `json:"operation_id"`
	Action            string             `json:"action"`
	Handle            string             `json:"handle,omitempty"`
	Generation        string             `json:"generation,omitempty"`
	Policy            policy.Snapshot    `json:"policy"`
	Identity          identity.Selection `json:"identity"`
}
type Acknowledgement struct {
	Operation Operation `json:"operation"`
	State     string    `json:"state"`
}

var ErrUnsupported = errors.New("lifecycle capability unsupported")
var ErrUnknown = errors.New("lifecycle outcome unknown; reconcile original operation or recover manually")
var ErrRefused = errors.New("lifecycle operation refused")

// Provider declarations describe this selected provider, rather than caller
// assertions. Implementations own real execution interception; the offline mock
// advertises fixture support only and does not establish live protection.
type Provider interface {
	ID() string
	Declaration() Declaration
	Prepare(context.Context, Operation) (Acknowledgement, error)
	Start(context.Context, Operation) (Acknowledgement, error)
	Terminate(context.Context, Operation) (Acknowledgement, error)
}

// Querier must query the original operation without mutating or relaunching.
type Querier interface {
	Query(context.Context, Operation) (Acknowledgement, error)
}
type PauseResumer interface {
	Pause(context.Context, Operation) (Acknowledgement, error)
	Resume(context.Context, Operation) (Acknowledgement, error)
}
