package artifact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
)

func TestParseRetainsUnknownFieldsAndRendersCompleteArtifact(t *testing.T) {
	doc, err := artifact.Parse([]byte(`
version: v1
kind: intent
id: task-1
created_at: 2026-09-26T10:00:00+08:00
project:
  root: /tmp/project
source:
  actor: human
links:
  task: task-1
summary: Add durable artifacts
future_field:
  enabled: true
`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "future_field:") || !strings.Contains(string(data), "enabled: true") {
		t.Fatalf("round-trip lost unknown field: %s", data)
	}
	rendered, err := doc.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "# intent: task-1") || !strings.Contains(rendered, "future_field") {
		t.Fatalf("rendered artifact = %q", rendered)
	}
}

func TestValidateRejectsInvalidArtifactAndLessonPayload(t *testing.T) {
	for name, data := range map[string]string{
		"bad version": "version: v2\nkind: intent\nid: task-1\ncreated_at: 2026-09-26T10:00:00Z\nsummary: x\n",
		"unsafe id":   "version: v1\nkind: intent\nid: ../task\ncreated_at: 2026-09-26T10:00:00Z\nsummary: x\n",
		"bad status":  "version: v1\nkind: verification\nid: task-1\ncreated_at: 2026-09-26T10:00:00Z\nstatus: unknown\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := artifact.Parse([]byte(data)); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
	_, err := artifact.Parse([]byte(`
version: v1
kind: lessons
id: task-1
created_at: 2026-09-26T10:00:00Z
project:
  root: /tmp/project
source:
  actor: human
links:
  task: task-1
items:
  - type: unknown
    scope: project
    content: x
    confidence: high
`))
	if err == nil {
		t.Fatal("unknown lesson type was accepted")
	}
}

func TestArtifactPolicySnapshotReferenceRoundTripsAndRejectsUnknownFields(t *testing.T) {
	doc, err := artifact.Parse([]byte(`version: v1
kind: verification
id: task-1
created_at: 2026-09-26T10:00:00Z
project: {root: /tmp/project}
source: {actor: human}
links: {task: task-1}
status: pass
checks: [{name: tests, status: pass}]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := artifact.PolicySnapshotReference{RunID: "run-1", PolicyID: "safe-policy", Version: "v1", Hash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ResolvedAt: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)}
	if err := doc.SetPolicySnapshot(want); err != nil {
		t.Fatal(err)
	}
	serialized, err := doc.YAML()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := artifact.Parse(serialized)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := loaded.PolicySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != want {
		t.Fatalf("policy snapshot = %#v, present %t, want %#v", got, ok, want)
	}
	loaded.Set("policy_snapshot", map[string]any{"policy_id": "safe-policy", "version": "v1", "hash": want.Hash, "resolved_at": want.ResolvedAt, "future_control": "must-not-drop"})
	if _, err := loaded.YAML(); err == nil {
		t.Fatal("artifact validation silently accepted an unknown policy snapshot field")
	}
}

func TestValidateRejectsMalformedStagePayloadAndMissingRelationships(t *testing.T) {
	plan := artifact.New(artifact.Plan, "task-1", "/tmp/project", time.Now())
	plan.Set("steps", "not a list")
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "plan steps") {
		t.Fatalf("malformed plan accepted: %v", err)
	}
	plan.Set("steps", []map[string]any{{"id": "P1", "description": "Do work", "verification": "go test ./..."}})
	if err := plan.Validate(); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	delete(plan.Values, "links")
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "links.task") {
		t.Fatalf("missing task relationship accepted: %v", err)
	}
	plan.Set("links", map[string]any{"task": "task-1"})
	spec := artifact.New(artifact.Spec, "task-1", "/tmp/project", time.Now())
	spec.Set("decisions", []any{"not a decision"})
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "spec decisions") {
		t.Fatalf("malformed spec accepted: %v", err)
	}
	verification := artifact.New(artifact.Verification, "task-1", "/tmp/project", time.Now())
	verification.Set("status", "pass")
	verification.Set("checks", []any{map[string]any{"name": "test", "status": "unknown"}})
	if err := verification.Validate(); err == nil || !strings.Contains(err.Error(), "verification check") {
		t.Fatalf("malformed check accepted: %v", err)
	}
}

func TestEnvelopeRejectsYAMLDateInRequiredStringFields(t *testing.T) {
	for name, field := range map[string]string{
		"actor": "source:\n  actor: 2026-09-26\nproject:\n  root: .\n",
		"root":  "source:\n  actor: human\nproject:\n  root: 2026-09-26\n",
	} {
		t.Run(name, func(t *testing.T) {
			data := "version: v1\nkind: intent\nid: task-1\ncreated_at: 2026-09-26T10:00:00Z\n" + field + "links:\n  task: task-1\nsummary: Test envelope\n"
			if _, err := artifact.Parse([]byte(data)); err == nil || !strings.Contains(err.Error(), "must be a string") {
				t.Fatalf("non-string %s accepted: %v", name, err)
			}
		})
	}
}

func TestStoreUsesDeterministicTaskLayoutAndRejectsOverwrite(t *testing.T) {
	project := t.TempDir()
	store, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-1", project, time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC))
	intent.Set("summary", "test task")
	path, err := store.Init("task-1", intent)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".agents", "tasks", "task-1", "intent.yaml")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := store.Init("task-1", intent); err == nil {
		t.Fatal("second Init() succeeded")
	}
	entries, err := store.List("task-1")
	if err != nil || len(entries) != 1 || entries[0].Kind != artifact.Intent {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestLessonsExposeTypedItemsForExplicitPromotion(t *testing.T) {
	doc := artifact.New(artifact.Lessons, "task-1", "/tmp/project", time.Now())
	doc.Set("items", []map[string]string{{"type": "lesson", "scope": "project", "content": "keep tests deterministic", "confidence": "high"}})
	items, err := doc.LessonItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Content != "keep tests deterministic" {
		t.Fatalf("items = %#v", items)
	}
}
