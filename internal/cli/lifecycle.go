package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/diagnostic"
	"github.com/AllenMuu/skill-manager/internal/initcmd"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/search"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "init", Short: "Install the minimal global Operator skill", RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		journal := filepath.Join(home, ".skill-manager", "journal.json")
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		var previewErr error
		svc := initcmd.New(home, journal, confirmer(cmd, &yes, &previewErr), initcmd.VerifyCLI(exe))
		_, err = svc.Initialize()
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}

func newProjectCommands(options *rootOptions) []*cobra.Command {
	return []*cobra.Command{newSelectCommand(options), newAddCommand(options), newListCommand(options), newRemoveCommand(options), newAdoptCommand(options), newForkCommand(options), newDoctorCommand(options), newReconcileCommand(options), newUndoCommand(options), newDeleteCommand(options)}
}
func newSelectCommand(options *rootOptions) *cobra.Command {
	var project string
	cmd := &cobra.Command{Use: "select", Short: "Interactively select skills to activate", RunE: func(cmd *cobra.Command, _ []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		skills, _, err := catalog.Discover(lib)
		if err != nil {
			return err
		}
		in := bufio.NewScanner(cmd.InOrStdin())
		if _, err := fmt.Fprint(cmd.OutOrStdout(), "Search query: "); err != nil {
			return err
		}
		if !in.Scan() {
			if err := in.Err(); err != nil {
				return err
			}
			return fmt.Errorf("selection cancelled")
		}
		matches := search.Match(in.Text(), skills)
		for _, skill := range matches {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", skill.Identifier, skill.Description); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), "Skills (comma-separated): "); err != nil {
			return err
		}
		if !in.Scan() {
			if err := in.Err(); err != nil {
				return err
			}
			return fmt.Errorf("selection cancelled")
		}
		ids := strings.Split(in.Text(), ",")
		if _, err := fmt.Fprint(cmd.OutOrStdout(), "Targets (comma-separated): "); err != nil {
			return err
		}
		if !in.Scan() {
			if err := in.Err(); err != nil {
				return err
			}
			return fmt.Errorf("selection cancelled")
		}
		targets, err := parseTargets(strings.Split(in.Text(), ","))
		if err != nil {
			return err
		}
		selected := make([]catalog.Skill, 0, len(ids))
		for _, id := range ids {
			var skill catalog.Skill
			found := false
			for _, candidate := range matches {
				if candidate.Identifier == strings.TrimSpace(id) {
					skill = candidate
					found = true
				}
			}
			if !found {
				return fmt.Errorf("selected skill %q was not found", id)
			}
			selected = append(selected, skill)
		}
		var previewErr error
		confirm := func(plan operation.Plan) bool {
			if _, previewErr = fmt.Fprint(cmd.OutOrStdout(), plan.String()); previewErr != nil {
				return false
			}
			if _, previewErr = fmt.Fprint(cmd.OutOrStdout(), "Confirm [y/N]: "); previewErr != nil {
				return false
			}
			if !in.Scan() {
				if err := in.Err(); err != nil {
					previewErr = err
				}
				return false
			}
			return strings.ToLower(strings.TrimSpace(in.Text())) == "y"
		}
		_, err = lifecycle.New(lib, journal(project), confirm).AddMany(project, selected, targets)
		if previewErr != nil {
			return previewErr
		}
		if err != nil {
			return err
		}
		return nil
	}}
	projectFlag(cmd, &project)
	return cmd
}
func projectFlag(cmd *cobra.Command, project *string) {
	cmd.Flags().StringVarP(project, "project", "p", ".", "project root")
}
func library(options *rootOptions) (string, error) {
	c, err := config.Load(options.configPath)
	if err != nil {
		return "", err
	}
	return c.LibraryPath, nil
}
func journal(project string) *operation.Journal {
	return operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
}
func confirmer(cmd *cobra.Command, yes *bool, outputErr *error) func(operation.Plan) bool {
	return func(p operation.Plan) bool {
		_, err := fmt.Fprint(cmd.OutOrStdout(), p.String())
		if err != nil {
			*outputErr = err
			return false
		}
		return *yes
	}
}
func parseTargets(values []string) ([]adapter.Target, error) {
	out := make([]adapter.Target, len(values))
	for i, v := range values {
		target := adapter.Target(v)
		if _, ok := adapter.For(target); !ok {
			return nil, fmt.Errorf("unsupported target %q", v)
		}
		out[i] = target
	}
	return out, nil
}
func newAddCommand(options *rootOptions) *cobra.Command {
	var project string
	var targets []string
	var allDetected bool
	var yes bool
	var conflict string
	var force bool
	cmd := &cobra.Command{Use: "add <skill>", Short: "Activate a library skill for selected target agents", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(targets) == 0 && !allDetected {
			return fmt.Errorf("at least one --target is required (or use --all-detected)")
		}
		if allDetected {
			targets = detectedTargets(project)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no target agents detected")
		}
		if conflict != "" && conflict != string(lifecycle.ConflictReplace) {
			return fmt.Errorf("unknown conflict strategy %q (available: replace)", conflict)
		}
		if conflict != "" && !force {
			return fmt.Errorf("conflict strategy %q requires --force confirmation", conflict)
		}
		opts := lifecycle.Options{Conflict: lifecycle.ConflictStrategy(conflict), Force: force}
		lib, err := library(options)
		if err != nil {
			return err
		}
		skills, _, err := catalog.Discover(lib)
		if err != nil {
			return err
		}
		var skill catalog.Skill
		found := false
		for _, s := range skills {
			if s.Identifier == args[0] {
				skill = s
				found = true
			}
		}
		if !found {
			return fmt.Errorf("skill %q not found", args[0])
		}
		ts, err := parseTargets(targets)
		if err != nil {
			return err
		}
		var previewErr error
		confirm := func(plan operation.Plan) bool {
			_, previewErr = fmt.Fprint(cmd.OutOrStdout(), plan.String())
			return previewErr == nil && yes
		}
		_, err = lifecycle.New(lib, journal(project), confirm).Add(project, skill, ts, opts)
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().StringSliceVar(&targets, "target", nil, "target agent (claude-code or codex)")
	cmd.Flags().BoolVar(&allDetected, "all-detected", false, "activate for every detected supported target agent")
	cmd.Flags().StringVar(&conflict, "conflict", "", "conflict strategy for existing destination paths (replace)")
	cmd.Flags().BoolVar(&force, "force", false, "supply force confirmation for the selected conflict strategy")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}
func detectedTargets(project string) []string {
	var targets []string
	for _, a := range adapter.Supported() {
		root := filepath.Dir(a.ProjectSkillPath(project, "placeholder"))
		if _, err := os.Stat(root); err == nil {
			targets = append(targets, string(a.Target()))
		}
	}
	return targets
}
func newListCommand(options *rootOptions) *cobra.Command {
	var project string
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Short: "List project skills", RunE: func(cmd *cobra.Command, _ []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		items, err := lifecycle.New(lib, journal(project), nil).List(project)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, i := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", i.Target, i.Identifier, i.Status); err != nil {
				return err
			}
		}
		return nil
	}}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	return cmd
}
func newRemoveCommand(options *rootOptions) *cobra.Command {
	var project, target string
	var yes bool
	cmd := &cobra.Command{Use: "remove <skill>", Short: "Remove a managed project link", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		var previewErr error
		_, err = lifecycle.New(lib, journal(project), confirmer(cmd, &yes, &previewErr)).Remove(project, adapter.Target(target), args[0])
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().StringVar(&target, "target", "", "target agent")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}
func newAdoptCommand(options *rootOptions) *cobra.Command {
	var project, target string
	var yes bool
	cmd := &cobra.Command{Use: "adopt <skill>", Short: "Adopt an eligible project skill into the library", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		var previewErr error
		_, err = lifecycle.New(lib, journal(project), confirmer(cmd, &yes, &previewErr)).Adopt(project, adapter.Target(target), args[0])
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().StringVar(&target, "target", "", "target agent")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}
func newForkCommand(options *rootOptions) *cobra.Command {
	var project, target string
	var yes bool
	cmd := &cobra.Command{Use: "fork <skill>", Short: "Fork a managed link into an independent project skill", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		var previewErr error
		_, err = lifecycle.New(lib, journal(project), confirmer(cmd, &yes, &previewErr)).Fork(project, adapter.Target(target), args[0])
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().StringVar(&target, "target", "", "target agent")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}
func newDoctorCommand(options *rootOptions) *cobra.Command {
	var project string
	var updateGitignore, yes bool
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose catalog and project activation", RunE: func(cmd *cobra.Command, _ []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		findings, err := diagnostic.Scan(lib, project)
		if err != nil {
			return err
		}
		for _, f := range findings {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", f.Path, f.Message); err != nil {
				return err
			}
		}
		if updateGitignore {
			var paths []string
			for _, f := range findings {
				if strings.Contains(f.Message, "managed absolute link is ") && !strings.Contains(f.Message, " is ignored;") {
					paths = append(paths, f.Path)
				}
			}
			if len(paths) > 0 {
				var previewErr error
				_, err = diagnostic.AddGitignore(project, paths, confirmer(cmd, &yes, &previewErr), journal(project))
				if previewErr != nil {
					return previewErr
				}
				if err != nil {
					return err
				}
			}
		}
		return nil
	}}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&updateGitignore, "update-gitignore", false, "add exact managed links to .gitignore after confirmation")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}
