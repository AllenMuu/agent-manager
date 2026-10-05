package adapter

import (
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
)

// EnforcementDeclaration describes placement only. Existing RuntimeCapabilities
// and GovernanceCapabilities cannot establish execution interception.
func EnforcementDeclaration(target Target) (enforcement.Declaration, error) {
	if _, ok := For(target); !ok {
		return enforcement.Declaration{}, fmt.Errorf("unsupported target %q", target)
	}
	return enforcement.Declaration{Name: string(target), Kind: "directory"}, nil
}
