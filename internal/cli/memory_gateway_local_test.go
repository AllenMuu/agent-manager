//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredCLIAddPreviewsOwnedIntentAndRequiresConfirmation(t *testing.T) {
	configPath, root, project := structuredCLIConfig(t)
	output, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes")
	if err != nil {
		t.Fatalf("register: %s %v", output, err)
	}
	args := []string{"memory", "add", "--project", project, "--type", "FACT", "--content", "local knowledge", "--source", "issue:21", "--evidence", "test:cli"}
	output, err = executeStructuredMemory(t, configPath, "n\n", args...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "PROJECT") || !strings.Contains(output, "local knowledge") || !strings.Contains(output, "add") || strings.Contains(output, root) {
		t.Fatalf("preview: %s", output)
	}
	output, err = executeStructuredMemory(t, configPath, "", "memory", "search", "knowledge", "--project", project)
	if err != nil {
		t.Fatal(err)
	}
	var found []memory.Record
	if err = json.Unmarshal([]byte(output), &found); err != nil || len(found) != 0 {
		t.Fatalf("preview mutated: %s %v", output, err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("preview created canonical state: %v %v", files, err)
	}
	output, err = executeStructuredMemory(t, configPath, "", append(args, "--yes")...)
	if err != nil {
		t.Fatalf("confirm: %s %v", output, err)
	}
	output, err = executeStructuredMemory(t, configPath, "", "memory", "search", "knowledge", "--project", project)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(output), &found); err != nil || len(found) != 1 || found[0].Source != "issue:21" || found[0].Evidence[0] != "test:cli" {
		t.Fatalf("search: %s %v", output, err)
	}
	if _, err = os.Stat(filepath.Join(project, ".agent-manager", "journal")); !os.IsNotExist(err) {
		t.Fatalf("Memory used filesystem operation journal: %v", err)
	}
}

