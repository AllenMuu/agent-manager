package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/spf13/cobra"
)

type memoryOwnerOptions struct{ project, user, registry, agent, session string }

func (o *memoryOwnerOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.project, "project", "", "registered project directory")
	cmd.Flags().StringVar(&o.user, "user", "", "explicit operator user identity")
	cmd.Flags().StringVar(&o.registry, "registry", "", "explicit project registry file (default: config path + .projects.json)")
	cmd.Flags().StringVar(&o.agent, "agent-id", "", "AGENT partition within the selected owner")
	cmd.Flags().StringVar(&o.session, "session-id", "", "SESSION partition within the selected owner")
}
func projectRegistry(rootOptions *rootOptions, path string) (*memory.ProjectRegistry, error) {
	if path == "" {
		if rootOptions.configPath == "" {
			return nil, fmt.Errorf("--config or --registry is required for project registration")
		}
		path = rootOptions.configPath + ".projects.json"
	}
	roots := []string{}
	loaded, loadErr := config.Load(rootOptions.configPath)
	if loadErr != nil {
		return nil, fmt.Errorf("invalid Memory configuration")
	}
	if loaded.Memory != nil && loaded.Memory.Provider == memory.StructuredLocalProviderID && loaded.Memory.Configuration.Kind == "file" {
		roots = append(roots, loaded.Memory.Configuration.Name)
	}
	registry, err := memory.OpenProjectRegistry(path, roots...)
	return registry, memory.SafeError(err)
}
func (o memoryOwnerOptions) owner(rootOptions *rootOptions) (memory.Owner, error) {
	if (o.project == "") == (o.user == "") {
		return memory.Owner{}, fmt.Errorf("select exactly one explicit --project or --user context")
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: o.user}
	if o.project != "" {
		registry, err := projectRegistry(rootOptions, o.registry)
		if err != nil {
			return memory.Owner{}, err
		}
		identity, err := registry.Lookup(o.project)
		if err != nil {
			return memory.Owner{}, memory.SafeError(err)
		}
		owner = memory.Owner{Kind: memory.OwnerProject, ProjectID: identity.ID}
	}
	if o.agent != "" && o.session != "" {
		return memory.Owner{}, fmt.Errorf("select at most one AGENT or SESSION partition")
	}
	if o.agent != "" {
		owner.Kind = memory.OwnerAgent
		owner.AgentID = o.agent
	}
	if o.session != "" {
		owner.Kind = memory.OwnerSession
		owner.SessionID = o.session
	}
	return owner, nil
}
func configuredGateway(rootOptions *rootOptions, o memoryOwnerOptions, write bool) (*memory.Gateway, memory.Owner, error) {
	owner, err := o.owner(rootOptions)
	if err != nil {
		return nil, owner, err
	}
	loaded, err := config.Load(rootOptions.configPath)
	if err != nil {
		return nil, owner, fmt.Errorf("invalid Memory configuration")
	}
	if loaded.Memory == nil {
		return nil, owner, memory.ErrUnavailable
	}
	scope := memory.ScopeUser
	if owner.ProjectID != "" {
		scope = memory.ScopeProject
	}
	if !containsMemoryScope(loaded.Memory.Scopes, scope) {
		return nil, owner, memory.ErrUnsupported
	}
	access := memory.Access{ReadOwners: []memory.Owner{owner}}
	if write {
		access.WriteOwners = []memory.Owner{owner}
	}
	provider, err := memory.OpenConfiguredProvider(*loaded.Memory)
	if err != nil && !errors.Is(err, memory.ErrUnavailable) {
		return nil, owner, memory.SafeError(err)
	}
	// An unavailable Memory path remains a Gateway failure so context can retain
	// independent artifacts and Skills with a safe diagnostic.
	if err != nil {
		provider = nil
	}
	gateway, err := memory.NewGateway(provider, access, loaded.Memory.Retrieval)
	return gateway, owner, err
}
func confirmMemoryPlan(cmd *cobra.Command, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Confirm exact owner and intent [y/N]: "); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	return strings.EqualFold(strings.TrimSpace(scanner.Text()), "y"), nil
}
func newMemoryProjectCommand(rootOptions *rootOptions) *cobra.Command {
	parent := &cobra.Command{Use: "project", Short: "Explicitly map stable Memory project identities"}
	for _, operation := range []string{"register", "relocate"} {
		var project, registryPath string
		var yes bool
		use := operation
		argsPolicy := cobra.NoArgs
		if operation == "relocate" {
			use += " <project-id>"
			argsPolicy = cobra.ExactArgs(1)
		}
		cmd := &cobra.Command{Use: use, Short: "Preview and confirm an exact project directory mapping", Args: argsPolicy, RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := projectRegistry(rootOptions, registryPath)
			if err != nil {
				return err
			}
			var preview memory.ProjectMappingPreview
			if operation == "register" {
				preview, err = registry.PreviewRegister(cmd.Context(), project)
			} else {
				preview, err = registry.PreviewRelocate(cmd.Context(), args[0], project)
			}
			if err != nil {
				return memory.SafeError(err)
			}
			if _, err = fmt.Fprintln(cmd.OutOrStdout(), "Memory project mapping plan"); err != nil {
				return err
			}
			if err = json.NewEncoder(cmd.OutOrStdout()).Encode(preview.Plan()); err != nil {
				return err
			}
			confirmed, err := confirmMemoryPlan(cmd, yes)
			if err != nil || !confirmed {
				return err
			}
			identity, err := registry.CommitMapping(cmd.Context(), preview, true)
			if err != nil {
				return memory.SafeError(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(identity)
		}}
		cmd.Flags().StringVar(&project, "project", "", "existing project directory")
		cmd.Flags().StringVar(&registryPath, "registry", "", "project registry file")
		cmd.Flags().BoolVar(&yes, "yes", false, "confirm displayed exact mapping")
		_ = cmd.MarkFlagRequired("project")
		parent.AddCommand(cmd)
	}
	return parent
}

func newStructuredMutationCommand(rootOptions *rootOptions, operation memory.Operation) *cobra.Command {
	var o memoryOwnerOptions
	var kind, content, source, layer, operationID string
	var evidence []string
	var yes bool
	var expectedVersion uint64
	use := string(operation)
	argsPolicy := cobra.NoArgs
	if operation != memory.OperationAdd {
		use += " <record-id>"
		argsPolicy = cobra.ExactArgs(1)
	}
	if operation == memory.OperationImport {
		use = "import <legacy-file>"
	}
	cmd := &cobra.Command{Use: use, Short: "Preview and confirm owned canonical Memory", Args: argsPolicy, RunE: func(cmd *cobra.Command, args []string) error {
		gateway, owner, err := configuredGateway(rootOptions, o, true)
		if err != nil {
			return err
		}
		id := memory.RecordID("")
		if len(args) > 0 {
			id = memory.RecordID(args[0])
		}
		mutation := memory.Mutation{Operation: operation, ID: id, ExpectedVersion: expectedVersion, Record: memory.NewRecord{Owner: owner, Type: memory.KnowledgeType(kind), Content: content, Source: source, Evidence: evidence, Layer: memory.Layer(layer)}, OperationID: operationID}
		if operation == memory.OperationImport {
			mutation.ID = ""
			mutation.Import = memory.LegacyImportRequest{Path: args[0], Owner: owner, Source: source, Type: memory.KnowledgeType(kind), Layer: memory.Layer(layer), OperationID: operationID}
		}
		preview, err := gateway.Preview(cmd.Context(), mutation)
		if err != nil {
			return memory.SafeError(err)
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), "Memory mutation plan (provider lifecycle; no Skill filesystem undo)"); err != nil {
			return err
		}
		if err = json.NewEncoder(cmd.OutOrStdout()).Encode(preview.Plan()); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		records, err := gateway.Commit(cmd.Context(), preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner, Source: source})
		if err != nil {
			return memory.SafeError(err)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(records)
	}}
	o.flags(cmd)
	if operation != memory.OperationAdd && operation != memory.OperationImport {
		cmd.Flags().Uint64Var(&expectedVersion, "expected-version", 0, "exact active version to condition the mutation on")
		_ = cmd.MarkFlagRequired("expected-version")
	}
	cmd.Flags().StringVar(&kind, "type", "", "canonical knowledge type")
	cmd.Flags().StringVar(&content, "content", "", "inert knowledge content")
	cmd.Flags().StringVar(&source, "source", "", "declared source attribution")
	cmd.Flags().StringSliceVar(&evidence, "evidence", nil, "evidence reference (repeatable)")
	cmd.Flags().StringVar(&layer, "layer", "RAW", "knowledge layer")
	cmd.Flags().StringVar(&operationID, "operation-id", "", "safe receipt retry identity")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm displayed exact owner and intent")
	return cmd
}
func newStructuredSearchCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var kind string
	cmd := &cobra.Command{Use: "search [query]", Short: "Read bounded current attributed Memory", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		gateway, owner, err := configuredGateway(rootOptions, o, false)
		if err != nil {
			return err
		}
		text := ""
		if len(args) > 0 {
			text = args[0]
		}
		records, err := gateway.Search(cmd.Context(), memory.SearchRequest{Owner: owner, Text: text, Type: memory.KnowledgeType(kind)})
		if err != nil {
			return memory.SafeError(err)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(records)
	}}
	o.flags(cmd)
	cmd.Flags().StringVar(&kind, "type", "", "optional knowledge type filter")
	return cmd
}

func newStructuredInspectCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var history bool
	cmd := &cobra.Command{Use: "inspect <record-id>", Short: "Explicitly inspect canonical records or retired lineage", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		gateway, owner, err := configuredGateway(rootOptions, o, false)
		if err != nil {
			return err
		}
		if history {
			records, err := gateway.History(cmd.Context(), owner, memory.RecordID(args[0]))
			if err != nil {
				return memory.SafeError(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(records)
		}
		record, err := gateway.Get(cmd.Context(), owner, memory.RecordID(args[0]))
		if err != nil {
			return memory.SafeError(err)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(record)
	}}
	o.flags(cmd)
	cmd.Flags().BoolVar(&history, "history", false, "read explicit record lineage, including retired versions")
	return cmd
}
