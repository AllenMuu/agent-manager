package skillsync

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestCheckUpdatesReportsRemoteAndLocalStatusesWithoutMutation(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client

	localSkill := filepath.Join(fixture.home, ".agents", "skills", "local-demo")
	writeSkill(t, localSkill, "local only")
	before, err := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}

	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]UpdateItem{}
	for _, item := range checked.Items {
		statuses[item.Identifier] = item
	}
	if item := statuses["remote-demo"]; item.Status != UpdateStatusAvailable || !item.CanUpdate || item.UpstreamHash != fixture.updatedHash {
		t.Fatalf("remote status = %+v, want available update to %s", item, fixture.updatedHash)
	}
	if item := statuses["local-demo"]; item.Status != UpdateStatusLocal || item.CanUpdate {
		t.Fatalf("local status = %+v, want local and not updatable", item)
	}
	if requests := fixture.requests.Load(); requests != 1 {
		t.Fatalf("upstream requests = %d, want one request for the remote Skill only", requests)
	}
	after, _ := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	lockAfter, _ := os.ReadFile(fixture.lockPath)
	if string(before) != string(after) || string(lockBefore) != string(lockAfter) {
		t.Fatal("checking for updates changed the Skill or lock file")
	}
}

func TestPrepareApplyAndUndoUpstreamSkillUpdate(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client

	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || !checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v; want available update", checked, err)
	}
	prepared, err := fixture.service.PrepareUpdates(context.Background(), []string{checked.Items[0].ID})
	if err != nil || len(prepared.Preview.Items) != 1 {
		t.Fatalf("PrepareUpdates = %+v, err=%v", prepared.Preview, err)
	}
	if prepared.Preview.Items[0].CurrentHash != fixture.currentHash || prepared.Preview.Items[0].UpstreamHash != fixture.updatedHash {
		t.Fatalf("preview hashes = %+v", prepared.Preview.Items[0])
	}

	journal := operation.New(filepath.Join(fixture.home, ".agent-manager", "journal", "upstream-skills.json"))
	if err := fixture.service.ApplyUpdates(context.Background(), prepared, journal); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	if err != nil || !strings.Contains(string(updated), "new upstream instructions") {
		t.Fatalf("updated Skill = %q, err=%v", updated, err)
	}
	lockContents, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Skills map[string]installRecord `json:"skills"`
	}
	if err := json.Unmarshal(lockContents, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Skills["remote-demo"].SkillFolderHash != fixture.updatedHash || lock.Skills["remote-demo"].UpdatedAt == "" {
		t.Fatalf("updated global lock entry = %+v", lock.Skills["remote-demo"])
	}

	available, err := journal.UndoAvailable()
	if err != nil || !available {
		t.Fatalf("UndoAvailable = %v, err=%v", available, err)
	}
	if err := journal.UndoLatest(func(plan operation.Plan) bool { return plan.Operation == "undo update upstream Skills" }); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	if err != nil || string(restored) != string(fixture.currentContent) {
		t.Fatalf("Undo restored Skill = %q, err=%v", restored, err)
	}
	lockRestored, err := os.ReadFile(fixture.lockPath)
	if err != nil || string(lockRestored) != string(fixture.lockContents) {
		t.Fatalf("Undo restored lock file = %q, err=%v", lockRestored, err)
	}
}

