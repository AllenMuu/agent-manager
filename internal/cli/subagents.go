package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
	"github.com/spf13/cobra"
)

type subAgentOptions struct {
	root    string
	library string
	json    bool
}

type subAgentValidationReport struct {
	Valid       bool                     `json:"valid"`
	Diagnostics []subAgentDiagnosticJSON `json:"diagnostics"`
}

type subAgentListReport struct {
	Definitions []subAgentJSONDefinition `json:"definitions"`
	Diagnostics []subAgentDiagnosticJSON `json:"diagnostics"`
}

type subAgentJSONDefinition struct {
	Version              string                    `json:"version"`
	ID                   string                    `json:"id"`
	Name                 string                    `json:"name"`
	Role                 string                    `json:"role"`
	Instructions         string                    `json:"instructions"`
	Skills               []string                  `json:"skills"`
	Compatibility        subAgentJSONCompatibility `json:"compatibility"`
	RequiredCapabilities []resource.Capability     `json:"requiredCapabilities"`
}

type subAgentJSONCompatibility struct {
	Agents []string `json:"agents"`
}

type subAgentDiagnosticJSON struct {
	ID      string `json:"id,omitempty"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func newSubAgentsCommand(rootOptions *rootOptions) *cobra.Command {
	options := &subAgentOptions{}
	command := &cobra.Command{
		Use:     "subagents",
		Aliases: []string{"subagent"},
		Short:   "Inspect canonical SubAgent definitions",
	}
	command.PersistentFlags().StringVar(&options.root, "root", "", "path to the Agent Manager data root")
	command.PersistentFlags().StringVar(&options.library, "library", "", "path to the Skill library used to verify references")
	command.PersistentFlags().BoolVar(&options.json, "json", false, "write machine-readable JSON")
	command.AddCommand(newSubAgentListCommand(rootOptions, options))
	command.AddCommand(newSubAgentShowCommand(rootOptions, options))
	command.AddCommand(newSubAgentValidateCommand(rootOptions, options))
	command.AddCommand(newSubAgentInstallCommand(rootOptions, options))
	command.AddCommand(newSubAgentRemoveCommand(rootOptions, options))
	return command
}

func newSubAgentInstallCommand(rootOptions *rootOptions, options *subAgentOptions) *cobra.Command {
	var project, target, conflict string
	var yes, force bool
	command := &cobra.Command{
		Use:   "install <id>",
		Short: "Install a canonical SubAgent for a target agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if target == "" {
				return errors.New("--target is required")
			}
			if conflict != "" && conflict != string(adapter.ConflictReplace) {
				return fmt.Errorf("unknown conflict strategy %q", conflict)
			}
			registry, definitions, diagnostics, err := discoverSubAgents(rootOptions, options)
			if err != nil {
				return err
			}
			definition, found := findSubAgent(args[0], definitions)
			if !found {
				return unknownSubAgentError(args[0], definitions, diagnostics)
			}
			projectPath, err := filepath.Abs(project)
			if err != nil {
				return fmt.Errorf("resolve project root: %w", err)
			}
			journal := operation.New(filepath.Join(projectPath, ".skill-manager", "journal.json"))
			planRendered := false
			var planRenderErr error
			plan, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: projectPath, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
				Target: adapter.Target(target), SourceRoot: registry.Root(), Conflict: adapter.ConflictStrategy(conflict), Force: force,
				Journal: journal, SkillReferenceChecker: registry.SkillReferenceChecker(),
				Confirm: func(confirmPlan operation.Plan) bool {
					// Render before authorizing the mutation. A broken output
					// stream is a failed confirmation, never a reason to proceed.
					if planRenderErr = writeSubAgentPlan(cmd, confirmPlan); planRenderErr != nil {
						return false
					}
					planRendered = true
					return yes
				},
			})
			if !planRendered && planRenderErr == nil {
				// Refusal can happen before confirmation (for example, an
				// unmanaged conflict without --force); still show its plan.
				if planRenderErr = writeSubAgentPlan(cmd, plan); planRenderErr != nil {
					return planRenderErr
				}
			}
			if planRenderErr != nil {
				return planRenderErr
			}
			return err
		},
	}
	command.Flags().StringVar(&project, "project", ".", "project root")
	command.Flags().StringVar(&target, "target", "", "target agent (claude-code or codex)")
	command.Flags().StringVar(&conflict, "conflict", "", "conflict strategy for existing destination paths (replace)")
	command.Flags().BoolVar(&force, "force", false, "confirm replacement of unmanaged content")
	command.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return command
}

func writeSubAgentPlan(cmd *cobra.Command, plan operation.Plan) error {
	if len(plan.Changes) == 0 && len(plan.Warnings) == 0 {
		return nil
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), plan.String())
	return err
}

func newSubAgentRemoveCommand(rootOptions *rootOptions, options *subAgentOptions) *cobra.Command {
	var project, target string
	var yes bool
	command := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove an installed SubAgent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if target == "" {
				return errors.New("--target is required")
			}
			registry, definitions, diagnostics, err := discoverSubAgents(rootOptions, options)
			if err != nil {
				return err
			}
			definition, found := findSubAgent(args[0], definitions)
			if !found {
				return unknownSubAgentError(args[0], definitions, diagnostics)
			}
			projectPath, err := filepath.Abs(project)
			if err != nil {
				return fmt.Errorf("resolve project root: %w", err)
			}
			journal := operation.New(filepath.Join(projectPath, ".skill-manager", "journal.json"))
			_, err = adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: projectPath, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
				Target: adapter.Target(target), SourceRoot: registry.Root(), Journal: journal,
				Confirm: func(plan operation.Plan) bool {
					if _, printErr := fmt.Fprint(cmd.OutOrStdout(), plan.String()); printErr != nil {
						return false
					}
					return yes
				},
			})
			return err
		},
	}
	command.Flags().StringVar(&project, "project", ".", "project root")
	command.Flags().StringVar(&target, "target", "", "target agent")
	command.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return command
}

func findSubAgent(id string, definitions []subagent.Definition) (subagent.Definition, bool) {
	for _, definition := range definitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return subagent.Definition{}, false
}

func newSubAgentListCommand(rootOptions *rootOptions, options *subAgentOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List canonical SubAgent definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, definitions, diagnostics, err := discoverSubAgents(rootOptions, options)
			if err != nil {
				return err
			}
			if options.json {
				jsonDefinitions := make([]subAgentJSONDefinition, 0, len(definitions))
				for _, definition := range definitions {
					jsonDefinitions = append(jsonDefinitions, canonicalSubAgentJSON(definition))
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(subAgentListReport{
					Definitions: jsonDefinitions,
					Diagnostics: diagnosticsJSON(diagnostics),
				})
			}
			for _, definition := range definitions {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\tcompatibility: %s\n", definition.ID, definition.Name, definition.Role, strings.Join(definition.Compatibility.Agents, ", ")); err != nil {
					return err
				}
			}
			return writeSubAgentDiagnostics(cmd, diagnostics)
		},
	}
}

func newSubAgentShowCommand(rootOptions *rootOptions, options *subAgentOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one canonical SubAgent definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, definitions, diagnostics, err := discoverSubAgents(rootOptions, options)
			if err != nil {
				return err
			}
			for _, definition := range definitions {
				if definition.ID != args[0] {
					continue
				}
				if options.json {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(canonicalSubAgentJSON(definition))
				}
				return renderSubAgentHuman(cmd, definition)
			}
			return unknownSubAgentError(args[0], definitions, diagnostics)
		},
	}
}

func newSubAgentValidateCommand(rootOptions *rootOptions, options *subAgentOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate [id]",
		Short: "Validate canonical SubAgent definitions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, definitions, diagnostics, err := discoverSubAgents(rootOptions, options)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				for _, definition := range definitions {
					if definition.ID == args[0] {
						if options.json {
							return json.NewEncoder(cmd.OutOrStdout()).Encode(subAgentValidationReport{Valid: true, Diagnostics: []subAgentDiagnosticJSON{}})
						}
						_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: valid\n", definition.ID)
						return err
					}
				}
				selected := diagnosticsForID(args[0], diagnostics)
				if len(selected) > 0 {
					if options.json {
						return json.NewEncoder(cmd.OutOrStdout()).Encode(subAgentValidationReport{Valid: false, Diagnostics: diagnosticsJSON(selected)})
					}
					return writeSubAgentDiagnostics(cmd, selected)
				}
				return unknownSubAgentError(args[0], definitions, diagnostics)
			}
			if options.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(subAgentValidationReport{Valid: len(diagnostics) == 0, Diagnostics: diagnosticsJSON(diagnostics)})
			}
			if len(diagnostics) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "all SubAgent definitions are valid")
				return err
			}
			return writeSubAgentDiagnostics(cmd, diagnostics)
		},
	}
}

func discoverSubAgents(rootOptions *rootOptions, options *subAgentOptions) (subagent.Registry, []subagent.Definition, []subagent.Diagnostic, error) {
	configured, err := config.Load(rootOptions.configPath)
	if err != nil {
		return subagent.Registry{}, nil, nil, err
	}
	library := options.library
	implicitDefaultLibrary := library == "" && rootOptions.configPath == ""
	if library == "" {
		library = configured.LibraryPath
	}
	skills, _, err := catalog.Discover(library)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && implicitDefaultLibrary {
			// A clean installation may not have a Skill library yet. Keep a
			// checker configured so referenced Skills get a precise diagnostic.
			skills = nil
		} else {
			return subagent.Registry{}, nil, nil, err
		}
	}
	checker := func(identifier string) bool {
		for _, skill := range skills {
			if skill.Identifier == identifier {
				return true
			}
		}
		return false
	}
	registry, err := registryForOptions(options, checker)
	if err != nil {
		return subagent.Registry{}, nil, nil, err
	}
	definitions, diagnostics, err := registry.Discover()
	return registry, definitions, diagnostics, err
}

func registryForOptions(options *subAgentOptions, checker subagent.SkillReferenceChecker) (subagent.Registry, error) {
	if options.root == "" {
		return subagent.NewDefaultRegistry(checker)
	}
	return subagent.NewRegistry(options.root, checker)
}

func renderSubAgentHuman(cmd *cobra.Command, definition subagent.Definition) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nversion: %s\nname: %s\nrole: %s\ninstructions: %s\nskills: %s\ncompatibility: %s\nrequired capabilities: %s\n", definition.ID, definition.Version, definition.Name, definition.Role, definition.Instructions, strings.Join(definition.Skills, ", "), strings.Join(definition.Compatibility.Agents, ", "), strings.Join(capabilityStrings(definition.RequiredCapabilities), ", "))
	return err
}

func canonicalSubAgentJSON(definition subagent.Definition) subAgentJSONDefinition {
	skills := append([]string{}, definition.Skills...)
	agents := append([]string{}, definition.Compatibility.Agents...)
	capabilities := append([]resource.Capability{}, definition.RequiredCapabilities...)
	return subAgentJSONDefinition{
		Version:              definition.Version,
		ID:                   definition.ID,
		Name:                 definition.Name,
		Role:                 definition.Role,
		Instructions:         definition.Instructions,
		Skills:               skills,
		Compatibility:        subAgentJSONCompatibility{Agents: agents},
		RequiredCapabilities: capabilities,
	}
}

func capabilityStrings(capabilities []resource.Capability) []string {
	values := make([]string, len(capabilities))
	for i, capability := range capabilities {
		values[i] = string(capability)
	}
	return values
}

func diagnosticsJSON(diagnostics []subagent.Diagnostic) []subAgentDiagnosticJSON {
	values := make([]subAgentDiagnosticJSON, len(diagnostics))
	for i, diagnostic := range diagnostics {
		values[i] = subAgentDiagnosticJSON{ID: diagnostic.ID, Path: diagnostic.Path, Message: diagnostic.Message}
	}
	return values
}

func writeSubAgentDiagnostics(cmd *cobra.Command, diagnostics []subagent.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "diagnostic:", diagnostic.Error()); err != nil {
			return err
		}
	}
	return nil
}

func unknownSubAgentError(id string, definitions []subagent.Definition, diagnostics []subagent.Diagnostic) error {
	ids := make([]string, len(definitions))
	for i, definition := range definitions {
		ids[i] = definition.ID
	}
	sort.Strings(ids)
	message := fmt.Sprintf("SubAgent %q not found; available: %s", id, strings.Join(ids, ", "))
	if len(diagnostics) > 0 {
		message += "; run 'subagents validate' for invalid definitions"
	}
	return errors.New(message)
}

func diagnosticsForID(id string, diagnostics []subagent.Diagnostic) []subagent.Diagnostic {
	selected := make([]subagent.Diagnostic, 0)
	for _, diagnostic := range diagnostics {
		base := filepath.Base(diagnostic.Path)
		if diagnostic.ID == id || strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml") == id {
			selected = append(selected, diagnostic)
		}
	}
	return selected
}
