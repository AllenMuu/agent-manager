// Package role defines agent-neutral SubAgent/workflow role contracts.
package role

import (
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/artifact"
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
	Outputs     []artifact.Kind `yaml:"outputs" json:"outputs"`
	Permissions Permissions     `yaml:"permissions" json:"permissions"`
}

type Binding struct {
	Target       adapter.Target              `json:"target"`
	Role         ID                          `json:"role"`
	Inputs       []artifact.Kind             `json:"inputs"`
	Outputs      []artifact.Kind             `json:"outputs"`
	Supported    bool                        `json:"supported"`
	Warnings     []string                    `json:"warnings"`
	Missing      []string                    `json:"missing"`
	Capabilities adapter.RuntimeCapabilities `json:"capabilities"`
}

// Builtins returns a stable copy of the four canonical role contracts.
func Builtins() []Contract {
	return []Contract{
		{ID: Planner, Role: Planner, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec}, Outputs: []artifact.Kind{artifact.Plan}, Permissions: Permissions{Filesystem: Read, Shell: Denied, Network: Denied}},
		{ID: Implementer, Role: Implementer, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan}, Outputs: []artifact.Kind{artifact.Implementation}, Permissions: Permissions{Filesystem: Write, Shell: Allowed, Network: Denied}},
		{ID: Reviewer, Role: Reviewer, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan, artifact.Implementation}, Outputs: []artifact.Kind{artifact.Verification}, Permissions: Permissions{Filesystem: Read, Shell: Denied, Network: Denied}},
		{ID: Verifier, Role: Verifier, Inputs: []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan, artifact.Implementation}, Outputs: []artifact.Kind{artifact.Verification}, Permissions: Permissions{Filesystem: Read, Shell: Allowed, Network: Denied}},
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
		if !input.Valid() {
			return fmt.Errorf("role %q contains invalid input artifact %q", c.ID, input)
		}
	}
	for _, output := range c.Outputs {
		if !output.Valid() {
			return fmt.Errorf("role %q contains invalid output artifact %q", c.ID, output)
		}
	}
	if c.Permissions.Filesystem != Denied && c.Permissions.Filesystem != Read && c.Permissions.Filesystem != Write {
		return fmt.Errorf("role %q has invalid filesystem permission %q", c.ID, c.Permissions.Filesystem)
	}
	for name, value := range map[string]Permission{"shell": c.Permissions.Shell, "network": c.Permissions.Network} {
		if value != Denied && value != Allowed {
			return fmt.Errorf("role %q has invalid %s permission %q", c.ID, name, value)
		}
	}
	return nil
}

// Bind compares a role contract with verified runtime capabilities. Resource
// placement support never proves that a runtime can enforce role permissions.
func Bind(target adapter.Target, contract Contract) (Binding, error) {
	if err := contract.Validate(); err != nil {
		return Binding{}, err
	}
	a, ok := adapter.For(target)
	if !ok {
		return Binding{Target: target, Role: contract.ID, Missing: []string{"adapter"}}, fmt.Errorf("unsupported agent target %q", target)
	}
	caps := a.RuntimeCapabilities()
	binding := Binding{Target: target, Role: contract.ID, Inputs: append([]artifact.Kind(nil), contract.Inputs...), Outputs: append([]artifact.Kind(nil), contract.Outputs...), Supported: true, Capabilities: caps, Warnings: []string{}, Missing: []string{}}
	if (contract.Permissions.Filesystem == Read || contract.Permissions.Filesystem == Write) && !caps.FilesystemRead {
		binding.Missing = append(binding.Missing, "runtime-filesystem-read")
	}
	if contract.Permissions.Filesystem == Write && !caps.FilesystemWrite {
		binding.Missing = append(binding.Missing, "runtime-filesystem-write")
	}
	if contract.Permissions.Filesystem == Denied && !caps.RestrictFilesystem || contract.Permissions.Filesystem == Read && !caps.RestrictFilesystem {
		binding.Missing = append(binding.Missing, "runtime-filesystem-restriction")
	}
	if contract.Permissions.Shell == Allowed && !caps.Shell {
		binding.Missing = append(binding.Missing, "runtime-shell")
	}
	if contract.Permissions.Shell == Denied && !caps.RestrictShell {
		binding.Missing = append(binding.Missing, "runtime-shell-restriction")
	}
	if contract.Permissions.Network == Allowed && !caps.Network {
		binding.Missing = append(binding.Missing, "runtime-network")
	}
	if contract.Permissions.Network == Denied && !caps.RestrictNetwork {
		binding.Missing = append(binding.Missing, "runtime-network-restriction")
	}
	if len(binding.Missing) > 0 {
		binding.Supported = false
		binding.Warnings = append(binding.Warnings, "this adapter only verifies resource placement; configure and verify runtime permissions before executing the role")
	}
	return binding, nil
}