func TestProjectSkillsCLILockUsesContentHashAndPreservesLocalExcludedFiles(t *testing.T) {
	fixture := newUpstreamFixture(t, true)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	localDependency := filepath.Join(fixture.skillPath, "node_modules", "local-package", "index.js")
	if err := os.MkdirAll(filepath.Dir(localDependency), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localDependency, []byte("keep local dependency"), 0o644); err != nil {
		t.Fatal(err)
	}

	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || !checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v", checked, err)
	}
	prepared, err := fixture.service.PrepareUpdates(context.Background(), []string{checked.Items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(fixture.home, ".agent-manager", "journal", "upstream-skills.json"))
	if err := fixture.service.ApplyUpdates(context.Background(), prepared, journal); err != nil {
		t.Fatal(err)
	}
	dependency, err := os.ReadFile(localDependency)
	if err != nil || string(dependency) != "keep local dependency" {
		t.Fatalf("local dependency after update = %q, err=%v", dependency, err)
	}
	contents, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Skills map[string]installRecord `json:"skills"`
	}
	if err := json.Unmarshal(contents, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Skills["remote-demo"].ComputedHash != fixture.updatedHash {
		t.Fatalf("project lock computedHash = %q, want %q", lock.Skills["remote-demo"].ComputedHash, fixture.updatedHash)
	}
}

func TestApplyRejectsLocalChangesMadeAfterUpdatePreview(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || !checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v", checked, err)
	}
	prepared, err := fixture.service.PrepareUpdates(context.Background(), []string{checked.Items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	localChange := []byte("---\nname: remote-demo\ndescription: changed after preview\n---\n\n# Instructions\n")
	if err := os.WriteFile(filepath.Join(fixture.skillPath, "SKILL.md"), localChange, 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(fixture.home, ".agent-manager", "journal", "upstream-skills.json"))
	if err := fixture.service.ApplyUpdates(context.Background(), prepared, journal); !errors.Is(err, ErrStaleUpdatePlan) {
		t.Fatalf("ApplyUpdates = %v, want stale update plan", err)
	}
	actual, err := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	if err != nil || string(actual) != string(localChange) {
		t.Fatalf("local Skill after stale apply = %q, err=%v", actual, err)
	}
}

func TestApplyRevalidatedUpdatesStopsWhenRequestExpiresWaitingForOperationLock(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client

	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || !checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v", checked, err)
	}
	prepared, err := fixture.service.PrepareUpdates(context.Background(), []string{checked.Items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	validated, err := fixture.service.RevalidateUpdates(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}

	journal := operation.New(filepath.Join(fixture.home, ".agent-manager", "journal", "upstream-skills.json"))
	unlock, err := journal.LockOperation()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		result <- fixture.service.ApplyRevalidatedUpdates(ctx, validated, journal)
	}()
	<-started
	select {
	case err := <-result:
		_ = unlock()
		t.Fatalf("apply returned while another process held the operation lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyRevalidatedUpdates error = %v, want context cancellation", err)
	}
	current, err := os.ReadFile(filepath.Join(fixture.skillPath, "SKILL.md"))
	if err != nil || string(current) != string(fixture.currentContent) {
		t.Fatalf("Skill changed after request cancellation: content=%q err=%v", current, err)
	}
}

func TestCheckUpdatesDoesNotRequestModifiedLocalInstall(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	localEdit := []byte("---\nname: remote-demo\ndescription: edited locally\n---\n\n# Instructions\n")
	if err := os.WriteFile(filepath.Join(fixture.skillPath, "SKILL.md"), localEdit, 0o644); err != nil {
		t.Fatal(err)
	}
	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || checked.Items[0].Status != UpdateStatusModified || checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v; want locally modified status", checked, err)
	}
	if requests := fixture.requests.Load(); requests != 0 {
		t.Fatalf("upstream requests = %d, want zero for locally modified install", requests)
	}
}

func TestCheckUpdatesDoesNotFollowGitHubRedirect(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	fixture.redirect.Store(true)
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || checked.Items[0].Status != UpdateStatusFailed || checked.Items[0].CanUpdate {
		t.Fatalf("redirect check = %+v, err=%v; want failed status", checked, err)
	}
}

func TestCheckUpdatesReportsUpToDateWhenGitTreeHashMatches(t *testing.T) {
	fixture := newUpstreamFixture(t, false)
	currentTreeHash := gitTreeHash([]remoteFile{{Path: "SKILL.md", Mode: "100644", Content: fixture.currentContent}})
	fixture.updatedTree.Tree[0].SHA = hex.EncodeToString(currentTreeHash[:])
	fixture.updatedContent = fixture.currentContent
	fixture.updatedHash = hex.EncodeToString(currentTreeHash[:])
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || checked.Items[0].Status != UpdateStatusCurrent || checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v; want up-to-date", checked, err)
	}
}

