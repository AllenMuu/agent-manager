// Package resource defines agent-neutral managed resource contracts.
package resource

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnsupportedCapabilities identifies a target that cannot represent a
// resource's declared requirements.
var ErrUnsupportedCapabilities = errors.New("resource capabilities are unsupported")

// Kind identifies a managed resource domain.
type Kind string

const (
	Skill    Kind = "skill"
	SubAgent Kind = "subagent"
	Memory   Kind = "memory"
)

// Capability is a behavior an adapter can represent for a resource.
type Capability string

const (
	CapabilityFilesystemRead  Capability = "filesystem-read"
	CapabilityFilesystemWrite Capability = "filesystem-write"
	CapabilityMemoryRead      Capability = "memory-read"
	CapabilityMemoryWrite     Capability = "memory-write"
	CapabilityMemorySearch    Capability = "memory-search"
)

// Provenance records the origin of a managed resource.
type Provenance struct {
	Source string `json:"source" yaml:"source"`
}

// Compatibility names target agents for which resource content is intended.
type Compatibility struct {
	Agents []string `json:"agents,omitempty" yaml:"agents,omitempty"`
}

// ManagedResource is the stable envelope shared by all resource handlers.
type ManagedResource struct {
	Version              string        `json:"version" yaml:"version"`
	ID                   string        `json:"id" yaml:"id"`
	Kind                 Kind          `json:"kind" yaml:"kind"`
	Provenance           Provenance    `json:"provenance" yaml:"provenance"`
	Compatibility        Compatibility `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`
	RequiredCapabilities []Capability  `json:"requiredCapabilities,omitempty" yaml:"requiredCapabilities,omitempty"`
}

// PlacementPlan is an agent-neutral operation description. Adapters may
// translate it into a runtime-specific destination and apply it through an
// explicitly guarded filesystem integration.
type PlacementPlan struct {
	Resource ManagedResource
	// Project identifies the selected project root whose adapter-owned
	// location may be mutated. Adapters use it to reject destinations supplied
	// by an untrusted caller.
	Project      string
	Capabilities []Capability
	Missing      []Capability
	Warnings     []string
	// These fields are populated only when a caller explicitly opts into
	// guarded local filesystem placement. They remain opaque here so the
	// resource package does not depend on delivery-layer operation types.
	Destination string
	Conflict    string
	Force       bool
	Journal     any
	Confirm     any
}

// ResourceHandler owns canonical catalog, inspection, and validation rules for
// one managed resource kind. It is deliberately free of agent locations:
// adapters translate a valid resource into a runtime-specific placement.
type ResourceHandler interface {
	Kind() Kind
	Validate(ManagedResource) error
	Catalog(string) ([]ManagedResource, []Diagnostic, error)
	Inspect(string) (ManagedResource, error)
	PlanLifecycle(LifecycleRequest) (LifecyclePlan, error)
}

// Handler is retained as a source-compatible name while callers migrate to
// ResourceHandler.
type Handler = ResourceHandler

// LifecycleAction identifies a resource-domain lifecycle decision. Handlers
// may expose action-specific request and plan types; the shared contract does
// not assume that every resource is represented by a filesystem link.
type LifecycleAction string

const (
	LifecycleActivate  LifecycleAction = "activate"
	LifecycleRemove    LifecycleAction = "remove"
	LifecycleAdopt     LifecycleAction = "adopt"
	LifecycleFork      LifecycleAction = "fork"
	LifecycleReconcile LifecycleAction = "reconcile"
)

// LifecycleRequest is implemented by resource-specific planning requests.
type LifecycleRequest interface {
	ActionKind() LifecycleAction
}

// LifecyclePlan is implemented by resource-specific lifecycle plans. Delivery
// services coordinate confirmation and mutation after composing a handler plan
// with an adapter-owned runtime placement.
type LifecyclePlan interface {
	ActionKind() LifecycleAction
}

