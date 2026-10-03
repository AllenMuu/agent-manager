// Package cli provides the Agent Manager command-line interface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	configPath string
}

// NewRootCommand constructs the primary Agent Manager command tree.
func NewRootCommand() *cobra.Command {
	return NewAgentManagerCommand()
}

// NewAgentManagerCommand constructs the primary Agent Manager command tree.
func NewAgentManagerCommand() *cobra.Command {
	return newRootCommand("agent-manager", false)
}

// NewSkillManagerCommand constructs the temporary Skill Manager compatibility
// alias. It preserves command behavior while directing operators to the new
// primary command.
func NewSkillManagerCommand() *cobra.Command {
	return newRootCommand("skill-manager", true)
}

func newRootCommand(name string, deprecated bool) *cobra.Command {
	options := &rootOptions{}
	root := &cobra.Command{
		Use:           name,
		Short:         "Discover and manage local agent resources",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	if deprecated {
		root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: skill-manager is deprecated; use agent-manager instead")
		}
	}
	root.PersistentFlags().StringVar(&options.configPath, "config", "", "path to Agent Manager configuration")
	root.AddCommand(newSearchCommand(options))
	root.AddCommand(newAgentsCommand())
	root.AddCommand(newRolesCommand(options))
	root.AddCommand(newTaskCommand())
	root.AddCommand(newArtifactsCommand())
	root.AddCommand(newEvalCommand())
	root.AddCommand(newMemoryCommand(options))
	root.AddCommand(newSubAgentsCommand(options))
	root.AddCommand(newInitCommand())
	root.AddCommand(newRecommendCommand(options))
	root.AddCommand(newWebCommand(options))
	root.AddCommand(newProjectCommands(options)...)
	root.AddCommand(newGovernanceCommands()...)
	return root
}