func TestCheckUpdatesReportsMultiFileProjectHashAsUnsupported(t *testing.T) {
	fixture := newUpstreamFixture(t, true)
	extra := filepath.Join(fixture.skillPath, "README.md")
	if err := os.WriteFile(extra, []byte("another tracked file"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.service.github.apiBase = fixture.server.URL
	fixture.service.github.rawBase = fixture.server.URL
	fixture.service.github.client = newGitHubClient().client
	checked, err := fixture.service.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 1 || checked.Items[0].Status != UpdateStatusUnsupported || checked.Items[0].CanUpdate {
		t.Fatalf("CheckUpdates = %+v, err=%v; want unsupported project hash", checked, err)
	}
	if requests := fixture.requests.Load(); requests != 0 {
		t.Fatalf("upstream requests = %d, want zero for incomparable project hash", requests)
	}
}

func TestFetchSkillFilesRejectsUnsafeRemoteEntriesBeforeDownloading(t *testing.T) {
	cases := []struct {
		name  string
		entry githubTreeEntry
	}{
		{name: "symlink", entry: githubTreeEntry{Path: "skills/demo/link", Mode: "120000", Type: "blob", Size: 1}},
		{name: "submodule", entry: githubTreeEntry{Path: "skills/demo/module", Mode: "160000", Type: "commit", Size: 0}},
		{name: "path escape", entry: githubTreeEntry{Path: "skills/demo/../outside", Mode: "100644", Type: "blob", Size: 1}},
		{name: "oversized file", entry: githubTreeEntry{Path: "skills/demo/large", Mode: "100644", Type: "blob", Size: maxSkillFileBytes + 1}},
	}
	client := newGitHubClient()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tree := remoteSkillTree{OwnerRepo: "acme/skills", Ref: "main", Folder: "skills/demo", Files: []githubTreeEntry{testCase.entry}}
			if _, err := client.fetchSkillFiles(context.Background(), tree); err == nil {
				t.Fatal("fetchSkillFiles accepted an unsafe remote entry")
			}
		})
	}
}

func TestPrepareUpdatesRejectsBatchesAboveAggregateContentLimit(t *testing.T) {
	fixture, server := newLargeBatchFixture(t, 9)
	defer server.Close()

	checked, err := fixture.CheckUpdates(context.Background())
	if err != nil || len(checked.Items) != 9 {
		t.Fatalf("CheckUpdates = %d items, err=%v; want nine candidates", len(checked.Items), err)
	}
	selected := make([]string, 0, len(checked.Items))
	for _, item := range checked.Items {
		if !item.CanUpdate {
			t.Fatalf("candidate %q is not available: %+v", item.Identifier, item)
		}
		selected = append(selected, item.ID)
	}

	if _, err := fixture.PrepareUpdates(context.Background(), selected); !errors.Is(err, ErrUpdatePlanTooLarge) {
		t.Fatalf("PrepareUpdates error = %v, want ErrUpdatePlanTooLarge", err)
	}
}

