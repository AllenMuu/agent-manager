package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/skillimport"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"github.com/spf13/cobra"
)

type skillImportOutput struct {
	Preview  skillimport.Preview `json:"preview"`
	Imported []skillrepo.Entry   `json:"imported,omitempty"`
}

type skillVersionsOutput struct {
	Identifier        string                      `json:"identifier"`
	CurrentVersionKey string                      `json:"currentVersionKey"`
	Versions          []skillrepo.VersionMetadata `json:"versions"`
}

func newSkillsCommand(options *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "skill", Short: "Inspect and import Skills in the local repository"}
	command.AddCommand(newSkillImportCommand(options), newSkillVersionsCommand(options))
	return command
}

func newSkillImportCommand(options *rootOptions) *cobra.Command {
	var legacyRoot string
	var confirmToken string
	var yes bool
	var asJSON bool
	command := &cobra.Command{
		Use:   "import [skill-id...]",
		Short: "Preview or explicitly import selected Skills from the legacy library",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if yes && len(args) == 0 {
				return fmt.Errorf("--yes requires one or more explicit Skill identifiers")
			}
			configured, err := config.Load(options.configPath)
			if err != nil {
				return err
			}
			if legacyRoot == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				legacyRoot = filepath.Join(home, ".agents", "skills")
			}
			service, err := skillimport.New(configured.LibraryPath, legacyRoot)
			if err != nil {
				return err
			}
			preview, err := service.Preview(args)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return renderSkillImportOutput(cmd, preview, nil, asJSON)
			}
			for _, entry := range preview.Entries {
				if entry.Conflict {
					if err := renderSkillImportOutput(cmd, preview, nil, asJSON); err != nil {
						return err
					}
					return fmt.Errorf("%w: %s", skillimport.ErrConflict, entry.Identifier)
				}
				if !entry.Eligible {
					if err := renderSkillImportOutput(cmd, preview, nil, asJSON); err != nil {
						return err
					}
					return fmt.Errorf("%w: %s: %s", skillimport.ErrInvalidSelection, entry.Identifier, entry.Problem)
				}
			}
			if !yes || confirmToken == "" {
				if err := renderSkillImportOutput(cmd, preview, nil, asJSON); err != nil {
					return err
				}
				if !asJSON {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Preview only; repeat with the listed identifiers, --confirm-token <token>, and --yes to copy the selected batch."); err != nil {
						return err
					}
				}
				return lifecycle.ErrNotConfirmed
			}
			if confirmToken != preview.ConfirmationToken {
				if err := renderSkillImportOutput(cmd, preview, nil, asJSON); err != nil {
					return err
				}
				return skillimport.ErrStalePreview
			}
			imported, err := service.Import(preview)
			if err != nil {
				return err
			}
			return renderSkillImportOutput(cmd, preview, imported, asJSON)
		},
	}
	command.Flags().StringVar(&legacyRoot, "source", "", "legacy Skill library (default: ~/.agents/skills)")
	command.Flags().StringVar(&confirmToken, "confirm-token", "", "confirmation token from the displayed import preview")
	command.Flags().BoolVar(&yes, "yes", false, "apply the displayed batch when paired with its matching --confirm-token")
	command.Flags().BoolVar(&asJSON, "json", false, "render the preview and result as JSON")
	return command
}

func renderSkillImportOutput(cmd *cobra.Command, preview skillimport.Preview, imported []skillrepo.Entry, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(skillImportOutput{Preview: preview, Imported: imported})
	}
	if err := renderSkillImportPreview(cmd, preview); err != nil {
		return err
	}
	if imported != nil {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Imported %d selected Skill(s). Legacy sources and project links were left unchanged.\n", len(imported))
		return err
	}
	return nil
}

func renderSkillImportPreview(cmd *cobra.Command, preview skillimport.Preview) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Legacy library: %s\nRepository: %s\n", preview.SourceRoot, preview.DestinationRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Confirmation token: %s\n", preview.ConfirmationToken); err != nil {
		return err
	}
	if len(preview.Entries) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No eligible Skills found."); err != nil {
			return err
		}
	}
	for _, entry := range preview.Entries {
		state := "ready"
		if !entry.Eligible {
			state = "ineligible: " + entry.Problem
		} else if entry.Conflict {
			state = "conflict"
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tversion: unknown\t%s\n", entry.Identifier, entry.Destination, state); err != nil {
			return err
		}
		if len(entry.MetadataGaps) > 0 {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  metadata gaps: %s\n", strings.Join(entry.MetadataGaps, ", ")); err != nil {
				return err
			}
		}
	}
	for _, diagnostic := range preview.Diagnostics {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "skip %s: %s\n", diagnostic.Path, diagnostic.Message); err != nil {
			return err
		}
	}
	return nil
}

func newSkillVersionsCommand(options *rootOptions) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "versions <skill-id>",
		Short: "List stored versions and provenance for one Skill identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configured, err := config.Load(options.configPath)
			if err != nil {
				return err
			}
			repository, err := skillrepo.Open(configured.LibraryPath)
			if err != nil {
				return err
			}
			current, err := repository.Resolve(args[0], "")
			if err != nil {
				return err
			}
			versions, err := repository.Versions(args[0])
			if err != nil {
				return err
			}
			output := skillVersionsOutput{Identifier: args[0], CurrentVersionKey: current.Key, Versions: make([]skillrepo.VersionMetadata, 0, len(versions))}
			for _, version := range versions {
				output.Versions = append(output.Versions, version.Metadata)
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
			}
			for _, version := range versions {
				declared := version.Version
				if declared == "" {
					declared = "unknown"
				}
				currentMark := ""
				if version.Key == current.Key {
					currentMark = " (current)"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\tkey: %s%s\tsource: %s\tsourceType: %s\tpublisher: %s\tref: %s\tchecksum: %s\ttrust: %s\tverification: %s\n", declared, version.Key, currentMark, unknown(version.Metadata.Source), unknown(version.Metadata.SourceType), unknown(version.Metadata.Publisher), unknown(version.Metadata.ImmutableRef), unknown(version.Metadata.Checksum), unknown(version.Metadata.TrustLevel), unknown(version.Metadata.VerificationStatus)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "render machine-readable JSON")
	return command
}

func unknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func parseSkillVersionOverrides(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	versions := make(map[string]string, len(values))
	for _, value := range values {
		identifier, version, ok := strings.Cut(value, "=")
		if !ok || identifier == "" || version == "" || strings.TrimSpace(identifier) != identifier || strings.TrimSpace(version) != version {
			return nil, fmt.Errorf("invalid --version %q; use <skill-id>=<version-or-key>", value)
		}
		if _, exists := versions[identifier]; exists {
			return nil, fmt.Errorf("duplicate --version selection for Skill %q", identifier)
		}
		versions[identifier] = version
	}
	return versions, nil
}
