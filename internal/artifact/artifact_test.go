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
