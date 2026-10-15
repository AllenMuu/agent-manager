package skillsync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestImportGroupsIdenticalSourcesAndPreservesSources(t *testing.T) {
	fixture := newSyncFixture(t)
	first := filepath.Join(fixture.home, ".claude", "skills", "demo")
	second := filepath.Join(fixture.project, ".agents", "skills", "demo")
	writeSkill(t, first, "same")
	writeSkill(t, second, "same")
	service := fixture.service(t)

	inventory, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sources) != 2 || len(groups) != 1 || len(groups[0].Sources) != 2 {
		t.Fatalf("identical sources did not collapse into one group: sources=%+v groups=%+v", inventory.Sources, groups)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.Apply(preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Metadata.SourceType != "local-web-sync" || len(entries[0].Metadata.SourceOrigins) != 2 {
		t.Fatalf("import provenance = %+v", entries)
	}
	if snapshots, err := os.ReadDir(service.syncRoot); err != nil {
		t.Fatal(err)
	} else {
		for _, snapshot := range snapshots {
			if snapshot.IsDir() {
				t.Errorf("successful import retained rollback snapshot %s", snapshot.Name())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(first, "SKILL.md")); err != nil {
		t.Fatalf("source was modified: %v", err)
	}
	repository, _ := skillrepo.Open(fixture.repository)
	if _, err := repository.Resolve("demo", ""); err != nil {
		t.Fatalf("imported repository version is missing: %v", err)
	}
}

func TestConcurrentImportsSerializeRepositoryPublication(t *testing.T) {
	fixture := newSyncFixture(t)
	writeSkill(t, filepath.Join(fixture.home, ".claude", "skills", "demo-a"), "source a")
	writeSkill(t, filepath.Join(fixture.project, ".agents", "skills", "demo-b"), "source b")
	first := fixture.service(t)
	second := fixture.service(t)
	_, groups, err := first.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	previews := make(map[string]Preview, len(groups))
	for _, group := range groups {
		preview, err := first.Preview([]string{group.ID}, nil)
		if err != nil {
			t.Fatal(err)
		}
		previews[group.Identifier] = preview
	}

	firstAtPublish := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondPublished := make(chan struct{})
	first.BeforeImportPublish = func(int, skillrepo.Entry) error {
		close(firstAtPublish)
		<-releaseFirst
		return nil
	}
	second.BeforeImportPublish = func(int, skillrepo.Entry) error {
		close(secondPublished)
		return nil
	}
	type applyResult struct{ err error }
	results := make(chan applyResult, 2)
	go func() {
		_, err := first.Apply(previews["demo-a"])
		results <- applyResult{err: err}
	}()
	select {
	case <-firstAtPublish:
	case <-time.After(2 * time.Second):
		t.Fatal("first import did not reach repository publication")
	}
	go func() {
		_, err := second.Apply(previews["demo-b"])
		results <- applyResult{err: err}
	}()

	secondPublishedEarly := false
	select {
	case <-secondPublished:
		secondPublishedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	firstResult, secondResult := <-results, <-results
	if firstResult.err != nil || secondResult.err != nil {
		t.Fatalf("concurrent import errors: first=%v second=%v", firstResult.err, secondResult.err)
	}
	if secondPublishedEarly {
		t.Fatal("second import published while the first held the repository transaction")
	}
	select {
	case <-secondPublished:
	case <-time.After(2 * time.Second):
		t.Fatal("second import did not publish after the first transaction released its lock")
	}
	repository, _ := skillrepo.Open(fixture.repository)
	for _, identifier := range []string{"demo-a", "demo-b"} {
		if _, err := repository.Resolve(identifier, ""); err != nil {
			t.Fatalf("concurrent import lost %s: %v", identifier, err)
		}
	}
}

func TestImportDivergentVariantsRequireAndHonorCurrentChoice(t *testing.T) {
	fixture := newSyncFixture(t)
	global := filepath.Join(fixture.home, ".claude", "skills", "demo")
	project := filepath.Join(fixture.project, ".agents", "skills", "demo")
	writeSkill(t, global, "global version")
	writeSkill(t, project, "project version")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("variant count = %d, want 2", len(groups))
	}
	ids := []string{groups[0].ID, groups[1].ID}
	preview, err := service.Preview(ids, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.CurrentChoiceRequired) != 1 || preview.CurrentChoiceRequired[0] != "demo" {
		t.Fatalf("preview choices = %+v", preview.CurrentChoiceRequired)
	}
	if _, err := service.Apply(preview); !errors.Is(err, ErrNeedsChoice) {
		t.Fatalf("apply without current choice = %v", err)
	}
	choices := map[string]string{"demo": groups[1].ID}
	preview, err = service.Preview(ids, choices)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.Apply(preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("import entries = %d, want both variants", len(entries))
	}
	repository, _ := skillrepo.Open(fixture.repository)
	current, err := repository.Resolve("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if current.Key != groups[1].VersionKey {
		t.Fatalf("current key = %s, want explicit choice %s", current.Key, groups[1].VersionKey)
	}
}

func TestImportIsNoOpForExistingChecksumAndRejectsStaleRepository(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(fixture.home, ".codex", "skills", "demo")
	writeSkill(t, source, "same")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(preview); err != nil {
		t.Fatal(err)
	}
	preview, err = service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Groups[0].AlreadyPresent {
		t.Fatal("matching repository version was not marked as present")
	}
	entries, err := service.Apply(preview)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing version apply = %v, %v", entries, err)
	}

	other := filepath.Join(fixture.home, ".agent", "skills", "new-skill")
	writeSkill(t, other, "new")
	_, groups, err = service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	var selected SourceGroup
	for _, group := range groups {
		if group.Identifier == "new-skill" {
			selected = group
		}
	}
	preview, err = service.Preview([]string{selected.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(fixture.repository, "new-skill")
	if err := os.MkdirAll(conflictPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(preview); !errors.Is(err, ErrStalePreview) {
		t.Fatalf("stale repository apply = %v", err)
	}
}

func TestImportBatchFailureRollsBackEveryPublishedIdentity(t *testing.T) {
	fixture := newSyncFixture(t)
	first := filepath.Join(fixture.home, ".claude", "skills", "first")
	second := filepath.Join(fixture.project, ".agents", "skills", "second")
	writeSkill(t, first, "first")
	writeSkill(t, second, "second")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]string, 0, len(groups))
	for _, group := range groups {
		selected = append(selected, group.ID)
	}
	preview, err := service.Preview(selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.BeforeImportPublish = func(index int, _ skillrepo.Entry) error {
		if index == 0 {
			return errors.New("injected repository publication failure")
		}
		return nil
	}
	if _, err := service.Apply(preview); err == nil {
		t.Fatal("injected failure unexpectedly succeeded")
	}
	repository, _ := skillrepo.Open(fixture.repository)
	for _, identifier := range []string{"first", "second"} {
		if _, err := repository.Resolve(identifier, ""); !errors.Is(err, skillrepo.ErrNotFound) {
			t.Fatalf("batch rollback left %s in the repository: %v", identifier, err)
		}
	}
	entries, err := os.ReadDir(service.syncRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("failed batch left import snapshot directory %s", entry.Name())
		}
	}
}

type syncFixture struct{ home, project, repository string }

func newSyncFixture(t *testing.T) syncFixture {
	t.Helper()
	root := t.TempDir()
	fixture := syncFixture{home: filepath.Join(root, "home"), project: filepath.Join(root, "project"), repository: filepath.Join(root, "repo")}
	for _, path := range []string{fixture.home, fixture.project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f syncFixture) service(t *testing.T) *Service {
	t.Helper()
	service, err := New(f.repository, f.home, f.project, operation.GlobalJournal(f.home), operation.ProjectJournal(f.project))
	if err != nil {
		t.Fatal(err)
	}
	return service
}
