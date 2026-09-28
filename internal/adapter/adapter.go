// Package adapter defines supported agent skill placement conventions.
package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

// Target identifies a supported coding agent.
type Target string

const (
	ClaudeCode Target = "claude-code"
	Codex      Target = "codex"
	Pi         Target = "pi"
)

// DetectionRequest selects the local locations an agent adapter may inspect.
// These paths live at the adapter boundary and never enter resource contracts.
type DetectionRequest struct {
	Home    string
	Project string
}

// Detection is a read-only snapshot of an adapter's local presence.
type Detection struct {
	Available  bool
	Configured bool
}

// RuntimeCapabilities describe permissions verified for agent execution. They
// are separate from resource placement capabilities. A directory adapter does
// not inspect a running agent's effective tool or sandbox configuration.
type RuntimeCapabilities struct {
	FilesystemRead     bool
	FilesystemWrite    bool
	Shell              bool
	Network            bool
	RestrictFilesystem bool
	RestrictShell      bool
	RestrictNetwork    bool
}

// RuntimeEvent is the adapter boundary record used to normalize a native
// runtime notification before it reaches the provider-neutral policy engine.
// Concrete directory adapters do not currently receive runtime notifications.
type RuntimeEvent struct {
	Category         policy.EventCategory
	Actor            string
	Tool             string
	Domain           string
	CredentialScope  string
	Resource         string
	ActionType       string
	Timestamp        time.Time
	RequestAuditID   string
	ObservedDecision policy.Outcome
	ReasonCode       policy.ReasonCode
}

// NormalizeGovernanceEvent converts an adapter's mapped notification into
// the canonical event contract. Runtime-specific parsing belongs before this
// boundary; the returned value contains no vendor event type.
func NormalizeGovernanceEvent(target Target, native RuntimeEvent) (policy.Event, error) {
	if target != ClaudeCode && target != Codex && target != Pi {
		return policy.Event{}, fmt.Errorf("unsupported runtime target %q", target)
	}
	event := policy.Event{
		Category: native.Category, Actor: native.Actor, Runtime: string(target),
		Tool: native.Tool, Domain: native.Domain, CredentialScope: native.CredentialScope,
		Resource: native.Resource, ActionType: native.ActionType,
		Timestamp: native.Timestamp.UTC(), RequestAuditID: native.RequestAuditID, ObservedDecision: native.ObservedDecision,
		ReasonCode: native.ReasonCode,
	}
	if err := event.Validate(); err != nil {
		return policy.Event{}, fmt.Errorf("normalize %s governance event: %w", target, err)
	}
	return event, nil
}

// InspectionRequest selects one resource kind in a project location.
type InspectionRequest struct {
	Project string
	Kind    resource.Kind
}

// Inspection identifies a runtime resource representation without interpreting
// its content.
type Inspection struct {
	Kind       resource.Kind
	Identifier string
	Path       string
}

// PlacementRequest asks an adapter to translate a managed resource to its
// runtime location. It is planning-only and does not mutate the filesystem.
type PlacementRequest struct {
	Project  string
	Resource resource.ManagedResource
}

// Placement is the adapter-owned result of a placement plan.
type Placement struct {
	Target      Target
	Kind        resource.Kind
	Identifier  string
	Destination string
}

// UnsupportedCapabilityError explains why a requested resource cannot be
// represented by an adapter. Callers can show it in a preview without ever
// receiving a filesystem placement to apply.
type UnsupportedCapabilityError struct {
	Target  Target
	Kind    resource.Kind
	Missing []resource.Capability
}

func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("%s does not support required %s capabilities: %s", e.Target, e.Kind, joinCapabilities(e.Missing))
}

// UnsupportedResourceKindError explains why an adapter cannot represent a
// requested resource domain. Keeping this distinct from capability errors
// prevents callers from treating an unsupported kind as an empty but valid
// placement and makes the no-write boundary explicit.
type UnsupportedResourceKindError struct {
	Target Target
	Kind   resource.Kind
}

func (e *UnsupportedResourceKindError) Error() string {
	return fmt.Sprintf("%s does not support resource kind %q", e.Target, e.Kind)
}

