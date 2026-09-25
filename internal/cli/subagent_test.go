package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/cli"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/spf13/cobra"
)

func TestSubAgentsListJSONReturnsCanonicalDefinitions(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "reviewer.yaml", `version: v1
id: reviewer
name: Code Reviewer
role: Reviews changes
instructions: Review the diff and report risks.
skills:
  - go-helper
compatibility:
  agents:
    - codex
requiredCapabilities:
  - filesystem-read
`)
	library := t.TempDir()
	writeSkill(t, library, "go-helper", "Go helper", "helper", "go\n")

	rootCommand := cli.NewRootCommand()
	output := &bytes.Buffer{}
	rootCommand.SetOut(output)
	rootCommand.SetErr(output)
	rootCommand.SetArgs([]string{"subagents", "list", "--root", root, "--library", library, "--json"})
	if err := rootCommand.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var report struct {
		Definitions []map[string]any `json:"definitions"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output)
	}
	if len(report.Definitions) != 1 || report.Definitions[0]["id"] != "reviewer" || report.Definitions[0]["role"] != "Reviews changes" {
		t.Fatalf("canonical list = %#v", report)
	}
	if _, ok := report.Definitions[0]["compatibility"]; !ok {
		t.Fatalf("canonical list omitted compatibility: %#v", report.Definitions[0])
	}
	if report.Diagnostics == nil {
		t.Fatal("canonical list omitted diagnostics array")
	}
}

func TestSubAgentsShowHumanIncludesCanonicalDetails(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "reviewer.yaml", `version: v1
id: reviewer
name: Code Reviewer
role: Reviews changes
instructions: Review the diff and report risks.
compatibility:
  agents:
    - codex
`)
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "show", "reviewer", "--root", root})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"reviewer", "Code Reviewer", "Reviews changes", "Review the diff and report risks.", "codex"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output=%q; missing %q", output.String(), want)
		}
	}
}

func TestSubAgentsShowUnknownIDIsActionable(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
`)
	command := cli.NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"subagents", "show", "missing", "--root", root})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "available") {
		t.Fatalf("error = %v, want actionable unknown ID", err)
	}
}

func TestSubAgentsValidateJSONReportsInvalidDefinitions(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "broken.yaml", `version: v2
id: Broken
name: Broken
role: Review
instructions: Review changes.
`)
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "validate", "--root", root, "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("validate should report diagnostics without command error: %v", err)
	}
	var report struct {
		Valid       bool `json:"valid"`
		Diagnostics []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output.String())
	}
	if report.Valid || len(report.Diagnostics) != 1 || !strings.Contains(report.Diagnostics[0].Message, "version") {
		t.Fatalf("validation report = %#v", report)
	}
}

func TestSubAgentsValidateSpecificInvalidIDReturnsItsDiagnostic(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "broken.yaml", "version: v2\nid: broken\n")
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "validate", "broken", "--root", root, "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("validate should report diagnostics without command error: %v", err)
	}
	if !strings.Contains(output.String(), `"valid":false`) || !strings.Contains(output.String(), "version") {
		t.Fatalf("validation output=%q; want invalid version diagnostic", output.String())
	}
}

func TestSubAgentsDefaultUsesDefaultConfigAndSkillLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataRoot := filepath.Join(home, ".agents")
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
skills:
  - go-helper
`)
	writeSkill(t, filepath.Join(dataRoot, "skills"), "go-helper", "Go helper", "helper", "go\n")

	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "list", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var report struct {
		Definitions []map[string]any `json:"definitions"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output.String())
	}
	if len(report.Definitions) != 1 || report.Definitions[0]["id"] != "reviewer" {
		t.Fatalf("default list = %#v", report)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("default list diagnostics = %#v", report.Diagnostics)
	}
}

func TestSubAgentsListJSONIncludesRegistryDiagnosticsInEnvelope(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "broken.yaml", "version: v2\nid: broken\n")
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "list", "--root", root, "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var report struct {
		Definitions []map[string]any `json:"definitions"`
		Diagnostics []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output.String())
	}
	if report.Definitions == nil || len(report.Definitions) != 0 {
		t.Fatalf("definitions = %#v, want empty array", report.Definitions)
	}
	if len(report.Diagnostics) != 1 || !strings.Contains(report.Diagnostics[0].Message, "version") {
		t.Fatalf("diagnostics = %#v, want invalid version", report.Diagnostics)
	}
}

