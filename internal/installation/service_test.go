package installation_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestPreviewAndApplyInstallSelectedSkills(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "one")
	writeSkill(t, library, "two")
	project := t.TempDir()
	svc := installation.New(library)
	request := installation.Request{
		Project:  project,
		SkillIDs: []string{"one", "two"},
		Targets:  []adapter.Target{adapter.Codex},
	}

	preview, err := svc.Preview("session-a", request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ID == "" || len(preview.Plan.Changes) != 2 {
		t.Fatalf("preview = %#v", preview)
	}
	for _, id := range request.SkillIDs {
		path := filepath.Join(project, ".codex", "skills", id)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("preview created %s: %v", path, err)
		}
	}

	result, err := svc.Apply("session-a", preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.Stale {
		t.Fatalf("apply result = %#v", result)
	}
	for _, id := range request.SkillIDs {
		path := filepath.Join(project, ".codex", "skills", id)
		got, err := os.Readlink(path)
		if err != nil || got != filepath.Join(library, id) || !filepath.IsAbs(got) {
			t.Fatalf("link %s = %q, %v", id, got, err)
		}
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	entry, ok, err := journal.Latest()
	if err != nil || !ok || len(entry.After) != 2 {
		t.Fatalf("journal entry = %#v, ok=%v, err=%v", entry, ok, err)
	}
}

func TestPreviewRejectsInvalidSelectionWithoutChanges(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "known")
	project := t.TempDir()
	svc := installation.New(library)
	tests := []struct {
		name string
		edit func(*installation.Request)
	}{
		{name: "unknown skill", edit: func(r *installation.Request) { r.SkillIDs = []string{"missing"} }},
		{name: "unsupported target", edit: func(r *installation.Request) { r.Targets = []adapter.Target{"other"} }},
		{name: "empty targets", edit: func(r *installation.Request) { r.Targets = nil }},
		{name: "empty selection", edit: func(r *installation.Request) { r.SkillIDs = nil }},
		{name: "invalid project", edit: func(r *installation.Request) { r.Project = filepath.Join(project, "missing") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := installation.Request{Project: project, SkillIDs: []string{"known"}, Targets: []adapter.Target{adapter.Codex}}
			tt.edit(&request)
			if _, err := svc.Preview("session", request); err == nil {
				t.Fatal("expected invalid request error")
			}
			if _, err := os.Lstat(filepath.Join(project, ".codex")); !os.IsNotExist(err) {
				t.Fatalf("project changed: %v", err)
			}
		})
	}
}

func TestApplyIsSessionScopedAndRejectsStalePlan(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "known")
	project := t.TempDir()
	svc := installation.New(library)
	preview, err := svc.Preview("session-a", installation.Request{
		Project: project, SkillIDs: []string{"known"}, Targets: []adapter.Target{adapter.Codex},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply("session-b", preview.ID); !errors.Is(err, installation.ErrPreviewExpired) {
		t.Fatalf("cross-session apply error = %v", err)
	}
	destination := filepath.Join(project, ".codex", "skills", "known")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Apply("session-a", preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || !result.Stale {
		t.Fatalf("stale apply result = %#v", result)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "unmanaged" {
		t.Fatalf("destination = %q, %v", contents, err)
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("stale apply journal = (ok=%v, err=%v), want no entry", ok, err)
	}
}

func TestApplyRejectsChangedConflictContentsEvenWhenPlanTextIsSame(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "known")
	project := t.TempDir()
	destination := filepath.Join(project, ".codex", "skills", "known")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("first version"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := installation.New(library)
	preview, err := svc.Preview("session", installation.Request{
		Project: project, SkillIDs: []string{"known"}, Targets: []adapter.Target{adapter.Codex},
		Options: lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("changed after preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Apply("session", preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stale || result.Applied {
		t.Fatalf("apply result = %#v, want stale", result)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "changed after preview" {
		t.Fatalf("changed conflict contents = %q, %v", contents, err)
	}
}

func TestApplyRejectsChangedContentBehindSkillSymlink(t *testing.T) {
	library := t.TempDir()
	project := t.TempDir()
	skillPath := filepath.Join(library, "known")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "SKILL.md")
	contents := "---\nname: known\ndescription: test skill\n---\noriginal body\n"
	if err := os.WriteFile(external, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(skillPath, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	svc := installation.New(library)
	preview, err := svc.Preview("session", installation.Request{
		Project: project, SkillIDs: []string{"known"}, Targets: []adapter.Target{adapter.Codex},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte(strings.Replace(contents, "original body", "changed body", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Apply("session", preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stale || result.Applied {
		t.Fatalf("apply result = %#v, want stale after symlink target content changed", result)
	}
}

func TestConcurrentAppliesKeepEveryOperationJournalEntry(t *testing.T) {
	const operationCount = 32
	library := t.TempDir()
	project := t.TempDir()
	type applyRequest struct {
		service *installation.Service
		preview installation.Preview
	}
	requests := make([]applyRequest, 0, operationCount)
	services := []*installation.Service{installation.New(library), installation.New(library)}
	for i := 0; i < operationCount; i++ {
		id := fmt.Sprintf("skill-%02d", i)
		writeSkill(t, library, id)
		service := services[i%len(services)]
		preview, err := service.Preview("session", installation.Request{
			Project: project, SkillIDs: []string{id}, Targets: []adapter.Target{adapter.Codex},
		})
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, applyRequest{service: service, preview: preview})
	}

	start := make(chan struct{})
	errs := make(chan error, len(requests))
	var workers sync.WaitGroup
	for _, request := range requests {
		workers.Add(1)
		go func(request applyRequest) {
			defer workers.Done()
			<-start
			result, err := request.service.Apply("session", request.preview.ID)
			if err != nil {
				errs <- err
			} else if !result.Applied {
				errs <- fmt.Errorf("apply %s returned %#v", request.preview.ID, result)
			}
		}(request)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	journalPath := filepath.Join(project, ".skill-manager", "journal.json")
	contents, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var entries []operation.Entry
	if err := json.Unmarshal(contents, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != operationCount {
		t.Fatalf("journal has %d entries, want %d", len(entries), operationCount)
	}
}

func writeSkill(t *testing.T, library, id string) {
	t.Helper()
	path := filepath.Join(library, id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("---\nname: %s\ndescription: test skill\n---\n", id)
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
