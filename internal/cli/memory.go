package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"github.com/spf13/cobra"
)

// These are the adapters known to Agent Manager. None currently declares a
// native shared-Memory integration, so available providers are reported as
// configured but unsupported for these targets until a runtime integration is
// explicitly implemented.
var memoryAgentIntegrations = []memory.AgentAccess{
	{Agent: string(adapter.ClaudeCode)},
	{Agent: string(adapter.Codex)},
	{Agent: string(adapter.Pi)},
}

func newMemoryCommand(rootOptions *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "memory",
		Short: "Inspect shared Memory provider integration",
	}
	command.AddCommand(newMemoryStatusCommand(rootOptions))
	command.AddCommand(newMemoryPromoteCommand(rootOptions))
	return command
}

func newMemoryPromoteCommand(rootOptions *rootOptions) *cobra.Command {
	var scope, knowledge, lessonsPath string
	var yes bool
	command := &cobra.Command{
		Use:   "promote",
		Short: "Explicitly append knowledge to the configured Memory provider",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(scope) == "" {
				return fmt.Errorf("--scope is required")
			}
			if (strings.TrimSpace(knowledge) == "") == (strings.TrimSpace(lessonsPath) == "") {
				return fmt.Errorf("provide exactly one of --knowledge or --lessons")
			}
			if lessonsPath != "" {
				doc, err := artifact.Load(lessonsPath)
				if err != nil {
					return err
				}
				knowledge, err = taskcontext.FormatLessons(doc)
				if err != nil {
					return err
				}
			}
			loaded, err := config.Load(rootOptions.configPath)
			if err != nil {
				return err
			}
			if loaded.Memory == nil {
				return fmt.Errorf("no Memory provider is configured; add a memory.provider configuration")
			}
			if loaded.Memory.Provider != memory.FileProviderID {
				return fmt.Errorf("provider %q is not supported locally; configure the supported file provider", loaded.Memory.Provider)
			}
			provider, err := memory.NewFileProvider(*loaded.Memory)
			if err != nil {
				if reason := provider.Status().Reason; reason != "" {
					return fmt.Errorf("%s", reason)
				}
				return err
			}
			providerStatus := provider.Status()
			scopeValue := memory.Scope(scope)
			if !containsMemoryScope(providerStatus.Scopes, scopeValue) {
				return fmt.Errorf("memory provider does not support scope %q", scope)
			}
			if !containsMemoryCapability(providerStatus.Capabilities, memory.CapabilityWrite) {
				return fmt.Errorf("memory provider does not support write capability")
			}
			if err := writeMemoryPromotionPlan(cmd, loaded.Memory.Provider, scopeValue, knowledge); err != nil {
				return err
			}
			confirmed := yes
			if !confirmed {
				if _, err := fmt.Fprint(cmd.OutOrStdout(), "Confirm [y/N]: "); err != nil {
					return err
				}
				scanner := bufio.NewScanner(cmd.InOrStdin())
				if !scanner.Scan() {
					if err := scanner.Err(); err != nil {
						return err
					}
					return nil
				}
				confirmed = strings.EqualFold(strings.TrimSpace(scanner.Text()), "y")
			}
			if !confirmed {
				return nil
			}
			if err := provider.Promote(scopeValue, knowledge); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Memory promotion complete.")
			return err
		},
	}
	command.Flags().StringVar(&scope, "scope", "", "Memory scope (user or project)")
	command.Flags().StringVar(&knowledge, "knowledge", "", "knowledge to append; never shown in the plan")
	command.Flags().StringVar(&lessonsPath, "lessons", "", "validated lessons artifact to promote explicitly")
	command.Flags().BoolVar(&yes, "yes", false, "confirm the displayed promotion plan")
	return command
}

func writeMemoryPromotionPlan(cmd *cobra.Command, provider string, scope memory.Scope, knowledge string) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Memory promotion plan\nprovider: %s\nscope: %s\nknowledge bytes: %d\n", provider, scope, len([]byte(knowledge)))
	return err
}

func containsMemoryScope(values []memory.Scope, wanted memory.Scope) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsMemoryCapability(values []memory.Capability, wanted memory.Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func newMemoryStatusCommand(rootOptions *rootOptions) *cobra.Command {
	var asJSON bool
	var project string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show configured Memory provider availability and agent mappings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			loaded, err := config.Load(rootOptions.configPath)
			if err != nil {
				return err
			}
			providerStatus := memory.ProviderStatus{}
			if loaded.Memory != nil {
				providerStatus, err = discoverConfiguredMemoryProvider(*loaded.Memory)
				if err != nil && providerStatus.Reason == "" {
					return err
				}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			inventory, err := adapter.Inventory(home, project)
			if err != nil {
				return err
			}
			unsupported := make([]string, 0)
			for _, item := range inventory {
				if item.Status == memory.StateUnsupported || item.Availability == memory.StateUnsupported {
					unsupported = append(unsupported, item.ID)
				}
			}
			report := memory.BuildStatus(loaded.Memory, providerStatus, memoryAgentIntegrations, unsupported)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			return writeMemoryStatus(cmd, report)
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	command.Flags().StringVar(&project, "project", "", "project directory whose agent locations should be included")
	return command
}

func discoverConfiguredMemoryProvider(config memory.ProviderConfig) (memory.ProviderStatus, error) {
	switch config.Provider {
	case memory.FileProviderID:
		return memory.DiscoverFileProvider(config)
	default:
		return memory.ProviderStatus{
			Unsupported: true,
			Reason:      fmt.Sprintf("provider %q is not supported locally; configure the supported file provider", config.Provider),
		}, memory.ErrProviderUnavailable
	}
}

func writeMemoryStatus(cmd *cobra.Command, report memory.StatusReport) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Memory provider: %s\n", valueOrNone(report.Provider)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Configured: %t\n", report.Configured); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", report.State); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Available: %t\n", report.Available); err != nil {
		return err
	}
	if report.Reason != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s\n", report.Reason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Capabilities: %s\n", joinMemoryValues(report.Capabilities)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Scopes: %s\n", joinMemoryValues(report.Scopes)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Agents:"); err != nil {
		return err
	}
	for _, agent := range report.Agents {
		line := fmt.Sprintf("  %s: %s", agent.Agent, agent.State)
		if len(agent.Capabilities) > 0 {
			line += "; capabilities: " + joinMemoryValues(agent.Capabilities)
		}
		if len(agent.UnsupportedCapabilities) > 0 {
			line += "; unsupported capabilities: " + joinMemoryValues(agent.UnsupportedCapabilities)
		}
		if len(agent.UnavailableCapabilities) > 0 {
			line += "; unavailable capabilities: " + joinMemoryValues(agent.UnavailableCapabilities)
		}
		if len(agent.Scopes) > 0 {
			line += "; scopes: " + joinMemoryValues(agent.Scopes)
		}
		if len(agent.UnsupportedScopes) > 0 {
			line += "; unsupported scopes: " + joinMemoryValues(agent.UnsupportedScopes)
		}
		if len(agent.UnavailableScopes) > 0 {
			line += "; unavailable scopes: " + joinMemoryValues(agent.UnavailableScopes)
		}
		if agent.Reason != "" {
			line += "; reason: " + agent.Reason
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
			return err
		}
	}
	return nil
}

func joinMemoryValues[T ~string](values []T) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = string(value)
	}
	return strings.Join(parts, ", ")
}

func valueOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
