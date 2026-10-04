// Package enforcement provides deterministic, declarative control-plane preflight.
// It neither observes nor starts a runtime and does not authorize execution.
package enforcement

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/policy"
)

type Dimension string

const (
	Tool       Dimension = "tool"
	Filesystem Dimension = "filesystem"
	Network    Dimension = "network"
	Process    Dimension = "process"
	Credential Dimension = "credential"
)

var dimensions = []Dimension{Tool, Filesystem, Network, Process, Credential}

type Permission struct {
	State string   `json:"state"`
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}
type Contribution struct {
	Provider    string                   `json:"provider"`
	Permissions map[Dimension]Permission `json:"permissions"`
}
type AttributedPermission struct {
	Provider   string     `json:"provider"`
	Permission Permission `json:"permission"`
}
type PermissionView struct {
	State         string                 `json:"state"`
	Policy        Permission             `json:"policy"`
	Contributions []AttributedPermission `json:"contributions"`
}

// Request is additive to AgentPolicy: existing policy versions remain unchanged.
// Permissions are exact opaque identifiers (paths, destinations, process functions,
// credential references/scopes); they do not imply prefix or wildcard authority.
type Request struct {
	Version       string                   `json:"version"`
	Policy        policy.AgentPolicy       `json:"policy"`
	Permissions   map[Dimension]Permission `json:"permissions"`
	Contributions []Contribution           `json:"contributions"`
	Required      []Dimension              `json:"required"`
	Optional      []Dimension              `json:"optional"`
	Provider      Declaration              `json:"provider"`
}
type Capability struct {
	Support  string `json:"support"`
	Update   string `json:"update"`
	Verified bool   `json:"verified"`
}
type Declaration struct {
	Governance map[policy.Control]bool  `json:"governance"`
	Name       string                   `json:"name"`
	Kind       string                   `json:"kind"`
	Controls   map[Dimension]Capability `json:"controls"`
}