func newLargeBatchFixture(t *testing.T, skillCount int) (*Service, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	skillsRoot := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	lockRecords := make(map[string]any, skillCount)
	treeEntries := make([]map[string]any, 0, skillCount*5)
	remoteFilesBySkill := make(map[string]map[string][]byte, skillCount)
	extraContent := bytes.Repeat([]byte{'x'}, int(maxSkillFileBytes))
	for i := 0; i < skillCount; i++ {
		identifier := fmt.Sprintf("bulk-%02d", i)
		currentContent := []byte(fmt.Sprintf("---\nname: %s\ndescription: old\n---\n\n# Old\n", identifier))
		localPath := filepath.Join(skillsRoot, identifier)
		if err := os.MkdirAll(localPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(localPath, "SKILL.md"), currentContent, 0o644); err != nil {
			t.Fatal(err)
		}
		currentFiles := []remoteFile{{Path: "SKILL.md", Mode: "100644", Content: currentContent}}
		currentTreeHash := gitTreeHash(currentFiles)
		lockRecords[identifier] = map[string]any{
			"source": "acme/skills", "sourceType": "github", "ref": "main",
			"skillPath":       "skills/" + identifier + "/SKILL.md",
			"skillFolderHash": hex.EncodeToString(currentTreeHash[:]),
		}

		mainContent := make([]byte, int(maxSkillFileBytes))
		copy(mainContent, []byte(fmt.Sprintf("---\nname: %s\ndescription: updated\n---\n\n# Updated\n", identifier)))
		remoteFiles := []remoteFile{
			{Path: "SKILL.md", Mode: "100644", Content: mainContent},
			{Path: "data-a.bin", Mode: "100644", Content: extraContent},
			{Path: "data-b.bin", Mode: "100644", Content: extraContent},
			{Path: "data-c.bin", Mode: "100644", Content: extraContent},
		}
		remoteTreeHash := gitTreeHash(remoteFiles)
		folder := "skills/" + identifier
		treeEntries = append(treeEntries, map[string]any{
			"path": folder, "mode": "040000", "type": "tree", "sha": hex.EncodeToString(remoteTreeHash[:]),
		})
		remoteFilesBySkill[identifier] = make(map[string][]byte, len(remoteFiles))
		for _, file := range remoteFiles {
			remoteFilesBySkill[identifier][file.Path] = file.Content
			treeEntries = append(treeEntries, map[string]any{
				"path": folder + "/" + file.Path, "mode": file.Mode, "type": "blob",
				"sha": testGitBlobSHA(file.Content), "size": len(file.Content),
			})
		}
	}

	lockContents, err := json.Marshal(map[string]any{"version": 3, "skills": lockRecords})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", ".skill-lock.json"), lockContents, 0o600); err != nil {
		t.Fatal(err)
	}
	treeBody, err := json.Marshal(map[string]any{"truncated": false, "tree": treeEntries})
	if err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/acme/skills/git/trees/") {
			_, _ = w.Write(treeBody)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/acme/skills/main/skills/") {
			for identifier, files := range remoteFilesBySkill {
				prefix := "/acme/skills/main/skills/" + identifier + "/"
				if strings.HasPrefix(r.URL.Path, prefix) {
					if content, ok := files[strings.TrimPrefix(r.URL.Path, prefix)]; ok {
						_, _ = w.Write(content)
						return
					}
				}
			}
		}
		http.NotFound(w, r)
	}))
	service, err := New(filepath.Join(home, ".agent-manager", "skills"), home, "", operation.New(filepath.Join(home, ".agent-manager", "journal", "global.json")), nil)
	if err != nil {
		remote.Close()
		t.Fatal(err)
	}
	service.github.apiBase = remote.URL
	service.github.rawBase = remote.URL
	service.github.client = newGitHubClient().client
	return service, remote
}

type upstreamFixture struct {
	service        *Service
	server         *httptest.Server
	home           string
	skillPath      string
	lockPath       string
	lockContents   []byte
	currentContent []byte
	currentHash    string
	updatedContent []byte
	updatedHash    string
	updatedTree    githubTreeResponse
	requests       atomic.Int64
	redirect       atomic.Bool
}

