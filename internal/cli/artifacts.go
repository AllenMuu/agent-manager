package cli

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/spf13/cobra"
)

func newTaskCommand() *cobra.Command {
	var project, taskID, summary, problem string
	var goals, constraints []string
	command := &cobra.Command{Use: "task", Short: "Create and inspect cross-agent tasks"}
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Create a task with its initial intent artifact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := artifact.NewStore(project)
			if err != nil {
				return err
			}
			if taskID == "" {
				taskID = "task-" + time.Now().UTC().Format("20060102-150405")
			}
			if strings.TrimSpace(summary) == "" {
				summary = "New Agent Manager task"
			}
			intent := artifact.New(artifact.Intent, taskID, store.ProjectRoot, time.Now().UTC())
			intent.Set("summary", summary)
			intent.Set("problem", problem)
			intent.Set("goals", stringList(goals))
			intent.Set("non_goals", []string{})
			intent.Set("constraints", stringList(constraints))
			intent.Set("acceptance_criteria", []string{})
			intent.Set("risk_level", "medium")
			path, err := store.Init(taskID, intent)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "created task %s\nintent: %s\n", taskID, path)
			return err
		},
	}
	initCommand.Flags().StringVar(&taskID, "id", "", "stable task identifier")
	initCommand.Flags().StringVar(&summary, "summary", "", "intent summary")
	initCommand.Flags().StringVar(&problem, "problem", "", "problem statement")
	initCommand.Flags().StringSliceVar(&goals, "goal", nil, "task goal (repeatable)")
	initCommand.Flags().StringSliceVar(&constraints, "constraint", nil, "task constraint (repeatable)")
	initCommand.Flags().StringVar(&project, "project", ".", "project root")
	command.AddCommand(initCommand)
	return command
}

func newArtifactsCommand() *cobra.Command {
	var project string
	command := &cobra.Command{Use: "artifacts", Short: "Save, inspect, and validate task artifacts"}
	command.PersistentFlags().StringVar(&project, "project", ".", "project root")
	command.AddCommand(newArtifactListCommand(&project))
	command.AddCommand(newArtifactSaveCommand(&project))
	command.AddCommand(newArtifactShowCommand(&project))
	command.AddCommand(newArtifactValidateCommand(&project))
	command.AddCommand(newArtifactRenderCommand(&project))
	return command
}

func newArtifactSaveCommand(project *string) *cobra.Command {
	var yes bool
	command := &cobra.Command{Use: "save <task-id> <path>", Short: "Validate and save an artifact into a task", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifact.NewStore(*project)
		if err != nil {
			return err
		}
		doc, err := artifact.Load(artifactPath(*project, args[1]))
		if err != nil {
			return err
		}
		destination := filepath.Join(store.TasksRoot(), args[0], string(doc.Kind())+".yaml")
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Artifact save plan\ntask: %s\nkind: %s\ndestination: %s\n", args[0], doc.Kind(), destination); err != nil {
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
		path, err := store.Save(args[0], doc)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "saved %s artifact for task %s: %s\n", doc.Kind(), args[0], path)
		return err
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm the displayed artifact save plan")
	return command
}

func newArtifactListCommand(project *string) *cobra.Command {
	return &cobra.Command{Use: "list <task-id>", Short: "List artifacts in a task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifact.NewStore(*project)
		if err != nil {
			return err
		}
		entries, err := store.List(args[0])
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", entry.Kind, entry.Path); err != nil {
				return err
			}
		}
		return nil
	}}
}

func newArtifactShowCommand(project *string) *cobra.Command {
	return &cobra.Command{Use: "show <task-id> <kind>", Short: "Show an artifact as YAML", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifact.NewStore(*project)
		if err != nil {
			return err
		}
		doc, _, err := store.Load(args[0], artifact.Kind(args[1]))
		if err != nil {
			return err
		}
		data, err := doc.YAML()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}}
}

func newArtifactValidateCommand(project *string) *cobra.Command {
	return &cobra.Command{Use: "validate <path>", Short: "Validate one artifact file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(*project, path)
		}
		doc, err := artifact.Load(path)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: valid (%s/%s)\n", path, doc.Kind(), doc.ID())
		return err
	}}
}

func newArtifactRenderCommand(project *string) *cobra.Command {
	return &cobra.Command{Use: "render <path>", Short: "Render an artifact for human inspection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(*project, path)
		}
		doc, err := artifact.Load(path)
		if err != nil {
			return err
		}
		rendered, err := doc.Render()
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), rendered)
		return err
	}}
}

func stringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func artifactPath(project, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(project, path)
}