// AgentAdapter defines the runtime boundary for agent detection, inspection,
// resource validation, placement planning, and declared capabilities.
type AgentAdapter interface {
	Target() Target
	ProjectSkillPath(project, identifier string) string
	GlobalSkillPath(home, identifier string) string
	Detect(DetectionRequest) (Detection, error)
	Inspect(InspectionRequest) ([]Inspection, error)
	ValidateResource(resource.ManagedResource) error
	PlanPlacement(PlacementRequest) (Placement, error)
	Place(resource.PlacementPlan) error
	ResourceKinds() []resource.Kind
	Capabilities(resource.Kind) []resource.Capability
	Supports(resource.Kind) bool
	HasCapability(resource.Kind, resource.Capability) bool
	RuntimeCapabilities() RuntimeCapabilities
	GovernanceCapabilities() map[policy.Control]bool
	InspectSubAgent(subagent.Definition, SubAgentRequest) (SubAgentInspection, error)
	PlanSubAgent(subagent.Definition, SubAgentRequest) (SubAgentPlan, error)
}

// Adapter is retained as a source-compatible name for AgentAdapter.
type Adapter = AgentAdapter

type directoryAdapter struct {
	target    Target
	dir       string
	globalDir string
}

func (a directoryAdapter) Target() Target { return a.target }

func (a directoryAdapter) ProjectSkillPath(project, identifier string) string {
	return filepath.Join(project, a.dir, "skills", identifier)
}

func (a directoryAdapter) GlobalSkillPath(home, identifier string) string {
	dir := a.globalDir
	if dir == "" {
		dir = a.dir
	}
	return filepath.Join(home, dir, "skills", identifier)
}

func (a directoryAdapter) Detect(request DetectionRequest) (Detection, error) {
	type detectionRoot struct{ project, root string }
	roots := make([]detectionRoot, 0, 2)
	if request.Project != "" {
		roots = append(roots, detectionRoot{project: request.Project, root: filepath.Dir(a.ProjectSkillPath(request.Project, "placeholder"))})
	}
	if request.Home != "" {
		roots = append(roots, detectionRoot{project: request.Home, root: filepath.Dir(a.GlobalSkillPath(request.Home, "placeholder"))})
	}
	for _, candidate := range roots {
		root := candidate.root
		if root == "" {
			continue
		}
		if err := a.validateProjectParents(candidate.project, root); err != nil {
			return Detection{}, err
		}
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			return Detection{Available: true, Configured: true}, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return Detection{}, fmt.Errorf("inspect %s: %w", a.Target(), err)
		}
	}
	return Detection{}, nil
}

func (a directoryAdapter) Inspect(request InspectionRequest) ([]Inspection, error) {
	if request.Project == "" {
		return nil, fmt.Errorf("project is required for inspection")
	}
	if !a.Supports(request.Kind) {
		return nil, &UnsupportedResourceKindError{Target: a.Target(), Kind: request.Kind}
	}
	root := filepath.Dir(a.ProjectSkillPath(request.Project, "placeholder"))
	if err := a.validateProjectParents(request.Project, root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s resources: %w", a.Target(), err)
	}
	inspections := make([]Inspection, 0, len(entries))
	for _, entry := range entries {
		inspections = append(inspections, Inspection{
			Kind:       request.Kind,
			Identifier: entry.Name(),
			Path:       filepath.Join(root, entry.Name()),
		})
	}
	return inspections, nil
}

func (a directoryAdapter) ValidateResource(managed resource.ManagedResource) error {
	if err := managed.Validate(); err != nil {
		return err
	}
	if !a.Supports(managed.Kind) {
		return &UnsupportedResourceKindError{Target: a.Target(), Kind: managed.Kind}
	}
	if missing := resource.MissingCapabilities(managed.RequiredCapabilities, a.Capabilities(managed.Kind)); len(missing) > 0 {
		return &UnsupportedCapabilityError{Target: a.Target(), Kind: managed.Kind, Missing: missing}
	}
	return nil
}

func (a directoryAdapter) PlanPlacement(request PlacementRequest) (Placement, error) {
	if err := a.ValidateResource(request.Resource); err != nil {
		return Placement{}, err
	}
	if request.Project == "" {
		return Placement{}, fmt.Errorf("project is required for placement")
	}
	destination := a.projectResourcePath(request.Project, request.Resource.Kind, request.Resource.ID)
	if err := a.validateResourcePlacement(request.Project, destination, request.Resource.Kind, request.Resource.ID); err != nil {
		return Placement{}, err
	}
	return Placement{
		Target:      a.Target(),
		Kind:        request.Resource.Kind,
		Identifier:  request.Resource.ID,
		Destination: destination,
	}, nil
}

