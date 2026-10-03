package diagnostic_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/diagnostic"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestScanReportsCatalogOrphanUnsupportedAndGitGuidance(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	mustMkdir(t, filepath.Join(library, "invalid"))
	mustMkdir(t, filepath.Join(project, ".codex", "skills"))
	mustLink(t, filepath.Join(library, "missing"), filepath.Join(project, ".codex", "skills", "missing"))
	mustMkdir(t, filepath.Join(project, ".cursor", "skills", "other"))
	mustWrite(t, filepath.Join(project, ".git", "index"), "")

	findings, err := diagnostic.Scan(library, project)
	if err != nil {
		t.Fatal(err)
	}
	joined := findingsText(findings)
	for _, want := range []string{"invalid catalog entry", "orphaned managed link", "unsupported agent", "Git"} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings %q do not contain %q", joined, want)
		}
	}
}

func TestScanReportsAdapterCapabilitiesAndUnmanagedResources(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(library, "managed", "SKILL.md"), "---\nname: managed\ndescription: managed\n---\n")
	mustWrite(t, filepath.Join(library, "managed", "check.sh"), "#!/bin/sh\nprintf executed > marker\n")
	if err := os.Chmod(filepath.Join(library, "managed", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(project, ".codex", "skills", "unmanaged", "SKILL.md"), "---\nname: unmanaged\ndescription: unmanaged\n---\n")
	mustLink(t, filepath.Join(library, "orphan"), filepath.Join(project, ".codex", "skills", "orphan"))
	mustMkdir(t, filepath.Join(project, ".other-agent", "skills"))

	before := snapshotTree(t, root)
	findings, err := diagnostic.Scan(library, project)
	if err != nil {
		t.Fatal(err)
	}
	joined := findingsText(findings)
	for _, want := range []string{"adapter capabilities", "filesystem-read", "unmanaged resource", "orphaned managed link", "unsupported agent skill location"} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings %q do not contain %q", joined, want)
		}
	}
	if got := snapshotTree(t, root); got != before {
		t.Fatalf("diagnostics mutated filesystem:\nbefore=%s\nafter=%s", before, got)
	}
	if _, err := os.Lstat(filepath.Join(project, ".skill-manager", "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("diagnostics created journal: %v", err)
	}
}

func TestScanTreatsPiAsSupportedLocation(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(library, "placeholder", "SKILL.md"), "---\nname: placeholder\ndescription: placeholder\n---\n")
	mustMkdir(t, filepath.Join(project, ".pi", "skills"))

	findings, err := diagnostic.Scan(library, project)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Path == filepath.Join(project, ".pi") && strings.Contains(finding.Message, "unsupported agent") {
			t.Fatalf("Pi location was reported as unsupported: %#v", finding)
		}
	}
}