func TestStructuredCLILifecycleRequiresConfirmationAndPreservesHistory(t *testing.T) {
	configPath, _, _ := structuredCLIConfig(t)
	exec := func(args ...string) string {
		t.Helper()
		out, err := executeStructuredMemory(t, configPath, "", args...)
		if err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
		return out
	}
	exec("memory", "add", "--user", "operator", "--type", "FACT", "--content", "v1", "--source", "notes", "--yes")
	var records []memory.Record
	if err := json.Unmarshal([]byte(exec("memory", "search", "--user", "operator")), &records); err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	id := string(records[0].ID)
	args := []string{"memory", "update", id, "--user", "operator", "--expected-version", "1", "--type", "FACT", "--content", "v2", "--source", "notes"}
	if out, err := executeStructuredMemory(t, configPath, "n\n", args...); err != nil {
		t.Fatalf("preview update: %s %v", out, err)
	}
	var current memory.Record
	if err := json.Unmarshal([]byte(exec("memory", "inspect", id, "--user", "operator")), &current); err != nil || current.Content != "v1" || current.Version != 1 {
		t.Fatal(current, err)
	}
	exec(append(args, "--yes")...)
	exec("memory", "supersede", id, "--user", "operator", "--expected-version", "2", "--type", "FACT", "--content", "v3", "--source", "notes", "--yes")
	if err := json.Unmarshal([]byte(exec("memory", "search", "--user", "operator")), &records); err != nil || len(records) != 1 || records[0].Content != "v3" {
		t.Fatal(records, err)
	}
	replacement := string(records[0].ID)
	out, err := executeStructuredMemory(t, configPath, "n\n", "memory", "forget", replacement, "--user", "operator", "--expected-version", "1")
	if err != nil {
		t.Fatalf("preview forget: %s %v", out, err)
	}
	exec("memory", "forget", replacement, "--user", "operator", "--expected-version", "1", "--yes")
	if err := json.Unmarshal([]byte(exec("memory", "inspect", replacement, "--user", "operator", "--history")), &records); err != nil || len(records) != 2 || records[1].State != memory.RecordDeleted {
		t.Fatal(records, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "update", id, "--user", "operator", "--expected-version", "1", "--type", "FACT", "--content", "stale", "--yes")
	if err == nil {
		t.Fatalf("stale mutation succeeded: %s", out)
	}
}

func TestStructuredCLIImportAndProjectRelocationAreExplicit(t *testing.T) {
	configPath, _, project := structuredCLIConfig(t)
	legacy := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(legacy, []byte("lesson one\nlesson two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes")
	if err != nil {
		t.Fatal(out, err)
	}
	registry, err := memory.OpenProjectRegistry(configPath + ".projects.json")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := registry.Lookup(project)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"memory", "import", legacy, "--project", project, "--type", "EXPERIENCE", "--source", "operator-declared"}
	out, err = executeStructuredMemory(t, configPath, "n\n", args...)
	if err != nil || !strings.Contains(out, "operator-declared") || !strings.Contains(out, identity.ID) {
		t.Fatal(out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "search", "--project", project)
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatal(out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", append(args, "--yes")...)
	if err != nil {
		t.Fatal(out, err)
	}
	relocated := t.TempDir()
	out, err = executeStructuredMemory(t, configPath, "n\n", "memory", "project", "relocate", identity.ID, "--project", relocated)
	if err != nil {
		t.Fatal(out, err)
	}
	if _, err = registry.Lookup(relocated); err == nil {
		t.Fatal("unconfirmed mapping moved")
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "project", "relocate", identity.ID, "--project", relocated, "--yes")
	if err != nil {
		t.Fatal(out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "search", "--project", relocated)
	if err != nil {
		t.Fatal(out, err)
	}
	var records []memory.Record
	if err = json.Unmarshal([]byte(out), &records); err != nil || len(records) != 2 || records[0].Owner.ProjectID != identity.ID || records[0].Source != "operator-declared" {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(legacy)
	if err != nil || string(data) != "lesson one\nlesson two\n" {
		t.Fatalf("legacy source changed: %q %v", data, err)
	}
}

func TestRoleContextUsesConfiguredGatewayWithoutImplicitWrites(t *testing.T) {
	configPath, root, project := structuredCLIConfig(t)
	exec := func(args ...string) string {
		t.Helper()
		out, err := executeStructuredMemory(t, configPath, "", args...)
		if err != nil {
			t.Fatal(out, err)
		}
		return out
	}
	exec("memory", "project", "register", "--project", project, "--yes")
	exec("memory", "add", "--project", project, "--type", "PROJECT_CONTEXT", "--content", "alpha guidance", "--source", "design:21", "--evidence", "issue:21", "--yes")
	exec("task", "init", "--project", project, "--id", "role-memory", "--summary", "alpha")
	search := exec("memory", "search", "alpha", "--project", project)
	before, err := os.ReadFile(filepath.Join(root, "memory.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := exec("roles", "context", "role-memory", "--project", project, "--role", "planner", "--agent", "codex")
	var handoff struct {
		Context taskcontext.Bundle `json:"context"`
	}
	if err = json.Unmarshal([]byte(out), &handoff); err != nil {
		t.Fatal(err)
	}
	var records []memory.Record
	if err = json.Unmarshal([]byte(search), &records); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records, handoff.Context.MemoryRecords) || len(handoff.Context.Memory) != 0 || len(records) != 1 {
		t.Fatalf("context: %s; CLI: %s", out, search)
	}
	after, err := os.ReadFile(filepath.Join(root, "memory.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("role handoff wrote Memory: %v", err)
	}
}

func TestMemoryOutageReportsUnavailableAndSkillWorkflowStillWorks(t *testing.T) {
	configPath, root, project := structuredCLIConfig(t)
	library := t.TempDir()
	skill := filepath.Join(library, "helper")
	if err := os.Mkdir(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: helper\ndescription: offline guidance\n---\nUse local files."), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, append([]byte("library: "+library+"\n"), data...), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeStructuredMemory(t, configPath, "", "memory", "project", "register", "--project", project, "--yes")
	if err != nil {
		t.Fatal(out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "task", "init", "--project", project, "--id", "outage-task", "--summary", "alpha")
	if err != nil {
		t.Fatal(out, err)
	}
	if err = os.Remove(root); err != nil {
		t.Fatal(err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "memory", "search", "alpha", "--project", project)
	if !errors.Is(err, memory.ErrUnavailable) || strings.Contains(out, root) || strings.Contains(err.Error(), root) {
		t.Fatalf("outage: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "search", "helper")
	if err != nil || !strings.Contains(out, "helper") {
		t.Fatalf("Skill inspection blocked: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configPath, "", "roles", "context", "outage-task", "--project", project, "--role", "planner", "--agent", "codex", "--library", library, "--skill", "helper")
	if err != nil {
		t.Fatalf("independent context resources blocked: %s %v", out, err)
	}
	var handoff struct {
		Context taskcontext.Bundle `json:"context"`
	}
	if err = json.Unmarshal([]byte(out), &handoff); err != nil || handoff.Context.MemoryDiagnostic != "unavailable" || len(handoff.Context.Skills) != 1 || len(handoff.Context.Artifacts) != 1 {
		t.Fatalf("outage context: %s %v", out, err)
	}
}

func TestStructuredCLIStatusReportsImplementedOperations(t *testing.T) {
	var status memory.StatusReport
	configuration, root, _ := structuredCLIConfig(t)
	out, err := executeStructuredMemory(t, configuration, "", "memory", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.StructuredCapabilities == nil || !status.StructuredCapabilities.ConditionalUpdate || strings.Contains(out, root) {
		t.Fatalf("structured status: %s", out)
	}
	for _, agent := range status.Agents {
		if agent.State != "unsupported" || len(agent.Capabilities) != 0 {
			t.Fatalf("invented native agent mechanism: %+v", agent)
		}
	}
}

func TestMovedRegisteredProjectCannotAcquireIdentityThroughSymlinkSubstitution(t *testing.T) {
	for _, substitution := range []string{"direct", "ancestor"} {
		t.Run(substitution, func(t *testing.T) {
			configuration, _, _ := structuredCLIConfig(t)
			parent := t.TempDir()
			originalParent := filepath.Join(parent, "original")
			movedParent := filepath.Join(parent, "moved")
			if err := os.Mkdir(originalParent, 0700); err != nil {
				t.Fatal(err)
			}
			original, moved := originalParent, movedParent
			if substitution == "ancestor" {
				original = filepath.Join(originalParent, "repository")
				moved = filepath.Join(movedParent, "repository")
				if err := os.Mkdir(original, 0700); err != nil {
					t.Fatal(err)
				}
			}
			out, err := executeStructuredMemory(t, configuration, "", "memory", "project", "register", "--project", original, "--yes")
			if err != nil {
				t.Fatal(out, err)
			}
			registry, err := memory.OpenProjectRegistry(configuration + ".projects.json")
			if err != nil {
				t.Fatal(err)
			}
			identity, err := registry.Lookup(original)
			if err != nil {
				t.Fatal(err)
			}
			out, err = executeStructuredMemory(t, configuration, "", "memory", "add", "--project", original, "--type", "FACT", "--content", "owned knowledge", "--source", "source", "--yes")
			if err != nil {
				t.Fatal(out, err)
			}
			if err = os.Rename(originalParent, movedParent); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(movedParent, originalParent); err != nil {
				t.Fatal(err)
			}
			if found, err := registry.Lookup(moved); !errors.Is(err, memory.ErrNotFound) {
				t.Fatalf("implicit mapping relocation: %v %v", found, err)
			}
			out, err = executeStructuredMemory(t, configuration, "", "memory", "search", "knowledge", "--project", moved)
			if !errors.Is(err, memory.ErrNotFound) || out != "" {
				t.Fatalf("unregistered relocated read allowed: %s %v", out, err)
			}
			out, err = executeStructuredMemory(t, configuration, "n\n", "memory", "project", "relocate", identity.ID, "--project", moved)
			if err != nil {
				t.Fatal(out, err)
			}
			if _, err = registry.Lookup(moved); !errors.Is(err, memory.ErrNotFound) {
				t.Fatalf("unconfirmed mapping adopted: %v", err)
			}
			out, err = executeStructuredMemory(t, configuration, "", "memory", "project", "relocate", identity.ID, "--project", moved, "--yes")
			if err != nil {
				t.Fatal(out, err)
			}
			found, err := registry.Lookup(moved)
			if err != nil || found.ID != identity.ID {
				t.Fatalf("explicit relocation: %v %v", found, err)
			}
			out, err = executeStructuredMemory(t, configuration, "", "memory", "search", "knowledge", "--project", moved)
			if err != nil {
				t.Fatal(out, err)
			}
			var records []memory.Record
			if err = json.Unmarshal([]byte(out), &records); err != nil || len(records) != 1 || records[0].Owner.ProjectID != identity.ID {
				t.Fatalf("confirmed relocated read: %s %v", out, err)
			}
		})
	}
}

func TestUnavailableProviderRootRetainsTrustedOwnerAndIndependentRoleResources(t *testing.T) {
	for _, failure := range []string{"missing", "regular-file", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			configuration, root, project := structuredCLIConfig(t)
			library := t.TempDir()
			skill := filepath.Join(library, "helper")
			if err := os.Mkdir(skill, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: helper\ndescription: offline resource\n---\nRead local guidance."), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := executeStructuredMemory(t, configuration, "", "memory", "project", "register", "--project", project, "--yes")
			if err != nil {
				t.Fatal(out, err)
			}
			out, err = executeStructuredMemory(t, configuration, "", "task", "init", "--project", project, "--id", "root-outage", "--summary", "alpha")
			if err != nil {
				t.Fatal(out, err)
			}
			if err = os.Remove(root); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "regular-file":
				if err = os.WriteFile(root, []byte("unavailable root"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err = os.Symlink(t.TempDir(), root); err != nil {
					t.Fatal(err)
				}
			}
			out, err = executeStructuredMemory(t, configuration, "", "roles", "context", "root-outage", "--project", project, "--role", "planner", "--agent", "codex", "--library", library, "--skill", "helper")
			if err != nil {
				t.Fatalf("provider health blocked independent resources: %s %v", out, err)
			}
			var handoff struct {
				Context taskcontext.Bundle `json:"context"`
			}
			if err = json.Unmarshal([]byte(out), &handoff); err != nil || handoff.Context.MemoryDiagnostic != "unavailable" || len(handoff.Context.Artifacts) != 1 || len(handoff.Context.Skills) != 1 {
				t.Fatalf("outage context: %s %v", out, err)
			}
			registry, err := memory.OpenProjectRegistry(configuration+".projects.json", root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = registry.Lookup(project); err != nil {
				t.Fatalf("trustworthy mapping unavailable: %v", err)
			}
			if failure != "missing" {
				if _, err = registry.Register(context.Background(), t.TempDir(), true); !errors.Is(err, memory.ErrUnavailable) {
					t.Fatalf("write skipped unavailable-root alias guard: %v", err)
				}
			}
		})
	}
}

func TestStructuredMutationFlagsDescribeOnlyAppliedIntent(t *testing.T) {
	configuration, _, _ := structuredCLIConfig(t)
	legacy := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(legacy, []byte("lesson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ operation, flag string }{{"import", "--content"}, {"import", "--evidence"}, {"forget", "--type"}, {"forget", "--content"}, {"forget", "--source"}, {"forget", "--evidence"}, {"forget", "--layer"}} {
		t.Run(test.operation+test.flag, func(t *testing.T) {
			target := legacy
			if test.operation == "forget" {
				target = "neutral-id"
			}
			args := []string{"memory", test.operation, target, "--user", "operator", test.flag, "ignored-value", "--yes"}
			if test.operation == "import" {
				args = append(args, "--type", "EXPERIENCE", "--source", "declared")
			} else {
				args = append(args, "--expected-version", "1")
			}
			out, err := executeStructuredMemory(t, configuration, "", args...)
			if err == nil || !strings.Contains(err.Error(), "unknown flag") || strings.Contains(out, "Memory mutation plan") {
				t.Fatalf("ineffective flag accepted/displayed: %s %v", out, err)
			}
		})
	}
	out, err := executeStructuredMemory(t, configuration, "", "memory", "search", "--user", "operator")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("unsupported flags mutated provider: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configuration, "", "memory", "import", legacy, "--user", "operator", "--type", "EXPERIENCE", "--source", "declared", "--layer", "ATOMIC", "--yes")
	if err != nil {
		t.Fatal(out, err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatal(out)
	}
	var plan map[string]json.RawMessage
	if err = json.Unmarshal([]byte(lines[1]), &plan); err != nil {
		t.Fatal(out, err)
	}
	if _, present := plan["record"]; present {
		t.Fatalf("import plan displays ineffective record metadata: %s", lines[1])
	}
	out, err = executeStructuredMemory(t, configuration, "", "memory", "search", "--user", "operator")
	if err != nil {
		t.Fatal(out, err)
	}
	var records []memory.Record
	if err = json.Unmarshal([]byte(out), &records); err != nil || len(records) != 1 || records[0].Type != memory.TypeExperience || records[0].Source != "declared" || records[0].Layer != memory.LayerAtomic {
		t.Fatalf("effective import intent lost: %s %v", out, err)
	}
	out, err = executeStructuredMemory(t, configuration, "", "memory", "forget", string(records[0].ID), "--user", "operator", "--expected-version", "1", "--yes")
	if err != nil {
		t.Fatal(out, err)
	}
	lines = strings.Split(out, "\n")
	plan = map[string]json.RawMessage{}
	if err = json.Unmarshal([]byte(lines[1]), &plan); err != nil {
		t.Fatal(out, err)
	}
	if _, present := plan["record"]; present {
		t.Fatalf("forget plan displays ineffective record metadata: %s", lines[1])
	}
}