// LifecycleChange is one handler-planned change for a delivery preview.
type LifecycleChange struct {
	Path   string
	Action string
	Detail string
}

// SkillConflictStrategy selects a supported filesystem-Skill conflict policy.
type SkillConflictStrategy string

const SkillConflictReplace SkillConflictStrategy = "replace"

var (
	ErrUnsafeLifecycle   = errors.New("refusing unmanaged or unexpected path")
	ErrForceRequired     = errors.New("conflict strategy requires force confirmation")
	ErrLifecycleConflict = errors.New("operation conflicts with existing skill")
)

// SkillLifecycleRequest contains the filesystem state a SkillHandler needs to
// validate and plan. PlacementPath is supplied by an AgentAdapter; the handler
// never derives a runtime location.
type SkillLifecycleRequest struct {
	Action      LifecycleAction
	LibraryPath string
	// ProjectPath is the selected project root for adapter-owned placement.
	// It is kept at the lifecycle boundary so mutation cannot trust an
	// arbitrary PlacementPath supplied by a downstream plan.
	ProjectPath   string
	Resource      ManagedResource
	CatalogEntry  *SkillCatalogEntry
	Identifier    string
	PlacementPath string
	Target        string
	Conflict      SkillConflictStrategy
	Force         bool
	JournalSource string
	JournalOwned  bool
}

func (r SkillLifecycleRequest) ActionKind() LifecycleAction { return r.Action }

// SkillLifecyclePlan is the handler-owned validation and planning result for a
// filesystem-backed Skill. Filesystem mutation remains the coordinator's job.
type SkillLifecyclePlan struct {
	Action          LifecycleAction
	Resource        ManagedResource
	SourcePath      string
	CurrentSource   string
	ReplaceExisting bool
	Applicable      bool
	Changes         []LifecycleChange
	Warnings        []string
}

func (p SkillLifecyclePlan) ActionKind() LifecycleAction { return p.Action }

// SkillHandler owns the existing directory-Skill catalog and lifecycle rules.
type SkillHandler struct{}

// NewSkillHandler constructs the handler for managed Skills.
func NewSkillHandler() SkillHandler { return SkillHandler{} }

func (SkillHandler) Kind() Kind { return Skill }

func (SkillHandler) Validate(managed ManagedResource) error {
	if managed.Kind != Skill {
		return fmt.Errorf("Skill handler does not support resource kind %q", managed.Kind)
	}
	return managed.Validate()
}

// Validate rejects unsafe or unsupported resource contract values.
func (r ManagedResource) Validate() error {
	if r.Version != "v1" {
		return fmt.Errorf("unsupported resource version %q", r.Version)
	}
	if r.ID == "" || r.ID == "." || r.ID == ".." || filepath.Base(r.ID) != r.ID || strings.ContainsAny(r.ID, `/\\`) {
		return fmt.Errorf("unsafe resource identifier %q", r.ID)
	}
	if !validKind(r.Kind) {
		return fmt.Errorf("unsupported resource kind %q", r.Kind)
	}
	for _, capability := range r.RequiredCapabilities {
		if !validCapability(capability) {
			return fmt.Errorf("unsupported required capability %q", capability)
		}
	}
	return nil
}

func validKind(kind Kind) bool {
	return kind == Skill || kind == SubAgent || kind == Memory
}

func validCapability(capability Capability) bool {
	switch capability {
	case CapabilityFilesystemRead, CapabilityFilesystemWrite, CapabilityMemoryRead, CapabilityMemoryWrite, CapabilityMemorySearch:
		return true
	default:
		return false
	}
}

// MissingCapabilities returns required capabilities absent from supported in
// the stable order requested by the resource contract.
func MissingCapabilities(required, supported []Capability) []Capability {
	present := make(map[Capability]struct{}, len(supported))
	for _, capability := range supported {
		present[capability] = struct{}{}
	}
	missing := make([]Capability, 0, len(required))
	for _, capability := range required {
		if _, ok := present[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	return missing
}
