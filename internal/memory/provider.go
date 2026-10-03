// Package memory defines the provider-neutral shared Memory control model.
// It deliberately contains no provider client or network implementation.
package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type Capability string

const (
	CapabilityRead   Capability = "read"
	CapabilityWrite  Capability = "write"
	CapabilitySearch Capability = "search"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// ConfigReference identifies where provider configuration is held. It never
// contains a credential or provider secret. The referenced value remains in
// the user's environment/keychain/file and is not managed by Agent Manager.
type ConfigReference struct {
	Kind string `json:"kind" yaml:"kind"`
	Name string `json:"name" yaml:"name"`
}

// Redacted returns an output-safe reference. The kind remains useful for
// diagnostics, while the external lookup name is omitted from evidence.
func (r ConfigReference) Redacted() ConfigReference {
	r.Name = "[redacted]"
	return r
}

func (r ConfigReference) MarshalJSON() ([]byte, error) {
	type plain ConfigReference
	return json.Marshal(plain(r.Redacted()))
}

func (r ConfigReference) MarshalYAML() (any, error) {
	return map[string]string{"kind": r.Kind, "name": "[redacted]"}, nil
}

type ProviderConfig struct {
	Retrieval     RetrievalPolicy `json:"retrieval" yaml:"retrieval"`
	Version       string          `json:"version" yaml:"version"`
	ID            string          `json:"id" yaml:"id"`
	Provider      string          `json:"provider" yaml:"provider"`
	Configuration ConfigReference `json:"configuration" yaml:"configuration"`
	Scopes        []Scope         `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Capabilities  []Capability    `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
}

func (c ProviderConfig) Validate() error {
	if _, err := c.Retrieval.normalized(); err != nil {
		return fmt.Errorf("invalid Memory retrieval policy")
	}
	if c.Version != "v1" {
		return fmt.Errorf("unsupported memory provider configuration version %q", c.Version)
	}
	if err := validateToken("memory provider id", c.ID); err != nil {
		return err
	}
	if err := validateToken("memory provider", c.Provider); err != nil {
		return err
	}
	if err := c.Configuration.validate(); err != nil {
		return err
	}
	if len(c.Scopes) == 0 {
		return fmt.Errorf("memory provider must declare at least one scope")
	}
	for _, scope := range c.Scopes {
		if scope != ScopeUser && scope != ScopeProject {
			return fmt.Errorf("unsupported memory scope %q", scope)
		}
	}
	if duplicates := duplicateScopes(c.Scopes); len(duplicates) > 0 {
		return fmt.Errorf("duplicate memory scope %q", duplicates[0])
	}
	for _, capability := range c.Capabilities {
		if capability != CapabilityRead && capability != CapabilityWrite && capability != CapabilitySearch {
			return fmt.Errorf("unsupported memory capability %q", capability)
		}
	}
	if duplicates := duplicateCapabilities(c.Capabilities); len(duplicates) > 0 {
		return fmt.Errorf("duplicate memory capability %q", duplicates[0])
	}
	return nil
}

func (r ConfigReference) validate() error {
	switch r.Kind {
	case "env", "file", "keychain":
	default:
		return fmt.Errorf("unsupported memory configuration reference kind %q", r.Kind)
	}
	if strings.TrimSpace(r.Name) == "" || strings.ContainsAny(r.Name, "\x00\n\r") {
		return fmt.Errorf("memory configuration reference must name an external value, not contain it")
	}
	if r.Kind == "env" && !validEnvironmentName(r.Name) {
		return fmt.Errorf("memory environment reference must be a valid environment variable name")
	}
	return nil
}

func validateToken(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain whitespace or control characters", label)
		}
	}
	return nil
}

func validEnvironmentName(value string) bool {
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return value != ""
}

func duplicateScopes(values []Scope) []Scope {
	seen := make(map[Scope]struct{}, len(values))
	var duplicates []Scope
	for _, value := range values {
		if _, ok := seen[value]; ok {
			duplicates = append(duplicates, value)
			continue
		}
		seen[value] = struct{}{}
	}
	return duplicates
}

func duplicateCapabilities(values []Capability) []Capability {
	seen := make(map[Capability]struct{}, len(values))
	var duplicates []Capability
	for _, value := range values {
		if _, ok := seen[value]; ok {
			duplicates = append(duplicates, value)
			continue
		}
		seen[value] = struct{}{}
	}
	return duplicates
}