// Place applies a placement only through the explicitly configured guarded
// filesystem integration. A plan without that context cannot mutate.
func (a directoryAdapter) Place(plan resource.PlacementPlan) error {
	if err := a.ValidateResource(plan.Resource); err != nil {
		return err
	}
	if len(plan.Missing) > 0 {
		return fmt.Errorf("%w: missing capabilities %v", resource.ErrUnsupportedCapabilities, plan.Missing)
	}
	if plan.Project == "" || plan.Destination == "" {
		return ErrPlacementConfiguration
	}
	if err := a.validateResourcePlacement(plan.Project, plan.Destination, plan.Resource.Kind, plan.Resource.ID); err != nil {
		return err
	}
	journal, ok := plan.Journal.(*operation.Journal)
	if !ok || journal == nil {
		return ErrPlacementConfiguration
	}
	var confirm func(operation.Plan) bool
	if plan.Confirm != nil {
		var ok bool
		confirm, ok = plan.Confirm.(func(operation.Plan) bool)
		if !ok {
			return ErrPlacementConfiguration
		}
	}
	_, err := PlaceFilesystem(plan, FilesystemPlacementOptions{
		Project:     plan.Project,
		Target:      a.Target(),
		Destination: plan.Destination,
		Conflict:    ConflictStrategy(plan.Conflict),
		Force:       plan.Force,
		Journal:     journal,
		Confirm:     confirm,
	})
	return err
}

// ValidatePlacement verifies that destination is exactly the location this
// adapter derives for identifier in project. Existing parent components must
// be real directories; absent components are left for the guarded publisher
// to create within the already-existing project root.
func (a directoryAdapter) validatePlacement(project, destination, identifier string) error {
	return a.validateResourcePlacement(project, destination, resource.Skill, identifier)
}

func (a directoryAdapter) projectResourcePath(project string, kind resource.Kind, identifier string) string {
	if kind == resource.SubAgent {
		ext := ".md"
		if a.target == Codex {
			ext = ".toml"
		}
		return filepath.Join(project, a.dir, "agents", identifier+ext)
	}
	return a.ProjectSkillPath(project, identifier)
}

func (a directoryAdapter) validateResourcePlacement(project, destination string, kind resource.Kind, identifier string) error {
	if identifier == "" || ValidateIdentifier(identifier) != nil {
		return fmt.Errorf("%w: unsafe resource identifier %q", ErrUnsafePath, identifier)
	}
	if !a.Supports(kind) {
		return &UnsupportedResourceKindError{Target: a.target, Kind: kind}
	}
	expected := a.projectResourcePath(project, kind, identifier)
	actual, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("%w: resolve placement destination", ErrUnsafePath)
	}
	expected, err = filepath.Abs(expected)
	if err != nil || filepath.Clean(actual) != filepath.Clean(expected) {
		return fmt.Errorf("%w: destination is not the adapter-owned project placement", ErrUnsafePath)
	}
	return a.validateProjectParents(project, filepath.Dir(expected))
}

func (a directoryAdapter) validateProjectParents(project, destinationParent string) error {
	root, err := filepath.Abs(project)
	if err != nil {
		return fmt.Errorf("%w: resolve project root", ErrUnsafePath)
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		// A project may not exist yet. Verify the existing ancestor chain so
		// MkdirAll can only create the selected project and its descendants.
		for ancestor := filepath.Dir(root); ; ancestor = filepath.Dir(ancestor) {
			ancestorInfo, ancestorErr := os.Lstat(ancestor)
			if ancestorErr == nil {
				if ancestorInfo.Mode()&os.ModeSymlink != 0 || !ancestorInfo.IsDir() {
					return fmt.Errorf("%w: project ancestor %q must be a real directory", ErrUnsafePath, ancestor)
				}
				break
			}
			if !os.IsNotExist(ancestorErr) {
				return fmt.Errorf("%w: inspect project ancestor: %w", ErrUnsafePath, ancestorErr)
			}
			next := filepath.Dir(ancestor)
			if next == ancestor {
				break
			}
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: inspect project root: %w", ErrUnsafePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: project root must be a real directory", ErrUnsafePath)
	}
	parent, err := filepath.Abs(destinationParent)
	if err != nil {
		return fmt.Errorf("%w: resolve placement parent", ErrUnsafePath)
	}
	parent = filepath.Clean(parent)
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: placement parent escapes project root", ErrUnsafePath)
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			// Everything below an absent component will be created by MkdirAll;
			// no path outside root can be reached from this point.
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: inspect placement parent %q: %w", ErrUnsafePath, current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: placement parent %q must be a real directory", ErrUnsafePath, current)
		}
	}
	return nil
}

