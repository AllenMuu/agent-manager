// Package role defines agent-neutral SubAgent/workflow role contracts.
package role

import (
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

type ID string

const (
	Planner     ID = "planner"
	Implementer ID = "implementer"
	Reviewer    ID = "reviewer"
	Verifier    ID = "verifier"
)

type Permission string

const (
	Denied  Permission = "denied"
	Read    Permission = "read"
	Write   Permission = "write"
	Allowed Permission = "allowed"
)

type Permissions struct {
	Filesystem Permission `yaml:"filesystem" json:"filesystem"`
	Shell      Permission `yaml:"shell" json:"shell"`
	Network    Permission `yaml:"network" json:"network"`
}

// Contract describes the artifact boundary and permissions of a role. It is
// independent of Claude, Codex, Pi, or any other runtime.
type Contract struct {
	ID          ID              `yaml:"id" json:"id"`
	Role        ID              `yaml:"role" json:"role"`
	Inputs      []artifact.Kind `yaml:"inputs" json:"inputs"`
	Outputs     []string        `yaml:"outputs" json:"outputs"`
	Permissions Permissions     `yaml:"permissions" json:"permissions"`
}

type Binding struct {
	Target       adapter.Target        `json:"target"`
	Role         ID                    `json:"role"`
	Supported    bool                  `json:"supported"`
	Warnings     []string              `json:"warnings"`
	Missing      []string              `json:"missing"`
	Capabilities []resource.Capability `json:"capabilities"`
}

// Builtins returns a stable copy of the four canonical role contracts.
func Builtins() []Contract {
	return []Contract{
		{ID: Planner, Role: Planner, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec}, Outputs: []string{"plan"}, Permissions: Permissions{Filesystem: Read, Shell: Denied, Network: Denied}},
		{ID: Implementer, Role: Implementer, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan}, Outputs: []string{"implementation"}, Permissions: Permissions{Filesystem: Write, Shell: Allowed, Network: Denied}},
		{ID: Reviewer, Role: Reviewer, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan, artifact.Implementation}, Outputs: []string{"review"}, Permissions: Permissions{Filesystem: Read, Shell: Denied, Network: Denied}},
		{ID: Verifier, Role: Verifier, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan, artifact.Implementation}, Outputs: []string{"verification"}, Permissions: Permissions{Filesystem: Read, Shell: Allowed, Network: Denied}},
	}
}

func For(id ID) (Contract, bool) {
	for _, contract := range Builtins() {
		if contract.ID == id {
			return contract, true
		}
	}
	return Contract{}, false
}

func (c Contract) Validate() error {
	if c.ID == "" || c.Role == "" || c.ID != c.Role {
		return fmt.Errorf("role contract requires matching id and role")
	}
	if len(c.Outputs) == 0 {
		return fmt.Errorf("role %q must declare an output artifact", c.ID)
	}
	for _, input := range c.Inputs {
		if input == "" {
			return fmt.Errorf("role %q contains an empty input artifact", c.ID)
		}
	}
	for name, value := range map[string]Permission{"filesystem": c.Permissions.Filesystem, "shell": c.Permissions.Shell, "network": c.Permissions.Network} {
		switch value {
		case "", Denied, Read, Write, Allowed:
		default:
			return fmt.Errorf("role %q has invalid %s permission %q", c.ID, name, value)
		}
	}
	return nil
}

// Bind maps a role contract to the capabilities an adapter actually declares.
// It never assumes that a runtime can execute shell/network operations merely
// because the role asks for them; those capabilities are surfaced as warnings.
func Bind(target adapter.Target, contract Contract) (Binding, error) {
	if err := contract.Validate(); err != nil {
		return Binding{}, err
	}
	a, ok := adapter.For(target)
	if !ok {
		return Binding{Target: target, Role: contract.ID, Missing: []string{"adapter"}}, fmt.Errorf("unsupported agent target %q", target)
	}
	binding := Binding{Target: target, Role: contract.ID, Supported: true, Capabilities: a.Capabilities(resource.Skill), Warnings: []string{}, Missing: []string{}}
	if contract.Permissions.Filesystem == Read || contract.Permissions.Filesystem == Write {
		if !a.HasCapability(resource.Skill, resource.CapabilityFilesystemRead) {
			binding.Supported = false
			binding.Missing = append(binding.Missing, string(resource.CapabilityFilesystemRead))
		}
	}
	if contract.Permissions.Filesystem == Write && !a.HasCapability(resource.Skill, resource.CapabilityFilesystemWrite) {
		binding.Supported = false
		binding.Missing = append(binding.Missing, string(resource.CapabilityFilesystemWrite))
	}
	if contract.Permissions.Shell == Allowed {
		binding.Warnings = append(binding.Warnings, "adapter does not declare a shell capability; runtime must enforce this role permission")
	}
	if contract.Permissions.Network == Allowed {
		binding.Warnings = append(binding.Warnings, "adapter does not declare a network capability; network access remains disabled by default")
	}
	return binding, nil
}