func TestSubAgentsValidateSpecificInvalidIDUsesCanonicalIDNotFilename(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "definition.yaml", "version: v2\nid: broken\n")
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "validate", "broken", "--root", root, "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("validate should report diagnostics without command error: %v", err)
	}
	if !strings.Contains(output.String(), `"valid":false`) || !strings.Contains(output.String(), "version") {
		t.Fatalf("validation output=%q; want invalid definition diagnostic", output.String())
	}
}

func TestSubAgentsCleanInstallListHumanTreatsMissingDefinitionsAsEmpty(t *testing.T) {
	_, command := cleanInstallSubAgentsCommand(t)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "list"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("list output = %q, want empty registry output", output.String())
	}
}

func TestSubAgentsCleanInstallListJSONReturnsEmptyArrays(t *testing.T) {
	_, command := cleanInstallSubAgentsCommand(t)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "list", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var report struct {
		Definitions []map[string]any `json:"definitions"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output.String())
	}
	if report.Definitions == nil || report.Diagnostics == nil {
		t.Fatalf("clean-install list = %#v, want non-nil arrays", report)
	}
	if len(report.Definitions) != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("clean-install list = %#v, want empty arrays", report)
	}
}

func TestSubAgentsCleanInstallValidateHumanReportsSuccess(t *testing.T) {
	_, command := cleanInstallSubAgentsCommand(t)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "validate"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "all SubAgent definitions are valid") {
		t.Fatalf("validate output = %q, want success guidance", output.String())
	}
}

func TestSubAgentsCleanInstallValidateJSONReportsSuccess(t *testing.T) {
	_, command := cleanInstallSubAgentsCommand(t)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "validate", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var report struct {
		Valid       bool             `json:"valid"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v; output=%s", err, output.String())
	}
	if !report.Valid || report.Diagnostics == nil || len(report.Diagnostics) != 0 {
		t.Fatalf("clean-install validation = %#v, want valid with empty diagnostics", report)
	}
}

func TestSubAgentsMinimalJSONUsesStableCanonicalFieldsForListAndShow(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "minimal.yaml", `version: v1
id: minimal
name: Minimal
role: Review
instructions: Review changes.
`)
	for _, args := range [][]string{
		{"subagents", "list", "--root", root, "--json"},
		{"subagents", "show", "minimal", "--root", root, "--json"},
	} {
		command := cli.NewRootCommand()
		output := &bytes.Buffer{}
		command.SetOut(output)
		command.SetErr(output)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		var value map[string]any
		if args[1] == "list" {
			var report struct {
				Definitions []map[string]any `json:"definitions"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatalf("decode list JSON: %v; output=%s", err, output.String())
			}
			if len(report.Definitions) != 1 {
				t.Fatalf("list definitions = %#v", report.Definitions)
			}
			value = report.Definitions[0]
		} else if err := json.Unmarshal(output.Bytes(), &value); err != nil {
			t.Fatalf("decode show JSON: %v; output=%s", err, output.String())
		}
		if skills, ok := value["skills"].([]any); !ok || skills == nil || len(skills) != 0 {
			t.Errorf("%v skills = %#v, want empty array", args, value["skills"])
		}
		compatibility, ok := value["compatibility"].(map[string]any)
		if !ok {
			t.Errorf("%v compatibility = %#v, want object", args, value["compatibility"])
			continue
		}
		if agents, ok := compatibility["agents"].([]any); !ok || agents == nil || len(agents) != 0 {
			t.Errorf("%v compatibility.agents = %#v, want empty array", args, compatibility["agents"])
		}
		if capabilities, ok := value["requiredCapabilities"].([]any); !ok || capabilities == nil || len(capabilities) != 0 {
			t.Errorf("%v requiredCapabilities = %#v, want empty array", args, value["requiredCapabilities"])
		}
	}
}

func TestSubAgentsHumanDiagnosticsPropagateStderrWriterFailure(t *testing.T) {
	root := t.TempDir()
	writeSubAgent(t, root, "broken.yaml", "version: v2\nid: broken\n")
	library := t.TempDir()
	for _, commandArgs := range [][]string{
		{"subagents", "list", "--root", root, "--library", library},
		{"subagents", "validate", "--root", root, "--library", library},
	} {
		command := cli.NewRootCommand()
		command.SetOut(&bytes.Buffer{})
		command.SetErr(failingWriter{err: errors.New("diagnostic write failure")})
		command.SetArgs(commandArgs)
		err := command.Execute()
		if err == nil || err.Error() != "diagnostic write failure" {
			t.Errorf("Execute(%v) error = %v, want diagnostic write failure", commandArgs, err)
		}
	}
}

func TestSubAgentsInstallAndRemoveRequireExplicitConfirmation(t *testing.T) {
	dataRoot := t.TempDir()
	project := t.TempDir()
	library := t.TempDir()
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
`)

	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code"})
	if err := command.Execute(); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("install without --yes error = %v, want not-confirmed", err)
	}
	destination := filepath.Join(project, ".claude", "agents", "reviewer.md")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed install destination = %v, want absent", err)
	}

	command = cli.NewRootCommand()
	output.Reset()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatalf("confirmed install error = %v", err)
	}
	if _, err := os.Lstat(destination); err != nil {
		t.Fatalf("confirmed install destination = %v", err)
	}

	command = cli.NewRootCommand()
	output.Reset()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "remove", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatalf("confirmed remove error = %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("removed destination = %v, want absent", err)
	}
}

