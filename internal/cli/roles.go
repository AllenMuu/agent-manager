package cli

import (
	"encoding/json"
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"github.com/spf13/cobra"
)

func newRolesCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "roles", Short: "Inspect agent-neutral workflow role contracts"}
	command.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		contracts := role.Builtins()
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(contracts)
		}
		for _, contract := range contracts {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\tinputs: %v\toutputs: %v\tfilesystem: %s\tshell: %s\n", contract.ID, contract.Inputs, contract.Outputs, contract.Permissions.Filesystem, contract.Permissions.Shell); err != nil {
				return err
			}
		}
		return nil
	}
	command.AddCommand(newRoleBindCommand())
	command.AddCommand(newRoleContextCommand())
	return command
}

func newRoleContextCommand() *cobra.Command {
	var project, library, target, roleID string
	var selectedSkills []string
	command := &cobra.Command{Use: "context <task-id>", Short: "Assemble a role's canonical task inputs for an external agent", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		contract, ok := role.For(role.ID(roleID))
		if !ok {
			return fmt.Errorf("unknown role %q", roleID)
		}
		binding, err := role.Bind(adapter.Target(target), contract)
		if err != nil {
			return err
		}
		bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{
			Project: project, TaskID: args[0], Library: library, Contract: contract,
			Agent: target, SelectedSkills: selectedSkills,
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Binding role.Binding       `json:"binding"`
			Context taskcontext.Bundle `json:"context"`
		}{Binding: binding, Context: bundle})
	}}
	command.Flags().StringVar(&project, "project", ".", "project root")
	command.Flags().StringVar(&library, "library", "", "local Skill library")
	command.Flags().StringVar(&target, "agent", "", "target agent for the external handoff")
	command.Flags().StringVar(&roleID, "role", "", "canonical role id")
	command.Flags().StringSliceVar(&selectedSkills, "skill", nil, "explicitly selected Skill identifier (repeatable)")
	_ = command.MarkFlagRequired("agent")
	_ = command.MarkFlagRequired("role")
	return command
}

func newRoleBindCommand() *cobra.Command {
	var target string
	var roleID string
	var asJSON bool
	command := &cobra.Command{Use: "bind", Short: "Check a role contract against an agent adapter", RunE: func(cmd *cobra.Command, _ []string) error {
		contract, ok := role.For(role.ID(roleID))
		if !ok {
			return fmt.Errorf("unknown role %q", roleID)
		}
		binding, err := role.Bind(adapter.Target(target), contract)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(binding)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s/%s supported: %t\n", target, roleID, binding.Supported); err != nil {
			return err
		}
		for _, missing := range binding.Missing {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "missing: %s\n", missing); err != nil {
				return err
			}
		}
		for _, warning := range binding.Warnings {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", warning); err != nil {
				return err
			}
		}
		return nil
	}}
	command.Flags().StringVar(&target, "agent", "", "target agent")
	command.Flags().StringVar(&roleID, "role", "", "role id")
	command.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	_ = command.MarkFlagRequired("agent")
	_ = command.MarkFlagRequired("role")
	return command
}
