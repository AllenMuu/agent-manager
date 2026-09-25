package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/recommend"
	"github.com/AllenMuu/skill-manager/internal/stack"
	"github.com/spf13/cobra"
)

func newRecommendCommand(options *rootOptions) *cobra.Command {
	var project string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend library skills for the current project's stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = "."
			}
			lib, err := library(options)
			if err != nil {
				return renderRecommendError(cmd, asJSON, "catalog_unavailable", err, options.configPath)
			}
			result, err := recommend.Recommend(project, lib)
			if err != nil {
				if isProjectError(err) {
					return renderRecommendError(cmd, asJSON, "invalid_project", err, project)
				}
				return renderRecommendError(cmd, asJSON, "catalog_unavailable", err, lib)
			}
			if asJSON {
				return writeRecommendJSON(cmd, result)
			}
			return writeRecommendText(cmd, result)
		},
	}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func isProjectError(err error) bool {
	return strings.Contains(err.Error(), "inspect project") || strings.Contains(err.Error(), "is not a directory")
}

type recommendJSONMatch struct {
	Technology string `json:"technology"`
	Field      string `json:"field"`
}

type recommendJSONRecommendation struct {
	Identifier       string                `json:"identifier"`
	Description      string                `json:"description"`
	MatchReason      string                `json:"matchReason"`
	MatchingEvidence []recommendJSONMatch  `json:"matchingEvidence"`
	Confidence       *recommend.Confidence `json:"confidence"`
}

type recommendJSONScope struct {
	Path            string                        `json:"path"`
	Status          recommend.Status              `json:"status"`
	Technologies    []stack.Technology            `json:"technologies"`
	Evidence        []stack.Evidence              `json:"evidence"`
	Diagnostics     []stack.Diagnostic            `json:"diagnostics"`
	Recommendations []recommendJSONRecommendation `json:"recommendations"`
	NextAction      recommend.Action              `json:"nextAction"`
}

type recommendJSONResult struct {
	Project      string               `json:"project"`
	ScanComplete bool                 `json:"scanComplete"`
	Scopes       []recommendJSONScope `json:"scopes"`
}

type recommendJSONError struct {
	Error recommendJSONErrorBody `json:"error"`
}

type recommendJSONErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

func writeRecommendJSON(cmd *cobra.Command, result recommend.Result) error {
	out := recommendJSONResult{
		Project:      result.Project,
		ScanComplete: result.ScanComplete,
		Scopes:       []recommendJSONScope{},
	}
	for _, scope := range result.Scopes {
		jsonScope := recommendJSONScope{
			Path:            scope.Path,
			Status:          scope.Status,
			Technologies:    scope.Technologies,
			Evidence:        scope.Evidence,
			Diagnostics:     scope.Diagnostics,
			Recommendations: []recommendJSONRecommendation{},
			NextAction:      scope.NextAction,
		}
		if jsonScope.Technologies == nil {
			jsonScope.Technologies = []stack.Technology{}
		}
		if jsonScope.Evidence == nil {
			jsonScope.Evidence = []stack.Evidence{}
		}
		if jsonScope.Diagnostics == nil {
			jsonScope.Diagnostics = []stack.Diagnostic{}
		}
		for _, rec := range scope.Recommendations {
			confidence := rec.Confidence
			jsonRec := recommendJSONRecommendation{
				Identifier:       rec.Identifier,
				Description:      rec.Description,
				MatchReason:      rec.MatchReason,
				MatchingEvidence: []recommendJSONMatch{},
				Confidence:       &confidence,
			}
			for _, m := range rec.MatchingEvidence {
				jsonRec.MatchingEvidence = append(jsonRec.MatchingEvidence, recommendJSONMatch{Technology: m.Technology, Field: m.Field})
			}
			jsonScope.Recommendations = append(jsonScope.Recommendations, jsonRec)
		}
		out.Scopes = append(out.Scopes, jsonScope)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return err
}

func renderRecommendError(cmd *cobra.Command, asJSON bool, code string, err error, path string) error {
	if asJSON {
		encoded, marshalErr := json.Marshal(recommendJSONError{Error: recommendJSONErrorBody{Code: code, Message: err.Error(), Path: path}})
		if marshalErr == nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		}
	}
	_, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", code, err)
	if writeErr != nil {
		return writeErr
	}
	return err
}

func writeRecommendText(cmd *cobra.Command, result recommend.Result) error {
	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(w, "Project: %s\n\n", result.Project); err != nil {
		return err
	}
	for _, scope := range result.Scopes {
		if _, err := fmt.Fprintf(w, "Scope %s: %s\n", scope.Path, scope.Status); err != nil {
			return err
		}
		if len(scope.Technologies) == 0 {
			if _, err := fmt.Fprintln(w, "  No supported project markers were recognized; select a directory with documented markers or use catalog search."); err != nil {
				return err
			}
		}
		for _, tech := range scope.Technologies {
			if _, err := fmt.Fprintf(w, "  technology: %s (%s)\n", tech.Label, tech.Category); err != nil {
				return err
			}
		}
		for _, e := range scope.Evidence {
			if _, err := fmt.Fprintf(w, "  evidence: %s -> %s\n", e.Path, e.Technology); err != nil {
				return err
			}
		}
		for _, d := range scope.Diagnostics {
			if _, err := fmt.Fprintf(w, "  diagnostic: %s: %s\n", d.Path, d.Message); err != nil {
				return err
			}
		}
		if len(scope.Recommendations) == 0 && len(scope.Technologies) > 0 {
			if _, err := fmt.Fprintln(w, "  No eligible catalog skill matched this stack; add technology tags to relevant skills or use catalog search."); err != nil {
				return err
			}
		}
		for _, rec := range scope.Recommendations {
			confidence := "n/a"
			if rec.Confidence != "" {
				confidence = string(rec.Confidence)
			}
			if _, err := fmt.Fprintf(w, "  recommend: %s (%s) — %s\n    reason: %s\n", rec.Identifier, confidence, rec.Description, rec.MatchReason); err != nil {
				return err
			}
			for _, m := range rec.MatchingEvidence {
				if _, err := fmt.Fprintf(w, "    match: %s in %s\n", m.Technology, m.Field); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(w, "  next action: %s\n\n", scope.NextAction); err != nil {
			return err
		}
	}
	if !result.ScanComplete {
		if _, err := fmt.Fprintln(w, "scan incomplete: some directories or markers were skipped"); err != nil {
			return err
		}
	}
	return nil
}
