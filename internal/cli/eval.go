package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AllenMuu/skill-manager/internal/eval"
	"github.com/spf13/cobra"
)

type evalOptions struct {
	project string
	json    bool
}

func newEvalCommand() *cobra.Command {
	options := &evalOptions{}
	command := &cobra.Command{Use: "eval", Short: "Run deterministic local regression evaluations"}
	command.PersistentFlags().StringVar(&options.project, "project", ".", "project root")
	command.PersistentFlags().BoolVar(&options.json, "json", false, "write machine-readable JSON")
	command.AddCommand(newEvalListCommand(options), newEvalRunCommand(options), newEvalCompareCommand(options))
	return command
}

func newEvalListCommand(options *evalOptions) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List local eval suites and case counts", RunE: func(cmd *cobra.Command, _ []string) error {
		root := filepath.Join(options.project, "evals")
		entries, err := os.ReadDir(root)
		if err != nil {
			return fmt.Errorf("read eval root: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			suite, loadErr := eval.LoadSuite(filepath.Join(root, entry.Name()))
			if loadErr != nil {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\tinvalid\t%s\n", entry.Name(), loadErr); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d cases\n", suite.ID, len(suite.Cases)); err != nil {
				return err
			}
		}
		return nil
	}}
}

func newEvalRunCommand(options *evalOptions) *cobra.Command {
	var agent, candidateDir, configVersion, output string
	command := &cobra.Command{Use: "run <suite>", Short: "Run rule-based checks for an eval suite", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		suitePath := args[0]
		if !filepath.IsAbs(suitePath) {
			if _, err := os.Stat(suitePath); err != nil {
				suitePath = filepath.Join(options.project, "evals", suitePath)
			}
		}
		suite, err := eval.LoadSuite(suitePath)
		if err != nil {
			return err
		}
		result, err := eval.Run(suite, eval.Options{Agent: agent, ConfigVersion: configVersion, CandidateDir: candidateDir})
		if err != nil {
			return err
		}
		if output == "" {
			output = filepath.Join(options.project, ".agent-manager", "evals", result.RunID+".yaml")
		}
		if !filepath.IsAbs(output) {
			output = filepath.Join(options.project, output)
		}
		if err := eval.WriteResult(output, result); err != nil {
			return err
		}
		if options.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "run: %s\nsuite: %s\nstatus: %s\nscore: %d\nresult: %s\n", result.RunID, result.Suite, resultStatus(result), result.Summary.Score, output)
		return err
	}}
	command.Flags().StringVar(&agent, "agent", "", "agent/runtime label recorded in the result")
	command.Flags().StringVar(&candidateDir, "candidate-dir", "", "directory containing <case-id>.md candidate responses")
	command.Flags().StringVar(&configVersion, "config-version", "v1", "Skill/prompt/context configuration version label")
	command.Flags().StringVar(&output, "output", "", "result file path")
	return command
}

func newEvalCompareCommand(options *evalOptions) *cobra.Command {
	return &cobra.Command{Use: "compare <baseline> <candidate>", Short: "Compare two persisted eval results", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		baseline, err := eval.LoadResult(artifactPath(options.project, args[0]))
		if err != nil {
			return err
		}
		candidate, err := eval.LoadResult(artifactPath(options.project, args[1]))
		if err != nil {
			return err
		}
		comparison := eval.Compare(baseline, candidate)
		if options.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(comparison)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "baseline: %s\ncandidate: %s\nregressions: %d\nimprovements: %d\nunchanged: %d\n", comparison.BaselineRun, comparison.CandidateRun, len(comparison.Regressions), len(comparison.Improvements), comparison.Unchanged)
		for _, regression := range comparison.Regressions {
			if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "regression: %s (%s)\n", regression.CaseID, regression.Reason); writeErr != nil {
				return writeErr
			}
		}
		return err
	}}
}

func resultStatus(result eval.Result) string {
	if result.Summary.Failed > 0 {
		return "fail"
	}
	if result.Summary.Partial > 0 {
		return "partial"
	}
	return "pass"
}