func newUpstreamFixture(t *testing.T, projectScoped bool) *upstreamFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := ""
	scopeRoot := home
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if projectScoped {
		project = filepath.Join(root, "project")
		scopeRoot = project
		lockPath = filepath.Join(project, "skills-lock.json")
	}
	skillPath := filepath.Join(scopeRoot, ".agents", "skills", "remote-demo")
	for _, dir := range []string{home, filepath.Dir(skillPath), filepath.Dir(lockPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	currentContent := []byte("---\nname: remote-demo\ndescription: old local instructions\n---\n\n# Instructions\n")
	updatedContent := []byte("---\nname: remote-demo\ndescription: new upstream instructions\n---\n\n# Instructions\n")
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), currentContent, 0o644); err != nil {
		t.Fatal(err)
	}
	currentFiles := []remoteFile{{Path: "SKILL.md", Mode: "100644", Content: currentContent}}
	updatedFiles := []remoteFile{{Path: "SKILL.md", Mode: "100644", Content: updatedContent}}
	currentTreeHash := gitTreeHash(currentFiles)
	updatedTreeHash := gitTreeHash(updatedFiles)
	currentHash := hex.EncodeToString(currentTreeHash[:])
	updatedHash := hex.EncodeToString(updatedTreeHash[:])
	record := map[string]any{
		"source": "acme/skills", "sourceType": "github", "ref": "main",
		"skillPath": "skills/remote-demo/SKILL.md", "skillFolderHash": currentHash,
	}
	if projectScoped {
		record = map[string]any{
			"source": "acme/skills", "sourceType": "github", "ref": "main",
			"skillPath": "skills/remote-demo/SKILL.md", "computedHash": hashRemoteContent(currentFiles),
		}
		currentHash = hashRemoteContent(currentFiles)
		updatedHash = hashRemoteContent(updatedFiles)
	}
	lock := map[string]any{"version": 1, "skills": map[string]any{"remote-demo": record}}
	lockContents, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lockContents = append(lockContents, '\n')
	if err := os.WriteFile(lockPath, lockContents, 0o600); err != nil {
		t.Fatal(err)
	}
	updatedTree := githubTreeResponse{Tree: []githubTreeEntry{
		{Path: "skills/remote-demo", Mode: "040000", Type: "tree", SHA: hex.EncodeToString(updatedTreeHash[:])},
		{Path: "skills/remote-demo/SKILL.md", Mode: "100644", Type: "blob", SHA: testGitBlobSHA(updatedContent), Size: int64(len(updatedContent))},
	}}
	fixture := &upstreamFixture{server: nil, home: home, skillPath: skillPath, lockPath: lockPath, lockContents: lockContents, currentContent: currentContent, currentHash: currentHash, updatedContent: updatedContent, updatedHash: updatedHash, updatedTree: updatedTree}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveRemote))
	fixture.server = server
	service, err := New(filepath.Join(home, ".agent-manager", "skills"), home, project, operation.New(filepath.Join(home, ".agent-manager", "journal", "global.json")), nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	service.github.apiBase = server.URL
	service.github.rawBase = server.URL
	service.github.client = newGitHubClient().client
	fixture.service = service
	return fixture
}

func (fixture *upstreamFixture) serveRemote(w http.ResponseWriter, r *http.Request) {
	fixture.requests.Add(1)
	if fixture.redirect.Load() {
		http.Redirect(w, r, "/redirect-target", http.StatusFound)
		return
	}
	serveUpstreamTree(w, r, fixture.updatedTree, fixture.updatedContent)
}

func serveUpstreamTree(w http.ResponseWriter, r *http.Request, tree githubTreeResponse, content []byte) {
	if strings.HasPrefix(r.URL.Path, "/repos/acme/skills/git/trees/") {
		_ = json.NewEncoder(w).Encode(tree)
		return
	}
	if r.URL.Path == "/acme/skills/main/skills/remote-demo/SKILL.md" {
		_, _ = w.Write(content)
		return
	}
	http.NotFound(w, r)
}

func testGitTreeSHA(content []byte) string {
	files := []remoteFile{{Path: "SKILL.md", Mode: "100644", Content: content}}
	hash := gitTreeHash(files)
	return hex.EncodeToString(hash[:])
}

func testGitBlobSHA(content []byte) string {
	hash := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(content))+"\x00"), content...))
	return hex.EncodeToString(hash[:])
}
