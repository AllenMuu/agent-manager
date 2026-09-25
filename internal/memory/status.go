package memory

import "sort"

const (
	StateUnconfigured = "unconfigured"
	StateAvailable    = "available"
	StateUnavailable  = "unavailable"
	StateUnsupported  = "unsupported"
)

// AgentStatus is the read-only Memory integration result for one agent. The
// capability and scope lists are the intersection of provider and agent
// declarations; explicit gap lists preserve unsupported and unavailable
// requirements instead of silently dropping them. No provider reference is
// included in this evidence model.
type AgentStatus struct {
	Agent                   string       `json:"agent" yaml:"agent"`
	State                   string       `json:"state" yaml:"state"`
	Reason                  string       `json:"reason,omitempty" yaml:"reason,omitempty"`
	Capabilities            []Capability `json:"capabilities" yaml:"capabilities"`
	UnsupportedCapabilities []Capability `json:"unsupportedCapabilities" yaml:"unsupportedCapabilities"`
	UnavailableCapabilities []Capability `json:"unavailableCapabilities" yaml:"unavailableCapabilities"`
	Scopes                  []Scope      `json:"scopes" yaml:"scopes"`
	UnsupportedScopes       []Scope      `json:"unsupportedScopes" yaml:"unsupportedScopes"`
	UnavailableScopes       []Scope      `json:"unavailableScopes" yaml:"unavailableScopes"`
}

// StatusReport is the safe, output-ready snapshot for `memory status`.
// ConfigReference is intentionally absent so environment, file, and keychain
// names cannot leak through status output.
type StatusReport struct {
	Configured   bool          `json:"configured" yaml:"configured"`
	Provider     string        `json:"provider,omitempty" yaml:"provider,omitempty"`
	State        string        `json:"state" yaml:"state"`
	Available    bool          `json:"available" yaml:"available"`
	Reason       string        `json:"reason,omitempty" yaml:"reason,omitempty"`
	Capabilities []Capability  `json:"capabilities" yaml:"capabilities"`
	Scopes       []Scope       `json:"scopes" yaml:"scopes"`
	Agents       []AgentStatus `json:"agents" yaml:"agents"`
}

// BuildStatus combines provider discovery with declared agent integrations
// and unsupported adapter identifiers. It only computes status and never
// performs provider writes.
func BuildStatus(config *ProviderConfig, provider ProviderStatus, integrations []AgentAccess, unsupported []string) StatusReport {
	report := StatusReport{
		State:        StateUnconfigured,
		Available:    false,
		Capabilities: []Capability{},
		Scopes:       []Scope{},
		Agents:       []AgentStatus{},
	}
	if config == nil {
		report.Reason = "no Memory provider is configured; add a memory.provider configuration"
	} else {
		report.Configured = true
		report.Provider = config.Provider
		report.Capabilities = append([]Capability(nil), provider.Capabilities...)
		report.Scopes = append([]Scope(nil), provider.Scopes...)
		if len(report.Capabilities) == 0 && !provider.Available {
			report.Capabilities = append([]Capability(nil), config.Capabilities...)
		}
		if len(report.Scopes) == 0 && !provider.Available {
			report.Scopes = append([]Scope(nil), config.Scopes...)
		}
		if provider.Unsupported {
			report.State = StateUnsupported
			report.Reason = provider.Reason
			if report.Reason == "" {
				report.Reason = "configured Memory provider is not supported locally"
			}
		} else if provider.Available {
			report.State = StateAvailable
			report.Available = true
		} else {
			report.State = StateUnavailable
			report.Reason = provider.Reason
			if report.Reason == "" {
				report.Reason = "configured Memory provider is unavailable"
			}
		}
	}

	for _, integration := range integrations {
		agent := AgentStatus{
			Agent:                   integration.Agent,
			State:                   StateUnsupported,
			Capabilities:            []Capability{},
			UnsupportedCapabilities: []Capability{},
			UnavailableCapabilities: []Capability{},
			Scopes:                  []Scope{},
			UnsupportedScopes:       []Scope{},
			UnavailableScopes:       []Scope{},
		}
		if config == nil {
			agent.State = StateUnconfigured
			agent.Reason = report.Reason
		} else if provider.Unsupported {
			agent.Reason = report.Reason
			agent.UnsupportedCapabilities = append([]Capability(nil), config.Capabilities...)
			agent.UnsupportedScopes = append([]Scope(nil), config.Scopes...)
		} else if !provider.Available {
			agent.State = StateUnavailable
			agent.Reason = report.Reason
			agent.UnsupportedCapabilities = unsupportedCapabilities(config.Capabilities, integration.Capabilities)
			agent.UnavailableCapabilities = availableCapabilities(config.Capabilities, integration.Capabilities)
			agent.UnsupportedScopes = unsupportedScopes(config.Scopes, integration.Scopes)
			agent.UnavailableScopes = availableScopes(config.Scopes, integration.Scopes)
		} else {
			access := MapAgentAccess(provider, integration)
			agent.Capabilities = append([]Capability(nil), access.Capabilities...)
			agent.Scopes = append([]Scope(nil), access.Scopes...)
			agent.UnsupportedCapabilities = unsupportedCapabilities(provider.Capabilities, integration.Capabilities)
			agent.UnsupportedScopes = unsupportedScopes(provider.Scopes, integration.Scopes)
			if len(integration.Capabilities) == 0 || len(integration.Scopes) == 0 {
				agent.Reason = "agent adapter does not declare a Memory integration"
			} else if len(agent.Capabilities) == 0 || len(agent.Scopes) == 0 {
				agent.Reason = "provider capabilities or scopes are not supported by this agent"
			} else {
				agent.State = StateAvailable
			}
		}
		report.Agents = append(report.Agents, agent)
	}
	for _, id := range unsupported {
		report.Agents = append(report.Agents, AgentStatus{
			Agent:                   id,
			State:                   StateUnsupported,
			Reason:                  "no compatible agent adapter is registered",
			Capabilities:            []Capability{},
			UnsupportedCapabilities: []Capability{},
			UnavailableCapabilities: []Capability{},
			Scopes:                  []Scope{},
			UnsupportedScopes:       []Scope{},
			UnavailableScopes:       []Scope{},
		})
	}
	sort.SliceStable(report.Agents, func(i, j int) bool { return report.Agents[i].Agent < report.Agents[j].Agent })
	return report
}

func unsupportedCapabilities(provider, agent []Capability) []Capability {
	result := make([]Capability, 0)
	for _, candidate := range provider {
		found := false
		for _, supported := range agent {
			if candidate == supported {
				found = true
				break
			}
		}
		if !found && !containsCapability(result, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func unsupportedScopes(provider, agent []Scope) []Scope {
	result := make([]Scope, 0)
	for _, candidate := range provider {
		found := false
		for _, supported := range agent {
			if candidate == supported {
				found = true
				break
			}
		}
		if !found && !containsScope(result, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func availableCapabilities(provider, agent []Capability) []Capability {
	result := make([]Capability, 0)
	for _, candidate := range provider {
		if containsCapability(agent, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func availableScopes(provider, agent []Scope) []Scope {
	result := make([]Scope, 0)
	for _, candidate := range provider {
		if containsScope(agent, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func containsCapability(values []Capability, candidate Capability) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsScope(values []Scope, candidate Scope) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