func TestSubAgentsInstallShowsConflictPlanBeforeForceRefusal(t *testing.T) {
	dataRoot := t.TempDir()
	project := t.TempDir()
	library := t.TempDir()
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
`)
	destination := filepath.Join(project, ".codex", "agents", "reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "codex", "--conflict", "replace"})
	err := command.Execute()
	if !errors.Is(err, adapter.ErrForceRequired) {
		t.Fatalf("install conflict error = %v, want force-required", err)
	}
	if !strings.Contains(output.String(), "replace conflicting path with absolute link") {
		t.Fatalf("conflict output = %q, want displayed replacement plan", output.String())
	}
	content, readErr := os.ReadFile(destination)
	if readErr != nil || string(content) != "user-owned" {
		t.Fatalf("unmanaged destination changed: %q, %v", content, readErr)
	}
}

func TestSubAgentsInstallRendersBeforeMutationAndWriterFailureRefuses(t *testing.T) {
	dataRoot := t.TempDir()
	project := t.TempDir()
	library := t.TempDir()
	writeSubAgent(t, dataRoot, "reviewer.yaml", `version: v1
id: reviewer
name: Reviewer
role: Review
instructions: Review changes.
`)
	destination := filepath.Join(project, ".claude", "agents", "reviewer.md")

	writer := &installPlanWriter{destination: destination}
	command := cli.NewRootCommand()
	command.SetOut(writer)
	command.SetErr(writer)
	command.SetArgs([]string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", project, "--target", "claude-code", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatalf("confirmed install error = %v", err)
	}
	if !writer.sawPlan {
		t.Fatal("install did not render its confirmation plan")
	}
	if writer.destinationExisted {
		t.Fatal("install mutated destination before rendering confirmation plan")
	}
	if _, err := os.Lstat(destination); err != nil {
		t.Fatalf("confirmed install destination = %v", err)
	}

	failedProject := t.TempDir()
	failure := errors.New("plan output failed")
	command = cli.NewRootCommand()
	command.SetOut(failingWriter{err: failure})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"subagents", "install", "reviewer", "--root", dataRoot, "--library", library, "--project", failedProject, "--target", "claude-code", "--yes"})
	if err := command.Execute(); !errors.Is(err, failure) {
		t.Fatalf("writer failure = %v, want %v", err, failure)
	}
	if _, err := os.Lstat(filepath.Join(failedProject, ".claude", "agents", "reviewer.md")); !os.IsNotExist(err) {
		t.Fatalf("writer failure mutated destination: %v", err)
	}
}

type installPlanWriter struct {
	destination        string
	sawPlan            bool
	destinationExisted bool
}

func (w *installPlanWriter) Write(content []byte) (int, error) {
	if strings.Contains(string(content), "place resource") {
		w.sawPlan = true
		if _, err := os.Lstat(w.destination); err == nil {
			w.destinationExisted = true
		}
	}
	return len(content), nil
}

func cleanInstallSubAgentsCommand(t *testing.T) (string, *cobra.Command) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// This is a clean installation: neither the default Skill library nor the
	// canonical SubAgent definitions directory has been initialized yet.
	return home, cli.NewRootCommand()
}

func writeSubAgent(t *testing.T, root, name, contents string) {
	t.Helper()
	dir := filepath.Join(root, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