// ValidateProjectPlacement is the adapter boundary check used by lifecycle
// coordinators immediately before mutating a generated project placement.
func ValidateProjectPlacement(target Target, project, destination, identifier string) error {
	return ValidateProjectResourcePlacement(target, project, destination, resource.Skill, identifier)
}

// ValidateProjectResourcePlacement verifies that destination is exactly the
// adapter-owned placement for a resource kind. It is called immediately before
// guarded publication so a caller cannot redirect a plan to an arbitrary path.
func ValidateProjectResourcePlacement(target Target, project, destination string, kind resource.Kind, identifier string) error {
	a, ok := supported[target]
	if !ok {
		return fmt.Errorf("unsupported target %q", target)
	}
	directory, ok := a.(directoryAdapter)
	if !ok {
		return fmt.Errorf("unsupported placement adapter %q", target)
	}
	return directory.validateResourcePlacement(project, destination, kind, identifier)
}

func (a directoryAdapter) Supports(kind resource.Kind) bool {
	if kind == resource.Skill {
		return true
	}
	return kind == resource.SubAgent && a.target != Pi
}

func (a directoryAdapter) ResourceKinds() []resource.Kind {
	if a.target == Pi {
		return []resource.Kind{resource.Skill}
	}
	return []resource.Kind{resource.Skill, resource.SubAgent}
}

func (a directoryAdapter) Capabilities(kind resource.Kind) []resource.Capability {
	if !a.Supports(kind) {
		return nil
	}
	return []resource.Capability{resource.CapabilityFilesystemRead, resource.CapabilityFilesystemWrite}
}

func (a directoryAdapter) HasCapability(kind resource.Kind, capability resource.Capability) bool {
	for _, declared := range a.Capabilities(kind) {
		if declared == capability {
			return true
		}
	}
	return false
}

func (a directoryAdapter) RuntimeCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{}
}

func (a directoryAdapter) GovernanceCapabilities() map[policy.Control]bool {
	return map[policy.Control]bool{
		policy.ControlToolInterception:    false,
		policy.ControlApprovalPauseResume: false,
		policy.ControlDurationBudget:      false,
		policy.ControlCostBudget:          false,
		policy.ControlToolCallBudget:      false,
		policy.ControlSubagentLimits:      false,
		policy.ControlNetworkRestriction:  false,
		policy.ControlCredentialScope:     false,
		policy.ControlRunTermination:      false,
		policy.ControlRuntimeEvents:       false,
	}
}

func joinCapabilities(capabilities []resource.Capability) string {
	values := make([]string, len(capabilities))
	for i, capability := range capabilities {
		values[i] = string(capability)
	}
	return strings.Join(values, ", ")
}

var supported = map[Target]Adapter{
	ClaudeCode: directoryAdapter{target: ClaudeCode, dir: ".claude"},
	Codex:      directoryAdapter{target: Codex, dir: ".codex"},
	Pi:         directoryAdapter{target: Pi, dir: ".pi", globalDir: filepath.Join(".pi", "agent")},
}

// ForAgent returns the richer adapter contract used by target-specific
// inspection and planning. For retains the historical Adapter return type for
// callers that only need Skill placement.
func ForAgent(target Target) (AgentAdapter, bool) {
	a, ok := supported[target]
	if !ok {
		return nil, false
	}
	return a, true
}

// For returns the adapter for target when that target is supported.
func For(target Target) (Adapter, bool) {
	a, ok := supported[target]
	return a, ok
}

// ValidateIdentifier accepts one portable skill-directory name, never a path.
func ValidateIdentifier(identifier string) error {
	if identifier == "" || identifier == "." || identifier == ".." || strings.ContainsAny(identifier, `/\\`) || filepath.Base(identifier) != identifier {
		return fmt.Errorf("unsafe skill identifier %q", identifier)
	}
	return nil
}

// Supported returns all supported adapters in stable target order.
func Supported() []Adapter {
	return []Adapter{supported[ClaudeCode], supported[Codex], supported[Pi]}
}