func newReconcileCommand(options *rootOptions) *cobra.Command {
	var project string
	var yes bool
	cmd := &cobra.Command{Use: "reconcile", Short: "Repair orphaned managed links", RunE: func(cmd *cobra.Command, _ []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		var previewErr error
		_, err = diagnostic.Reconcile(lib, project, journal(project), confirmer(cmd, &yes, &previewErr))
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}
func newUndoCommand(options *rootOptions) *cobra.Command {
	var project string
	var yes bool
	cmd := &cobra.Command{Use: "undo", Short: "Undo the latest project operation", RunE: func(cmd *cobra.Command, _ []string) error {
		var previewErr error
		err := lifecycle.New("", journal(project), confirmer(cmd, &yes, &previewErr)).Undo()
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}
func newDeleteCommand(options *rootOptions) *cobra.Command {
	var force, yes bool
	cmd := &cobra.Command{Use: "delete <skill>", Short: "Delete an eligible library skill", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		lib, err := library(options)
		if err != nil {
			return err
		}
		var previewErr error
		_, err = diagnostic.DeleteLibrarySkill(lib, args[0], force, confirmer(cmd, &yes, &previewErr), journal("."))
		if previewErr != nil {
			return previewErr
		}
		return err
	}}
	cmd.Flags().BoolVar(&force, "force", false, "allow library deletion")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed plan")
	return cmd
}