// Redacted returns an output-safe view. Reference names are intentionally
// omitted because environment variable/keychain names can disclose credential
// usage and must not enter status output or journal evidence.
func (c ProviderConfig) Redacted() ProviderConfig {
	c.Configuration.Name = "[redacted]"
	return c
}

// MarshalJSON keeps provider references out of status output and journals.
// Callers that need to persist the actual reference should use a dedicated
// configuration store rather than serializing this control-plane model.
func (c ProviderConfig) MarshalJSON() ([]byte, error) {
	type plain ProviderConfig
	return json.Marshal(plain(c.Redacted()))
}

// MarshalYAML applies the same safety rule as MarshalJSON for YAML output.
func (c ProviderConfig) MarshalYAML() (any, error) {
	return map[string]any{
		"version":       c.Version,
		"id":            c.ID,
		"provider":      c.Provider,
		"configuration": c.Redacted().Configuration,
		"scopes":        c.Scopes,
		"capabilities":  c.Capabilities,
		"retrieval":     c.Retrieval,
	}, nil
}

func (c ProviderConfig) JournalEvidence() string {
	b, _ := json.Marshal(c.Redacted())
	return string(b)
}

func (c ProviderConfig) String() string { return c.JournalEvidence() }

type AgentAccess struct {
	Agent        string       `json:"agent" yaml:"agent"`
	Capabilities []Capability `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Scopes       []Scope      `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// ProviderStatus is the read-only capability snapshot returned by a Memory
// provider. Providers may discover this locally; the control plane never
// assumes that a configured provider is reachable or available.
type ProviderStatus struct {
	StructuredCapabilities *StructuredCapabilities `json:"structuredCapabilities,omitempty" yaml:"structuredCapabilities,omitempty"`
	Ranking                string                  `json:"ranking,omitempty" yaml:"ranking,omitempty"`
	Available              bool                    `json:"available" yaml:"available"`
	Unsupported            bool                    `json:"unsupported,omitempty" yaml:"unsupported,omitempty"`
	Reason                 string                  `json:"reason,omitempty" yaml:"reason,omitempty"`
	Capabilities           []Capability            `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Scopes                 []Scope                 `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// Provider is the narrow boundary between the control plane and a user-owned
// Memory implementation. Promotion is deliberately separate from status and
// inspection so ordinary resource operations cannot write by accident.
type Provider interface {
	Status() ProviderStatus
	Promote(scope Scope, knowledge string) error
}

// Searcher is the optional read boundary used when assembling task context.
// It is separate from Provider so write-only integrations need not implement
// search, and callers can keep storage concerns outside the resolver.
type Searcher interface {
	Status() ProviderStatus
	Search(scope Scope, query string) ([]string, error)
}

// MapAgentAccess intersects provider capabilities with an agent's declared
// integration capabilities and scopes. It never mutates either input and
// returns no access when the provider is unavailable.
func MapAgentAccess(provider ProviderStatus, agent AgentAccess) AgentAccess {
	if !provider.Available {
		return AgentAccess{Agent: agent.Agent}
	}
	capabilities := intersectCapabilities(provider.Capabilities, agent.Capabilities)
	scopes := intersectScopes(provider.Scopes, agent.Scopes)
	return AgentAccess{Agent: agent.Agent, Capabilities: capabilities, Scopes: scopes}
}

func intersectCapabilities(provider, agent []Capability) []Capability {
	allowed := make(map[Capability]struct{}, len(provider))
	for _, capability := range provider {
		allowed[capability] = struct{}{}
	}
	result := make([]Capability, 0, len(agent))
	for _, capability := range agent {
		if _, ok := allowed[capability]; ok {
			result = append(result, capability)
		}
	}
	return result
}

func intersectScopes(provider, agent []Scope) []Scope {
	allowed := make(map[Scope]struct{}, len(provider))
	for _, scope := range provider {
		allowed[scope] = struct{}{}
	}
	result := make([]Scope, 0, len(agent))
	for _, scope := range agent {
		if _, ok := allowed[scope]; ok {
			result = append(result, scope)
		}
	}
	return result
}

func (a AgentAccess) Supports(capability Capability, scope Scope) bool {
	capable, scoped := false, false
	for _, candidate := range a.Capabilities {
		if candidate == capability {
			capable = true
		}
	}
	for _, candidate := range a.Scopes {
		if candidate == scope {
			scoped = true
		}
	}
	return capable && scoped
}
