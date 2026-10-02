//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package cli_test

import (
	"bytes"
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