func validDimension(d Dimension) bool {
	for _, v := range dimensions {
		if d == v {
			return true
		}
	}
	return false
}
func normalized(p Permission) (Permission, error) {
	if p.State == "" {
		p.State = "unknown"
	}
	if p.State != "known" && p.State != "unknown" {
		return p, fmt.Errorf("invalid permission state")
	}
	p.Allow = append([]string{}, p.Allow...)
	p.Deny = append([]string{}, p.Deny...)
	for _, values := range [][]string{p.Allow, p.Deny} {
		for _, v := range values {
			if strings.TrimSpace(v) != v || v == "" || strings.ContainsAny(v, "\x00\r\n") || strings.ContainsAny(v, "*?") {
				return p, fmt.Errorf("permission identifiers must be exact and nonempty")
			}
		}
		sort.Strings(values)
	}
	p.Allow = unique(p.Allow)
	p.Deny = unique(p.Deny)
	return p, nil
}
func unique(v []string) []string {
	out := []string{}
	for _, s := range v {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func ResolvePermissions(r Request) (map[Dimension]PermissionView, error) {
	if r.Version != "v1" {
		return nil, fmt.Errorf("unsupported enforcement preflight version")
	}
	if err := r.Policy.Validate(); err != nil {
		return nil, err
	}
	for _, d := range sortedKeys(r.Permissions) {
		if !validDimension(d) {
			return nil, dimensionError{d, fmt.Sprintf("unknown permission dimension %q", d)}
		}
	}
	seen := map[string]bool{}
	for _, c := range r.Contributions {
		if c.Provider == "" || strings.TrimSpace(c.Provider) != c.Provider || seen[c.Provider] {
			return nil, fmt.Errorf("provider contributions require unique nonempty names")
		}
		seen[c.Provider] = true
		for _, d := range sortedKeys(c.Permissions) {
			if !validDimension(d) {
				return nil, dimensionError{d, fmt.Sprintf("unknown contribution dimension %q", d)}
			}
		}
	}
	view := map[Dimension]PermissionView{}
	for _, d := range dimensions {
		p := r.Permissions[d]
		// Legacy policy constraints are retained separately, never translated to
		// filesystem/process permissions or verified runtime interception.
		switch d {
		case Tool:
			if _, ok := r.Permissions[d]; !ok {
				p = Permission{State: "known", Allow: r.Policy.Tools.Allow, Deny: r.Policy.Tools.Deny}
			}
		case Network:
			if r.Policy.Network != nil {
				if _, ok := r.Permissions[d]; ok {
					return nil, fmt.Errorf("network has conflicting policy declarations")
				}
				p = Permission{State: "known", Allow: r.Policy.Network.AllowedDomains, Deny: r.Policy.Network.DeniedDomains}
			}
		case Credential:
			if r.Policy.Credentials != nil {
				if _, ok := r.Permissions[d]; ok {
					return nil, fmt.Errorf("credential has conflicting policy declarations")
				}
				p = Permission{State: "known", Allow: r.Policy.Credentials.AllowedScopes, Deny: r.Policy.Credentials.DeniedScopes}
			}
		}
		if d == Tool {
			if _, ok := r.Permissions[d]; ok {
				return nil, fmt.Errorf("tool rules belong in AgentPolicy")
			}
		}
		p, err := normalized(p)
		if err != nil {
			return nil, dimensionError{d, fmt.Sprintf("%s: %s", d, err)}
		}
		v := PermissionView{State: "unknown", Policy: p, Contributions: []AttributedPermission{}}
		for _, c := range r.Contributions {
			cp, err := normalized(c.Permissions[d])
			if err != nil {
				return nil, dimensionError{d, fmt.Sprintf("%s provider %s: %s", d, c.Provider, err)}
			}
			v.Contributions = append(v.Contributions, AttributedPermission{c.Provider, cp})
		}
		sort.Slice(v.Contributions, func(i, j int) bool { return v.Contributions[i].Provider < v.Contributions[j].Provider })
		// Sources stay distinct. Declared constraints alone cannot establish actual
		// effective reachability, even when all identifiers are known.
		view[d] = v
	}
	return view, nil
}

type Diagnostic struct {
	Dimension Dimension `json:"dimension"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
}
type Result struct {
	Governance  policy.CapabilityReport      `json:"governance"`
	Accepted    bool                         `json:"accepted"`
	Basis       string                       `json:"basis"`
	Permissions map[Dimension]PermissionView `json:"permissions"`
	Controls    map[Dimension]Capability     `json:"controls"`
	Errors      []Diagnostic                 `json:"errors"`
	Warnings    []Diagnostic                 `json:"warnings"`
}

func Preflight(r Request) Result {
	out := Result{Basis: "offline-declarations-only", Errors: []Diagnostic{}, Warnings: []Diagnostic{}}
	v, err := ResolvePermissions(r)
	if err != nil {
		out.Errors = append(out.Errors, inputDiagnostic("invalid_permissions", err))
		return out
	}
	out.Permissions = v
	out.Controls, err = DiscoverCapabilities(r.Provider)
	if err != nil {
		out.Errors = append(out.Errors, inputDiagnostic("invalid_capabilities", err))
		return out
	}
	required := append([]Dimension{}, r.Required...)
	optional := append([]Dimension{}, r.Optional...)
	legacyOptional := map[policy.Control]bool{}
	for _, c := range r.Policy.Enforcement.OptionalControls {
		legacyOptional[c] = true
	}
	for _, pair := range []struct {
		dimension  Dimension
		control    policy.Control
		configured bool
	}{{Tool, policy.ControlToolInterception, true}, {Network, policy.ControlNetworkRestriction, r.Policy.Network != nil}, {Credential, policy.ControlCredentialScope, r.Policy.Credentials != nil}} {
		if pair.configured {
			if legacyOptional[pair.control] {
				optional = append(optional, pair.dimension)
			} else {
				required = append(required, pair.dimension)
			}
		}
	}
	for _, d := range []Dimension{Filesystem, Process, Network, Credential} {
		if _, ok := r.Permissions[d]; ok {
			if !containsDimension(optional, d) {
				required = append(required, d)
			}
		}
	}
	for _, d := range normalizedDimensions(required) {
		if containsDimension(optional, d) {
			out.Errors = append(out.Errors, Diagnostic{d, "conflicting_requirement", "dimension cannot be both mandatory and optional"})
		}
	}
	for _, d := range normalizedDimensions(required) {
		if !validDimension(d) {
			out.Errors = append(out.Errors, Diagnostic{d, "invalid_dimension", "unknown mandatory dimension"})
			continue
		}
		c := out.Controls[d]
		if c.Support != "supported" || !c.Verified {
			out.Errors = append(out.Errors, Diagnostic{d, "missing_control", string(d) + " mandatory enforcement is unknown or unsupported"})
		}
	}
	for _, d := range normalizedDimensions(optional) {
		if !validDimension(d) {
			out.Errors = append(out.Errors, Diagnostic{d, "invalid_dimension", "unknown optional dimension"})
			continue
		}
		c := out.Controls[d]
		if c.Support != "supported" || !c.Verified {
			out.Warnings = append(out.Warnings, Diagnostic{d, "optional_control_gap", string(d) + " optional enforcement is unknown or unsupported"})
		}
	}
	governance := r.Provider.Governance
	if r.Provider.Kind != "execution" {
		governance = nil
	}
	out.Governance = policy.CheckCapabilities(r.Policy, governance)
	for _, c := range out.Governance.Missing {
		out.Errors = append(out.Errors, Diagnostic{Code: "missing_governance_control", Message: string(c) + " mandatory governance control is unsupported"})
	}
	for _, c := range out.Governance.Warnings {
		out.Warnings = append(out.Warnings, Diagnostic{Code: "optional_governance_gap", Message: string(c) + " optional governance control is unsupported"})
	}
	out.Accepted = len(out.Errors) == 0
	return out
}

// DiscoverCapabilities validates a submitted declaration. Verified is a caller
// assertion for an execution provider, not independent evidence of a live hook.
func DiscoverCapabilities(p Declaration) (map[Dimension]Capability, error) {
	for _, c := range sortedKeys(p.Governance) {
		if !policy.IsKnownControl(c) {
			return nil, fmt.Errorf("unknown governance control %q", c)
		}
	}

	if p.Kind != "" && p.Kind != "execution" && p.Kind != "noop" && p.Kind != "directory" {
		return nil, fmt.Errorf("unknown provider kind")
	}
	for _, d := range sortedKeys(p.Controls) {
		if !validDimension(d) {
			return nil, dimensionError{d, fmt.Sprintf("unknown control dimension %q", d)}
		}
	}
	out := map[Dimension]Capability{}
	for _, d := range dimensions {
		c := p.Controls[d]
		if c.Support == "" {
			c.Support = "unknown"
		}
		if c.Update == "" {
			c.Update = "unsupported"
		}
		if c.Support != "supported" && c.Support != "unsupported" && c.Support != "unknown" {
			return nil, dimensionError{d, fmt.Sprintf("%s invalid support", d)}
		}
		if c.Update != "live-update" && c.Update != "recreate-required" && c.Update != "unsupported" {
			return nil, dimensionError{d, fmt.Sprintf("%s invalid policy update mode", d)}
		}
		if c.Support != "supported" && (c.Verified || c.Update != "unsupported") {
			return nil, dimensionError{d, fmt.Sprintf("%s unsupported control cannot advertise verified updates", d)}
		}
		if p.Kind == "noop" || p.Kind == "directory" || p.Kind == "" {
			c = Capability{Support: "unsupported", Update: "unsupported"}
		}
		out[d] = c
	}
	return out, nil
}

func normalizedDimensions(input []Dimension) []Dimension {
	values := make([]string, len(input))
	for i, d := range input {
		values[i] = string(d)
	}
	sort.Strings(values)
	values = unique(values)
	out := make([]Dimension, len(values))
	for i, d := range values {
		out[i] = Dimension(d)
	}
	return out
}

// LoadRequest strictly reads the additive v1 JSON declaration document.
func LoadRequest(data []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var r Request
	if err := decoder.Decode(&r); err != nil {
		return r, fmt.Errorf("parse preflight request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return r, fmt.Errorf("preflight request must contain exactly one JSON document")
	}
	return r, nil
}

func containsDimension(values []Dimension, d Dimension) bool {
	for _, v := range values {
		if v == d {
			return true
		}
	}
	return false
}

// NoopDeclaration is the canonical non-executing provider declaration.
func NoopDeclaration() Declaration { return Declaration{Name: "noop", Kind: "noop"} }
func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

type dimensionError struct {
	dimension Dimension
	message   string
}

func (e dimensionError) Error() string { return e.message }
func inputDiagnostic(code string, err error) Diagnostic {
	d := Diagnostic{Code: code, Message: err.Error()}
	var e dimensionError
	if errors.As(err, &e) {
		d.Dimension = e.dimension
	}
	return d
}