func TestScanReportsTrackedIgnoredAndUntrackedManagedLinks(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	for _, id := range []string{"tracked", "ignored", "untracked"} {
		mustWrite(t, filepath.Join(lib, id, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
		mustLink(t, filepath.Join(lib, id), filepath.Join(project, ".codex", "skills", id))
	}
	runGit(t, project, "init")
	runGit(t, project, "config", "user.email", "test@example.com")
	runGit(t, project, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(project, ".gitignore"), "/.codex/skills/ignored\n")
	runGit(t, project, "add", ".codex/skills/tracked")
	runGit(t, project, "commit", "-m", "fixture")
	findings, err := diagnostic.Scan(lib, project)
	if err != nil {
		t.Fatal(err)
	}
	text := findingsText(findings)
	for _, want := range []string{"tracked", "ignored", "would be tracked"} {
		if !strings.Contains(text, want) {
			t.Fatalf("findings=%q missing %q", text, want)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestAddGitignorePreservesNewlineAndRejectsSymlink(t *testing.T) {
	project := t.TempDir()
	link := filepath.Join(project, ".codex", "skills", "demo")
	target := filepath.Join(t.TempDir(), "demo")
	mustWrite(t, filepath.Join(target, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	mustLink(t, target, link)
	ignore := filepath.Join(project, ".gitignore")
	mustWrite(t, ignore, "existing")
	if _, err := diagnostic.AddGitignore(project, []string{link}, func(operation.Plan) bool { return true }, operation.New(filepath.Join(project, "j.json"))); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(ignore)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing\n/.codex/skills/demo\n" {
		t.Fatalf("ignore=%q", contents)
	}
	project = t.TempDir()
	ignore = filepath.Join(project, ".gitignore")
	mustLink(t, filepath.Join(project, "target"), ignore)
	if _, err := diagnostic.AddGitignore(project, []string{filepath.Join(project, ".codex", "skills", "demo")}, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestAddGitignoreRejectsUnmanagedAndSkipsExistingRule(t *testing.T) {
	project := t.TempDir()
	unmanaged := filepath.Join(project, ".codex", "skills", "local")
	mustMkdir(t, unmanaged)
	if _, err := diagnostic.AddGitignore(project, []string{unmanaged}, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("unmanaged accepted")
	}
	link := filepath.Join(project, ".codex", "skills", "demo")
	target := filepath.Join(t.TempDir(), "demo")
	mustWrite(t, filepath.Join(target, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	mustLink(t, target, link)
	mustWrite(t, filepath.Join(project, ".gitignore"), "/.codex/skills/demo\n")
	called := false
	plan, err := diagnostic.AddGitignore(project, []string{link}, func(operation.Plan) bool { called = true; return true })
	if err != nil || called || len(plan.Changes) != 0 {
		t.Fatalf("plan=%#v called=%v err=%v", plan, called, err)
	}
}

func TestAddGitignoreAcceptsPiManagedLink(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(library, "demo", "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	link := filepath.Join(project, ".pi", "skills", "demo")
	mustLink(t, filepath.Join(library, "demo"), link)

	if _, err := diagnostic.AddGitignore(project, []string{link}, func(operation.Plan) bool { return true }); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(project, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "/.pi/skills/demo\n"; got != want {
		t.Fatalf("ignore = %q, want %q", got, want)
	}
}

func TestAddGitignoreTreatsManagedPathsLiterally(t *testing.T) {
	for _, tc := range []struct{ identifier, neighbor string }{
		{"demo*", "demo-other"}, {"demo?", "demox"}, {"demo[ab]", "demoa"}, {"demo ", "demo"},
	} {
		t.Run(tc.identifier, func(t *testing.T) {
			project := t.TempDir()
			target := filepath.Join(t.TempDir(), tc.identifier)
			mustWrite(t, filepath.Join(target, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
			link := filepath.Join(project, ".codex", "skills", tc.identifier)
			mustLink(t, target, link)
			neighbor := filepath.Join(".codex", "skills", tc.neighbor, "SKILL.md")
			mustWrite(t, filepath.Join(project, neighbor), "unmanaged")
			runGit(t, project, "init")
			if _, err := diagnostic.AddGitignore(project, []string{link}, func(operation.Plan) bool { return true }); err != nil {
				t.Fatal(err)
			}
			runGit(t, project, "check-ignore", "-q", "--", filepath.Join(".codex", "skills", tc.identifier))
			err := exec.Command("git", "-C", project, "check-ignore", "-q", "--", neighbor).Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				t.Fatalf("neighbor was ignored by managed rule: %v", err)
			}
		})
	}
}

func TestAddGitignoreRejectsLineSeparatorsBeforeConfirmation(t *testing.T) {
	for _, id := range []string{"demo\n*", "demo\r*"} {
		t.Run(id, func(t *testing.T) {
			project := t.TempDir()
			target := filepath.Join(t.TempDir(), id)
			mustWrite(t, filepath.Join(target, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
			link := filepath.Join(project, ".codex", "skills", id)
			mustLink(t, target, link)
			confirmed := false
			_, err := diagnostic.AddGitignore(project, []string{link}, func(operation.Plan) bool { confirmed = true; return true })
			if err == nil || confirmed {
				t.Fatalf("line separator accepted or confirmation requested: confirmed=%v err=%v", confirmed, err)
			}
			for _, path := range []string{filepath.Join(project, ".gitignore"), filepath.Join(project, ".skill-manager")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("refusal changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestDeleteRevalidatesConcurrentReplacementAndCatalogIO(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	path := filepath.Join(lib, "demo")
	mustWrite(t, filepath.Join(path, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	_, err := diagnostic.DeleteLibrarySkill(lib, "demo", true, func(operation.Plan) bool {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, path)
		return true
	})
	if err == nil {
		t.Fatal("concurrent replacement accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := diagnostic.DeleteLibrarySkill(filepath.Join(root, "missing", "library"), "demo", true, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("catalog I/O accepted")
	}
}

func TestReconcileRelinksOrphanOnlyAfterConfirmation(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "new-library")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(library, "demo", "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	link := filepath.Join(project, ".codex", "skills", "demo")
	old := filepath.Join(root, "old-library", "demo")
	mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	mustLink(t, old, link)
	journal := operation.New(filepath.Join(root, "journal.json"))
	after, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(old)); err != nil {
		t.Fatal(err)
	}
	if _, err := diagnostic.Reconcile(library, project, journal, func(operation.Plan) bool { return false }); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("Reconcile() = %v", err)
	}
	if got, _ := os.Readlink(link); !strings.Contains(got, "old-library") {
		t.Fatalf("link changed without confirmation: %q", got)
	}
	if _, err := diagnostic.Reconcile(library, project, journal, func(operation.Plan) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != filepath.Join(library, "demo") {
		t.Fatalf("link = %q", got)
	}
}

func TestReconcilePreviewDetailsNameReplacementSource(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "new-library")
	project := filepath.Join(root, "project")
	old := filepath.Join(root, "old-library", "demo")
	newSource := filepath.Join(library, "demo")
	mustWrite(t, filepath.Join(newSource, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	link := filepath.Join(project, ".codex", "skills", "demo")
	mustLink(t, old, link)
	journal := operation.New(filepath.Join(root, "journal.json"))
	after, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(old)); err != nil {
		t.Fatal(err)
	}
	var preview operation.Plan
	if _, err := diagnostic.Reconcile(library, project, journal, func(plan operation.Plan) bool {
		preview = plan
		return false
	}); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("Reconcile() = %v, want confirmation refusal", err)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Detail != newSource {
		t.Fatalf("reconcile preview = %#v, want replacement source %q", preview.Changes, newSource)
	}
}

func TestScanAndReconcileLeaveAmbiguousDanglingLinkUntouched(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(library, "demo", "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	link := filepath.Join(project, ".codex", "skills", "demo")
	mustLink(t, filepath.Join(root, "unrelated", "demo"), link)
	findings, err := diagnostic.Scan(library, project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findingsText(findings), "ambiguous dangling link") {
		t.Fatalf("findings = %q", findingsText(findings))
	}
	plan, err := diagnostic.Reconcile(library, project, operation.New(filepath.Join(project, ".skill-manager", "journal.json")), func(operation.Plan) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("ambiguous link was planned: %#v", plan)
	}
}

func TestScanRecognizesJournalOwnedMovedLibraryLink(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old", "demo")
	lib := filepath.Join(root, "new")
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	mustWrite(t, filepath.Join(lib, "demo", "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	link := filepath.Join(project, ".codex", "skills", "demo")
	mustLink(t, old, link)
	j := operation.New(filepath.Join(root, "journal.json"))
	after, err := j.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(old)); err != nil {
		t.Fatal(err)
	}
	findings, err := diagnostic.Scan(lib, project, j)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findingsText(findings), "orphaned managed link") {
		t.Fatalf("findings=%q", findingsText(findings))
	}
}

func TestReconcileSecondPublicationFailureRollsBack(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	j := operation.New(filepath.Join(root, "j.json"))
	var links []string
	for _, id := range []string{"a", "b"} {
		mustWrite(t, filepath.Join(lib, id, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
		old := filepath.Join(root, "old", id)
		mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
		link := filepath.Join(project, ".codex", "skills", id)
		mustLink(t, old, link)
		links = append(links, link)
	}
	after, captureErr := j.Capture(links)
	if captureErr != nil {
		t.Fatal(captureErr)
	}
	if err := j.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	diagnostic.BeforeReconcilePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("second")
		}
		return nil
	}
	defer func() { diagnostic.BeforeReconcilePublish = nil }()
	_, err := diagnostic.Reconcile(lib, project, j, func(operation.Plan) bool { return true })
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, id := range []string{"a", "b"} {
		target, _ := os.Readlink(filepath.Join(project, ".codex", "skills", id))
		if target != filepath.Join(root, "old", id) {
			t.Fatalf("%s not rolled back: %q", id, target)
		}
	}
}

func TestReconcileDetectsLaterLinkReplacementAndRestoresPreState(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	j := operation.New(filepath.Join(root, "j.json"))
	var links []string
	for _, id := range []string{"a", "b"} {
		mustWrite(t, filepath.Join(lib, id, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
		old := filepath.Join(root, "old", id)
		mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
		link := filepath.Join(project, ".codex", "skills", id)
		mustLink(t, old, link)
		links = append(links, link)
	}
	after, _ := j.Capture(links)
	if err := j.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	diagnostic.BeforeReconcilePublish = func(path string) error {
		calls++
		if calls == 2 {
			if err := os.Remove(path); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(root, "concurrent", "b"), path)
		}
		return nil
	}
	defer func() { diagnostic.BeforeReconcilePublish = nil }()
	if _, err := diagnostic.Reconcile(lib, project, j, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("replacement accepted")
	}
	target, _ := os.Readlink(filepath.Join(project, ".codex", "skills", "a"))
	if target != filepath.Join(root, "old", "a") {
		t.Fatalf("a=%q", target)
	}
	target, _ = os.Readlink(filepath.Join(project, ".codex", "skills", "b"))
	if target != filepath.Join(root, "concurrent", "b") {
		t.Fatalf("concurrent b was overwritten: %q", target)
	}
}

func TestReconcileRejectsProjectParentSwapBeforeAnchoredPublish(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	j := operation.New(filepath.Join(root, "j.json"))
	mustWrite(t, filepath.Join(lib, "a", "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	old := filepath.Join(root, "old", "a")
	mustWrite(t, filepath.Join(old, "SKILL.md"), "---\nname: d\ndescription: d\n---\n")
	path := filepath.Join(project, ".codex", "skills", "a")
	mustLink(t, old, path)
	after, _ := j.Capture([]string{path})
	if err := j.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "external", "skills")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	externalTarget := filepath.Join(root, "external", "owner")
	if err := os.MkdirAll(externalTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(external, "a")
	if err := os.Symlink(externalTarget, externalPath); err != nil {
		t.Fatal(err)
	}
	called := false
	diagnostic.BeforeReconcilePublish = func(string) error {
		if called {
			return nil
		}
		called = true
		codexRoot := filepath.Join(project, ".codex")
		if err := os.Rename(codexRoot, codexRoot+".real"); err != nil {
			return err
		}
		return os.Symlink(filepath.Dir(external), codexRoot)
	}
	defer func() { diagnostic.BeforeReconcilePublish = nil }()
	if _, err := diagnostic.Reconcile(lib, project, j, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("Reconcile unexpectedly succeeded after parent swap")
	}
	if got, err := os.Readlink(externalPath); err != nil || got != externalTarget {
		t.Fatalf("external owner changed: %q, %v", got, err)
	}
}

func TestDeleteLibrarySkillRequiresForceConfirmation(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	mustWrite(t, filepath.Join(library, "demo", "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	if _, err := diagnostic.DeleteLibrarySkill(library, "demo", false, func(operation.Plan) bool { return true }); err == nil {
		t.Fatal("DeleteLibrarySkill succeeded without force")
	}
	if _, err := diagnostic.DeleteLibrarySkill(library, "demo", true, func(operation.Plan) bool { return false }); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("DeleteLibrarySkill = %v", err)
	}
	if _, err := diagnostic.DeleteLibrarySkill(library, "demo", true, func(operation.Plan) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(library, "demo")); !os.IsNotExist(err) {
		t.Fatalf("library skill remains: %v", err)
	}
}

func findingsText(findings []diagnostic.Finding) string {
	var text []string
	for _, f := range findings {
		text = append(text, f.Message)
	}
	return strings.Join(text, "\n")
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		snapshot.WriteString(rel + "|" + info.Mode().String() + "|")
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot.WriteString(target)
		} else if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot.Write(contents)
		}
		snapshot.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %q: %v", root, err)
	}
	return snapshot.String()
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustLink(t *testing.T, target, path string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
