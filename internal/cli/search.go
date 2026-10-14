package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/search"
	"github.com/spf13/cobra"
)

type searchOutput struct {
	Identifier          string                      `json:"identifier"`
	Description         string                      `json:"description"`
	Tags                []string                    `json:"tags"`
	Compatibility       []string                    `json:"compatibility"`
	SupportedLanguages  []string                    `json:"supportedLanguages,omitempty"`
	SupportedFrameworks []string                    `json:"supportedFrameworks,omitempty"`
	SupportedAgents     []string                    `json:"supportedAgents,omitempty"`
	Version             string                      `json:"version"`
	VersionKey          string                      `json:"versionKey"`
	Versions            []resource.SkillVersionInfo `json:"versions"`
	Source              string                      `json:"source,omitempty"`
	SourceType          string                      `json:"sourceType,omitempty"`
	Publisher           string                      `json:"publisher,omitempty"`
	ImmutableRef        string                      `json:"immutableRef,omitempty"`
	Checksum            string                      `json:"checksum,omitempty"`
	TrustLevel          string                      `json:"trustLevel,omitempty"`
	VerificationStatus  string                      `json:"verificationStatus,omitempty"`
	LegacyProvenance    string                      `json:"legacyProvenance,omitempty"`
}

func newSearchCommand(options *rootOptions) *cobra.Command {
	var libraryPath string
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the local skill catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configured, err := config.Load(options.configPath)
			if err != nil {
				return err
			}
			if libraryPath != "" {
				configured.LibraryPath = libraryPath
			}

			skills, _, err := catalog.Discover(configured.LibraryPath)
			if err != nil {
				return err
			}
			matches := search.Match(args[0], skills)
			if jsonOutput {
				return renderSearchJSON(cmd, matches)
			}
			return renderSearchHuman(cmd, matches)
		},
	}
	command.Flags().StringVar(&libraryPath, "library", "", "path to skill library")
	command.Flags().BoolVar(&jsonOutput, "json", false, "render machine-readable JSON")
	return command
}

func renderSearchJSON(cmd *cobra.Command, skills []catalog.Skill) error {
	results := make([]searchOutput, 0, len(skills))
	for _, skill := range skills {
		results = append(results, searchOutput{
			Identifier:          skill.Identifier,
			Description:         skill.Description,
			Tags:                nonNilStrings(skill.Tags),
			Compatibility:       nonNilStrings(skill.Compatibility),
			SupportedLanguages:  nonNilStrings(skill.SupportedLanguages),
			SupportedFrameworks: nonNilStrings(skill.SupportedFrameworks),
			SupportedAgents:     nonNilStrings(skill.SupportedAgents),
			Version:             skill.Version,
			VersionKey:          skill.VersionKey,
			Versions:            nonNilVersions(skill.Versions),
			Source:              skill.Source,
			SourceType:          skill.SourceType,
			Publisher:           skill.Publisher,
			ImmutableRef:        skill.ImmutableRef,
			Checksum:            skill.Checksum,
			TrustLevel:          skill.TrustLevel,
			VerificationStatus:  skill.VerificationStatus,
			LegacyProvenance:    skill.Provenance,
		})
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	return encoder.Encode(results)
}

func renderSearchHuman(cmd *cobra.Command, skills []catalog.Skill) error {
	for _, skill := range skills {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tversion: %s (key: %s)\ttags: %s\tcompatibility: %s\tsource: %s\ttype: %s\tpublisher: %s\tref: %s\tchecksum: %s\ttrust: %s\tverification: %s\n", skill.Identifier, skill.Description, printable(skill.Version), printable(skill.VersionKey), strings.Join(skill.Tags, ", "), strings.Join(skill.Compatibility, ", "), printable(skill.Source), printable(skill.SourceType), printable(skill.Publisher), printable(skill.ImmutableRef), printable(skill.Checksum), printable(skill.TrustLevel), printable(skill.VerificationStatus)); err != nil {
			return err
		}
	}
	return nil
}

func nonNilVersions(values []resource.SkillVersionInfo) []resource.SkillVersionInfo {
	if values == nil {
		return []resource.SkillVersionInfo{}
	}
	return values
}

func printable(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
