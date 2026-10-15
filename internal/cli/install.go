package cli

import (
	"fmt"
	"os"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newInstallCommand(options *rootOptions) *cobra.Command {
	var project string
	var global bool
	var targets []string
	var versions []string
	var allDetected bool
	var yes bool
	var conflict string
	var force bool
	cmd := &cobra.Command{
		Use:   "install <skill-id>...",
		Short: "Install selected library Skills into a project or globally",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if global && cmd.Flags().Changed("project") {
				return fmt.Errorf("--global cannot be combined with --project")
			}
			if global {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				project = home
			}
			if len(targets) == 0 && !allDetected {
				return fmt.Errorf("at least one --target is required (or use --all-detected)")
			}
			if conflict != "" && conflict != string(lifecycle.ConflictReplace) {
				return fmt.Errorf("unknown conflict strategy %q (available: replace)", conflict)
			}
			if conflict != "" && !force {
				return fmt.Errorf("conflict strategy %q requires --force confirmation", conflict)
			}
			if allDetected {
				if global {
					targets = detectedGlobalTargets(project)
				} else {
					targets = detectedTargets(project)
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("no target agents detected")
			}
			parsedTargets, err := parseTargets(targets)
			if err != nil {
				return err
			}
			selectedVersions, err := parseSkillVersionOverrides(versions)
			if err != nil {
				return err
			}
			settings, err := config.Load(options.configPath)
			if err != nil {
				return err
			}
			service := installation.NewWithMaterialization(settings.LibraryPath, adapter.MaterializationMode(settings.SyncMode))
			preview, previewErr := service.Preview("cli", installation.Request{
				Project:  project,
				Global:   global,
				SkillIDs: args,
				Versions: selectedVersions,
				Targets:  parsedTargets,
				Options: lifecycle.Options{
					Conflict: lifecycle.ConflictStrategy(conflict),
					Force:    force,
				},
			})
			if len(preview.Plan.Changes) > 0 || len(preview.Plan.Warnings) > 0 {
				if _, err := fmt.Fprint(cmd.OutOrStdout(), preview.Plan.String()); err != nil {
					return fmt.Errorf("write installation preview: %w", err)
				}
			}
			if previewErr != nil {
				return previewErr
			}
			if !yes {
				return lifecycle.ErrNotConfirmed
			}
			result, err := service.Apply("cli", preview.ID)
			if err != nil {
				return err
			}
			if result.Stale {
				return lifecycle.ErrPlanChanged
			}
			return nil
		},
	}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&global, "global", false, "install into the selected agents' global Skill directories")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "target agent (claude-code, codex, or pi)")
	cmd.Flags().StringArrayVar(&versions, "version", nil, "select a Skill version as <skill-id>=<version-or-key>; repeat as needed")
	cmd.Flags().BoolVar(&allDetected, "all-detected", false, "install for every detected supported target agent")
	cmd.Flags().StringVar(&conflict, "conflict", "", "conflict strategy for existing destination paths (replace)")
	cmd.Flags().BoolVar(&force, "force", false, "supply force confirmation for the selected conflict strategy")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm and apply the displayed plan")
	return cmd
}
