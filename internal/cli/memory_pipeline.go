package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memorypipeline"
	"github.com/AllenMuu/skill-manager/internal/run"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"github.com/spf13/cobra"
)

func newMemoryPipelineCommand(rootOptions *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "pipeline", Short: "Manage local derived memory from completed run evidence"}
	command.AddCommand(newMemoryPipelineInitCommand(rootOptions))
	command.AddCommand(newMemoryPipelineImportCommand(rootOptions))
	command.AddCommand(newMemoryPipelineExtractCommand(rootOptions))
	command.AddCommand(newMemoryPipelineConsolidateCommand(rootOptions))
	command.AddCommand(newMemoryPipelineSearchCommand(rootOptions))
	command.AddCommand(newMemoryPipelineSummaryCommand(rootOptions))
	command.AddCommand(newMemoryPipelineEvidenceCommand(rootOptions))
	command.AddCommand(newMemoryPipelineProposalCommand(rootOptions))
	command.AddCommand(newMemoryPipelineCandidateCommand(rootOptions))
	return command
}

func newMemoryPipelineCandidateCommand(rootOptions *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "candidates", Short: "Detect and review explicitly validated Skill candidates"}
	command.AddCommand(newMemoryPipelineCandidateDetectCommand(rootOptions))
	command.AddCommand(newMemoryPipelineCandidateClaimCommand(rootOptions))
	command.AddCommand(newMemoryPipelineCandidateListCommand(rootOptions))
	command.AddCommand(newMemoryPipelineCandidateReviewCommand(rootOptions))
	command.AddCommand(newMemoryPipelineCandidateConvertCommand(rootOptions))
	return command
}

func newMemoryPipelineCandidateClaimCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	var key, workflow, outcome string
	var generalizable, yes bool
	cmd := &cobra.Command{Use: "claim <evidence-id>", Short: "Record an explicitly confirmed procedure claim against one imported run", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		ref, err := store.Evidence(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		evidence, err := provider.Read(cmd.Context(), ref)
		if err != nil {
			return err
		}
		computed, err := memorypipeline.ReferenceFor(evidence)
		if err != nil || computed.ID != ref.ID || computed.ContentHash != ref.ContentHash {
			return memorypipeline.ErrEvidenceChanged
		}
		procedure := memorypipeline.ProcedureEvidence{Key: key, Content: workflow, Outcome: outcome, Generalizable: generalizable}
		procedure, err = memorypipeline.NormalizeProcedureClaim(procedure)
		if err != nil {
			return err
		}
		if procedure.Outcome != memorypipeline.ProcedureOutcomeFailed && evidence.Status != string(run.Completed) {
			return fmt.Errorf("successful or verified procedure claims require a completed source run")
		}
		preview := struct {
			ProjectID string                           `json:"projectId"`
			Evidence  memorypipeline.RunEvidenceRef    `json:"evidence"`
			Procedure memorypipeline.ProcedureEvidence `json:"procedure"`
			Origin    string                           `json:"origin"`
			Effect    string                           `json:"effect"`
		}{owner.ProjectID, ref, procedure, "operator", "record one local untrusted annotation; no candidate is approved or created by this action"}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(preview); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		claim, err := store.RecordProcedureClaim(cmd.Context(), owner.ProjectID, ref.ID, procedure, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(claim)
	}}
	o.flags(cmd)
	sources.flags(cmd)
	cmd.Flags().StringVar(&key, "key", "", "stable procedure key")
	cmd.Flags().StringVar(&workflow, "workflow", "", "operator-reviewed reusable procedure steps")
	cmd.Flags().StringVar(&outcome, "outcome", "", "observed outcome: succeeded, verified, or failed")
	cmd.Flags().BoolVar(&generalizable, "generalizable", false, "whether the procedure applies beyond project-specific facts")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm recording the displayed local procedure claim")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("key")
	_ = cmd.MarkFlagRequired("workflow")
	_ = cmd.MarkFlagRequired("outcome")
	_ = cmd.MarkFlagRequired("generalizable")
	return cmd
}

var memoryPipelineSkillComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func newMemoryPipelineCandidateConvertCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var identifier, library, version string
	var yes bool
	cmd := &cobra.Command{Use: "convert <candidate-id>", Short: "Preview and explicitly convert an approved candidate into a local Skill", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !memoryPipelineSkillComponent.MatchString(identifier) || !memoryPipelineSkillComponent.MatchString(version) {
			return fmt.Errorf("--identifier and --version must be simple Skill components")
		}
		if strings.TrimSpace(library) == "" {
			return fmt.Errorf("--library is required")
		}
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		candidate, err := store.SkillCandidate(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		ensureFreshEvidence := func() error {
			generation, err := store.EvidenceGeneration(cmd.Context(), owner.ProjectID)
			if err != nil {
				return err
			}
			if generation != candidate.EvidenceGeneration {
				return fmt.Errorf("%w: candidate evidence changed; run candidate detection again", memorypipeline.ErrConflict)
			}
			return nil
		}
		if err := ensureFreshEvidence(); err != nil {
			return err
		}
		if candidate.Status != memorypipeline.SkillCandidateApproved {
			return fmt.Errorf("%w: candidate must be approved before conversion", memorypipeline.ErrConflict)
		}
		preview := struct {
			CandidateID string   `json:"candidateId"`
			Identifier  string   `json:"identifier"`
			Version     string   `json:"version"`
			Library     string   `json:"library"`
			Workflow    string   `json:"workflow"`
			Evidence    []string `json:"evidenceRefs"`
			Effect      string   `json:"effect"`
		}{candidate.ID, identifier, version, library, candidate.ProposedWorkflow, candidate.EvidenceRefs, "create one untrusted local Skill version; no installation or activation"}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(preview); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		if err := ensureFreshEvidence(); err != nil {
			return err
		}
		source, err := os.MkdirTemp("", "agent-manager-candidate-")
		if err != nil {
			return fmt.Errorf("prepare Skill candidate source: %w", err)
		}
		defer os.RemoveAll(source)
		content := fmt.Sprintf("---\nname: %s\ndescription: %q\n---\n\n# %s\n\nThis Skill was explicitly converted from approved candidate `%s`. Its source procedure remains untrusted; review and edit it before use.\n\n## Workflow\n\n%s\n", identifier, "Untrusted workflow converted from a reviewed local Skill candidate.", identifier, candidate.ID, candidate.ProposedWorkflow)
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(content), 0o600); err != nil {
			return fmt.Errorf("write converted Skill source: %w", err)
		}
		repository, err := skillrepo.Open(library)
		if err != nil {
			return err
		}
		entry, err := repository.AddVersion(identifier, source, skillrepo.VersionMetadata{
			Version: version, Source: "memory-candidate://" + candidate.ID, SourceType: "agent-manager-memory-candidate",
			TrustLevel: "untrusted", VerificationStatus: "candidate-reviewed",
			LegacyProvenance: "Derived from approved SkillCandidate " + candidate.ID + "; evidence references remain in the project-local memory pipeline.",
		}, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Identifier string `json:"identifier"`
			Version    string `json:"version"`
			Path       string `json:"path"`
			Trust      string `json:"trustLevel"`
		}{entry.Identifier, entry.Version, entry.Path, entry.Metadata.TrustLevel})
	}}
	o.flags(cmd)
	cmd.Flags().StringVar(&identifier, "identifier", "", "safe Skill identifier in the destination repository")
	cmd.Flags().StringVar(&library, "library", "", "explicit local Skill repository root")
	cmd.Flags().StringVar(&version, "version", "0.1.0", "new Skill version")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed candidate conversion")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("identifier")
	_ = cmd.MarkFlagRequired("library")
	return cmd
}

func newMemoryPipelineCandidateDetectCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	cmd := &cobra.Command{Use: "detect", Short: "Detect repeated generalizable procedures from imported completed runs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		candidates, err := store.DetectSkillCandidates(cmd.Context(), owner.ProjectID, provider)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(candidates)
	}}
	o.flags(cmd)
	sources.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineCandidateListCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "list", Short: "List Skill candidates with provenance and review state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		candidates, err := store.ListSkillCandidates(cmd.Context(), owner.ProjectID)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(candidates)
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineCandidateReviewCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var decision string
	var yes bool
	cmd := &cobra.Command{Use: "review <candidate-id>", Short: "Confirm a candidate decision for manual Skill authoring review", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		var state memorypipeline.SkillCandidateReviewState
		switch decision {
		case "approve":
			state = memorypipeline.SkillCandidateApproved
		case "reject":
			state = memorypipeline.SkillCandidateRejected
		default:
			return fmt.Errorf("--decision must be approve or reject")
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		candidate, err := store.SkillCandidate(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Candidate memorypipeline.SkillCandidate            `json:"candidate"`
			Decision  memorypipeline.SkillCandidateReviewState `json:"decision"`
			Effect    string                                   `json:"effect"`
		}{candidate, state, "review state only; no Skill is created, installed, or published"}); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		reviewed, err := store.ReviewSkillCandidate(cmd.Context(), owner.ProjectID, candidate.ID, state, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(reviewed)
	}}
	o.flags(cmd)
	cmd.Flags().StringVar(&decision, "decision", "", "review decision: approve or reject")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed candidate review decision")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("decision")
	return cmd
}

func newMemoryPipelineInitCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "init", Short: "Explicitly initialize the project-local SQLite memory pipeline", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.InitializeProject(project)
		if err != nil {
			return err
		}
		defer store.Close()
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			ProjectID string `json:"projectId"`
			Path      string `json:"path"`
			FTS5      bool   `json:"fts5"`
		}{owner.ProjectID, store.Path(), store.FTS5Available()})
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineImportCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	cmd := &cobra.Command{Use: "import", Short: "Import references to Agent Manager runs and explicitly selected Codex/Pi sessions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		refs, err := provider.Discover(cmd.Context(), owner.ProjectID)
		if err != nil {
			return err
		}
		imported := make([]memorypipeline.RunEvidenceRef, 0, len(refs))
		for _, ref := range refs {
			stored, err := store.ImportEvidence(cmd.Context(), ref)
			if err != nil {
				return err
			}
			imported = append(imported, stored)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(imported)
	}}
	o.flags(cmd)
	sources.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineExtractCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	cmd := &cobra.Command{Use: "extract <evidence-id>", Short: "Extract one imported source reference and consolidate its derived memory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		ref, err := store.Evidence(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		evidence, err := provider.Read(cmd.Context(), ref)
		if err != nil {
			return err
		}
		extraction, err := memorypipeline.ExtractEvidence(evidence)
		if err != nil {
			return err
		}
		if err := store.Consolidate(cmd.Context(), extraction); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(extraction)
	}}
	o.flags(cmd)
	sources.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineConsolidateCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	cmd := &cobra.Command{Use: "consolidate", Short: "Re-extract imported evidence and refresh project derived memory", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		refs, err := store.ListEvidence(cmd.Context(), owner.ProjectID)
		if err != nil {
			return err
		}
		processed := 0
		for _, ref := range refs {
			evidence, err := provider.Read(cmd.Context(), ref)
			if err != nil {
				return err
			}
			extraction, err := memorypipeline.ExtractEvidence(evidence)
			if err != nil {
				return err
			}
			if err := store.Consolidate(cmd.Context(), extraction); err != nil {
				return err
			}
			processed++
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			ProjectID string `json:"projectId"`
			Evidence  int    `json:"evidenceProcessed"`
		}{owner.ProjectID, processed})
	}}
	o.flags(cmd)
	sources.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineSearchCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var limit int
	cmd := &cobra.Command{Use: "search <query>", Short: "Search bounded local derived memory with FTS5", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		results, err := store.Search(cmd.Context(), owner.ProjectID, args[0], limit)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
	}}
	o.flags(cmd)
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum derived results (1-100)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineSummaryCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "summary", Short: "Read the compact generated project summary", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		summary, err := store.Summary(cmd.Context(), owner.ProjectID)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(summary)
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineEvidenceCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var sources memoryEvidenceSourceOptions
	cmd := &cobra.Command{Use: "evidence <evidence-id>", Short: "Explicitly open one source run through its provider", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, provider, err := openPipelineAndRunProvider(project, owner, sources)
		if err != nil {
			return err
		}
		defer store.Close()
		ref, err := store.Evidence(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		evidence, err := provider.Read(cmd.Context(), ref)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(evidence)
	}}
	o.flags(cmd)
	sources.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineProposalCommand(rootOptions *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "proposal", Short: "Review derived-memory proposals before canonical publication"}
	command.AddCommand(newMemoryPipelineProposalCreateCommand(rootOptions))
	command.AddCommand(newMemoryPipelineProposalListCommand(rootOptions))
	command.AddCommand(newMemoryPipelineProposalAcceptCommand(rootOptions))
	command.AddCommand(newMemoryPipelineProposalRejectCommand(rootOptions))
	command.AddCommand(newMemoryPipelineProposalReconcileCommand(rootOptions))
	return command
}

func newMemoryPipelineProposalCreateCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "create <derived-id>", Short: "Create a pending proposal from an active unverified derived memory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		proposal, err := store.Propose(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(proposal)
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineProposalListCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "list", Short: "List pending and terminal proposal records", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		proposals, err := store.ListProposals(cmd.Context(), owner.ProjectID)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(proposals)
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineProposalAcceptCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var yes bool
	cmd := &cobra.Command{Use: "accept <proposal-id>", Short: "Preview and confirm exact canonical Memory publication", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		gateway, gatewayOwner, err := configuredGateway(rootOptions, o, true)
		if err != nil {
			return err
		}
		if gatewayOwner != owner {
			return memory.ErrOwnershipDenied
		}
		prepared, err := store.PreviewProposalAcceptance(cmd.Context(), owner.ProjectID, args[0], gateway, owner)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Canonical Memory proposal acceptance plan"); err != nil {
			return err
		}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(prepared); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		accepted, err := store.ConfirmProposalAcceptance(cmd.Context(), prepared, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(accepted)
	}}
	o.flags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the displayed exact Memory Gateway plan")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineProposalRejectCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	var yes bool
	cmd := &cobra.Command{Use: "reject <proposal-id>", Short: "Review and explicitly reject a pending proposal", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		proposal, err := store.Proposal(cmd.Context(), owner.ProjectID, args[0])
		if err != nil {
			return err
		}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(proposal); err != nil {
			return err
		}
		confirmed, err := confirmMemoryPlan(cmd, yes)
		if err != nil || !confirmed {
			return err
		}
		rejected, err := store.RejectProposal(cmd.Context(), owner.ProjectID, args[0], true)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(rejected)
	}}
	o.flags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm rejection of the displayed proposal")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newMemoryPipelineProposalReconcileCommand(rootOptions *rootOptions) *cobra.Command {
	var o memoryOwnerOptions
	cmd := &cobra.Command{Use: "reconcile <proposal-id>", Short: "Inspect canonical Memory without replaying an uncertain write", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		owner, project, err := pipelineProject(rootOptions, o)
		if err != nil {
			return err
		}
		store, err := memorypipeline.OpenProjectStore(project)
		if err != nil {
			return err
		}
		defer store.Close()
		gateway, gatewayOwner, err := configuredGateway(rootOptions, o, false)
		if err != nil {
			return err
		}
		if gatewayOwner != owner {
			return memory.ErrOwnershipDenied
		}
		proposal, err := store.ReconcileProposal(cmd.Context(), owner.ProjectID, args[0], gateway, owner)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(proposal)
	}}
	o.flags(cmd)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func pipelineProject(rootOptions *rootOptions, options memoryOwnerOptions) (memory.Owner, string, error) {
	if options.project == "" || options.user != "" || options.agent != "" || options.session != "" {
		return memory.Owner{}, "", fmt.Errorf("select exactly one project with --project; user, agent, and session owners are not supported")
	}
	registry, err := projectRegistry(rootOptions, options.registry)
	if err != nil {
		return memory.Owner{}, "", err
	}
	identity, err := registry.Lookup(options.project)
	if err != nil {
		return memory.Owner{}, "", memory.SafeError(err)
	}
	return memory.Owner{Kind: memory.OwnerProject, ProjectID: identity.ID}, identity.Directory, nil
}

type memoryEvidenceSourceOptions struct{ codexDirectory, piDirectory string }

func (o *memoryEvidenceSourceOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.codexDirectory, "codex-sessions-dir", "", "explicit read-only Codex sessions directory for imported native evidence")
	cmd.Flags().StringVar(&o.piDirectory, "pi-sessions-dir", "", "explicit read-only Pi sessions directory for imported native evidence")
}

func openPipelineAndRunProvider(project string, owner memory.Owner, sources memoryEvidenceSourceOptions) (*memorypipeline.Store, *memorypipeline.CompositeRunEvidenceProvider, error) {
	store, err := memorypipeline.OpenProjectStore(project)
	if err != nil {
		return nil, nil, err
	}
	runStore, err := run.DefaultStore()
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	managerProvider, err := memorypipeline.NewAgentManagerRunProvider(runStore, owner.ProjectID, project)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	providers := []memorypipeline.RunEvidenceProvider{managerProvider}
	if sources.codexDirectory != "" {
		provider, err := memorypipeline.NewCodexRolloutProvider(sources.codexDirectory, owner.ProjectID, project)
		if err != nil {
			_ = store.Close()
			return nil, nil, err
		}
		providers = append(providers, provider)
	}
	if sources.piDirectory != "" {
		provider, err := memorypipeline.NewPiSessionProvider(sources.piDirectory, owner.ProjectID, project)
		if err != nil {
			_ = store.Close()
			return nil, nil, err
		}
		providers = append(providers, provider)
	}
	provider, err := memorypipeline.NewCompositeRunEvidenceProvider(providers...)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return store, provider, nil
}
