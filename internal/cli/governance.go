package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newGovernanceCommands() []*cobra.Command {
	return []*cobra.Command{newPoliciesCommand(), newRunsCommand(), newApprovalsCommand()}
}

func newPoliciesCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{Use: "policies", Short: "Validate and inspect local AgentPolicy files"}
	command.PersistentFlags().BoolVar(&jsonOutput, "json", false, "write machine-readable JSON")
	command.AddCommand(&cobra.Command{Use: "validate <path>", Short: "Strictly parse and validate a versioned AgentPolicy", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPolicyFile(args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Valid   bool   `json:"valid"`
				ID      string `json:"id"`
				Version string `json:"version"`
			}{true, p.ID, p.Version})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "valid policy %s (%s)\n", p.ID, p.Version)
		return err
	}})
	command.AddCommand(&cobra.Command{Use: "list", Short: "List policies in the user-level local policy directory", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := localPoliciesRoot()
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			entries = nil
		} else if err != nil {
			return fmt.Errorf("read policy directory: %w", err)
		}
		items := make([]policy.AgentPolicy, 0)
		for _, entry := range entries {
			if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
				continue
			}
			p, loadErr := loadPolicyFile(filepath.Join(root, entry.Name()))
			if loadErr != nil {
				return fmt.Errorf("load policy %s: %w", entry.Name(), loadErr)
			}
			items = append(items, p)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.ID, item.Version, item.Name); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "show <policy-id>", Short: "Show a policy from the user-level local policy directory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.ContainsAny(args[0], `/\\`) || args[0] == "." || args[0] == ".." {
			return fmt.Errorf("invalid policy id %q", args[0])
		}
		root, err := localPoliciesRoot()
		if err != nil {
			return err
		}
		path := filepath.Join(root, args[0]+".yaml")
		if _, err = os.Lstat(path); os.IsNotExist(err) {
			path = filepath.Join(root, args[0]+".yml")
		}
		p, err := loadPolicyFile(path)
		if err != nil {
			return err
		}
		if p.ID != args[0] {
			return fmt.Errorf("policy file id %q does not match requested id %q", p.ID, args[0])
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(p)
		}
		data, err := yaml.Marshal(p)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}})
	return command
}

func newRunsCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{Use: "runs", Short: "Inspect and control locally recorded AgentRuns"}
	command.PersistentFlags().BoolVar(&jsonOutput, "json", false, "write machine-readable JSON")
	command.AddCommand(&cobra.Command{Use: "list", Short: "List runs in the shared user-level local state store", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		items, err := store.List()
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Runtime, item.Policy.PolicyID, item.ProjectRoot); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "show <run-id>", Short: "Show a run, its snapshot, approvals, and audit events", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		record, err := store.Get(args[0])
		if err != nil {
			return err
		}
		events, err := store.Events(args[0])
		if err != nil {
			return err
		}
		approvals, err := store.ListApprovals(args[0])
		if err != nil {
			return err
		}
		view := struct {
			Run       run.Record        `json:"run"`
			Approvals []run.Approval    `json:"approvals"`
			Events    []run.AuditRecord `json:"events"`
		}{record, approvals, events}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "run: %s\nstatus: %s\nruntime: %s\nproject: %s\npolicy: %s %s\npolicy_hash: %s\ncreated: %s\n", record.ID, record.Status, record.Runtime, record.ProjectRoot, record.Policy.PolicyID, record.Policy.Version, record.Policy.Hash, record.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return err
		}
		for _, approval := range approvals {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "approval: %s\t%s\t%s\n", approval.ID, approval.Status, approval.ActionType); err != nil {
				return err
			}
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "event: %s\t%s\t%s\t%s\n", event.ID, event.Category, event.Decision, event.ReasonCode); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "events <run-id>", Short: "List structured local audit events for a run", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		events, err := store.Events(args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(events)
	}})
	var reason string
	kill := &cobra.Command{Use: "kill <run-id>", Short: "Request a capability-aware run termination", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		manager, err := run.NewManager(store)
		if err != nil {
			return err
		}
		_, err = manager.Kill(context.Background(), args[0], reason, time.Now().UTC())
		return err
	}}
	kill.Flags().StringVar(&reason, "reason", run.TerminationOperatorRequested, "termination reason code (operator_requested, policy_violation, budget_exhausted, unexpected_termination, runtime_failure)")
	command.AddCommand(kill)
	return command
}

func newApprovalsCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{Use: "approvals", Short: "Inspect and record local approval decisions"}
	command.PersistentFlags().BoolVar(&jsonOutput, "json", false, "write machine-readable JSON")
	command.AddCommand(&cobra.Command{Use: "list", Short: "List approval requests", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		items, err := store.ListApprovals("")
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ID, item.Status, item.RunID, item.ActionType); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "show <approval-id>", Short: "Show one approval request", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := run.DefaultStore()
		if err != nil {
			return err
		}
		item, err := store.GetApproval(args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(item)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "approval: %s\nrun: %s\nstatus: %s\naction: %s\ntool: %s\nreason: %s\n", item.ID, item.RunID, item.Status, item.ActionType, item.Tool, item.ReasonCode)
		return err
	}})
	for _, decision := range []struct {
		name   string
		status run.ApprovalStatus
	}{{"approve", run.ApprovalApproved}, {"reject", run.ApprovalRejected}, {"expire", run.ApprovalExpired}} {
		decision := decision
		var reason, approverID, approverKind, approverSubject, approverProvider string
		var approverRoles []string
		cmd := &cobra.Command{Use: decision.name + " <approval-id>", Short: "Record a manual approval decision", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			store, err := run.DefaultStore()
			if err != nil {
				return err
			}
			approver := identity.ActorIdentity{ID: approverID, Kind: identity.ActorKind(approverKind), Subject: approverSubject, Provider: approverProvider, Roles: approverRoles}
			item, err := store.DecideApproval(args[0], decision.status, reason, approver, time.Now().UTC())
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(item)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "approval %s recorded as %s; no runtime action was executed or resumed\n", item.ID, item.Status)
			return err
		}}
		cmd.Flags().StringVar(&reason, "reason", "", "operator decision note")
		cmd.Flags().StringVar(&approverID, "approver-id", "", "stable ID of the actor making this decision")
		cmd.Flags().StringVar(&approverKind, "approver-kind", "", "approver kind: human, agent, or service")
		cmd.Flags().StringVar(&approverSubject, "approver-subject", "", "stable subject reference for the approver")
		cmd.Flags().StringVar(&approverProvider, "approver-provider", "", "optional identity provider reference")
		cmd.Flags().StringSliceVar(&approverRoles, "approver-role", nil, "optional approver role; may be repeated")
		for _, flag := range []string{"approver-id", "approver-kind", "approver-subject"} {
			if err := cmd.MarkFlagRequired(flag); err != nil {
				panic(err)
			}
		}
		command.AddCommand(cmd)
	}
	return command
}

func localPoliciesRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(base, "agent-manager", "policies"), nil
}

func loadPolicyFile(path string) (policy.AgentPolicy, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return policy.AgentPolicy{}, fmt.Errorf("inspect policy file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return policy.AgentPolicy{}, errors.New("policy path must be a direct regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return policy.AgentPolicy{}, fmt.Errorf("open policy file: %w", err)
	}
	defer file.Close()
	p, err := policy.Load(file)
	if err != nil {
		return policy.AgentPolicy{}, err
	}
	return p, nil
}
