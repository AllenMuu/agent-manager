package skillsync

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestDiscoverSupportedGlobalAndProjectRoots(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	repository := filepath.Join(home, ".agent-manager", "skills")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	locations := []struct{ path, id string }{
		{filepath.Join(home, ".claude", "skills"), "global-claude"},
		{filepath.Join(home, ".codex", "skills"), "global-codex"},
		{filepath.Join(home, ".pi", "agent", "skills"), "global-pi"},
		{filepath.Join(home, ".agent", "skills"), "global-agent"},
		{filepath.Join(home, ".agents", "skills"), "global-agents"},
		{filepath.Join(project, ".claude", "skills"), "project-claude"},
		{filepath.Join(project, ".codex", "skills"), "project-codex"},
		{filepath.Join(project, ".pi", "skills"), "project-pi"},
		{filepath.Join(project, ".agent", "skills"), "project-agent"},
		{filepath.Join(project, ".agents", "skills"), "project-agents"},
	}
	for _, location := range locations {
		writeSkill(t, filepath.Join(location.path, location.id), "same description")
	}
	inventory, err := Discover(home, project, repository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Locations) != 10 || len(inventory.Sources) != 10 {
		t.Fatalf("inventory locations/sources = %d/%d, want 10/10", len(inventory.Locations), len(inventory.Sources))
	}
	for _, source := range inventory.Sources {
		if !source.Eligible || source.Checksum == "" {
			t.Errorf("source %s is not eligible: %+v", source.ID, source)
		}
	}
	if inventory.Sources[0].Scope != ScopeGlobal {
		t.Fatalf("sources are not deterministically grouped by scope: %+v", inventory.Sources)
	}
}

func TestDiscoverTreatsMissingLocationsAsEmpty(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sources) != 0 || len(inventory.Diagnostics) != 0 {
		t.Fatalf("absent source roots were not empty: %+v", inventory)
	}
}

func TestDiscoverResolvesRelativeMultiHopAdapterLink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, ".claude", "skills", "linked-demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "external", "linked-demo")
	writeSkill(t, real, "linked source")
	hop := filepath.Join(root, "links", "first-hop")
	if err := os.MkdirAll(filepath.Dir(hop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, hop); err != nil {
		t.Fatal(err)
	}
	originalLink, err := filepath.Rel(filepath.Dir(entry), hop)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(originalLink, entry); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var source *Source
	for i := range inventory.Sources {
		if inventory.Sources[i].Identifier == "linked-demo" {
			source = &inventory.Sources[i]
			break
		}
	}
	if source == nil {
		t.Fatalf("linked Skill is missing from inventory: %+v", inventory)
	}
	wantChecksum, err := skillrepo.ChecksumTree(real)
	if err != nil {
		t.Fatal(err)
	}
	if !source.Eligible || source.Checksum != wantChecksum {
		t.Fatalf("link source was not resolved and hashed: %+v", source)
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var details map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &details); err != nil {
		t.Fatal(err)
	}
	var realPath, observedLink string
	if err := json.Unmarshal(details["realPath"], &realPath); err != nil {
		t.Fatalf("source omits resolved realPath: %s", encoded)
	}
	if err := json.Unmarshal(details["originalLink"], &observedLink); err != nil {
		t.Fatalf("source omits original link text: %s", encoded)
	}
	var chain []struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(details["linkChain"], &chain); err != nil {
		t.Fatalf("source omits link chain: %s", encoded)
	}
	var migrationEligible bool
	if err := json.Unmarshal(details["migrationEligible"], &migrationEligible); err != nil {
		t.Fatalf("source omits migration eligibility: %s", encoded)
	}
	if realPath != real || observedLink != originalLink || len(chain) != 2 || chain[0].Path != entry || chain[0].Target != originalLink || chain[1].Path != hop || chain[1].Target != real || !migrationEligible {
		t.Fatalf("resolved link evidence = %s", encoded)
	}
}

func TestDiscoverMarksInventoryIncompleteWhenScopeEntryLimitIsExceeded(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	entries := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(entries, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4097; i++ {
		path := filepath.Join(entries, fmt.Sprintf("entry-%04d", i))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Complete    *bool        `json:"complete"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if response.Complete == nil || *response.Complete {
		t.Fatalf("inventory did not report an incomplete scan: %s", encoded)
	}
	for _, diagnostic := range response.Diagnostics {
		if strings.Contains(diagnostic.Message, "4096") && strings.Contains(diagnostic.Message, "scope") {
			return
		}
	}
	t.Fatalf("inventory omits the scope entry-limit diagnostic: %s", encoded)
}

func TestDiscoverRejectsSkillTreeAboveContentLimit(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(home, ".agents", "skills", "large-tree")
	writeSkill(t, skillPath, "large")
	largeFile := filepath.Join(skillPath, "payload.bin")
	file, err := os.Create(largeFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((64 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range inventory.Sources {
		if source.Identifier == "large-tree" {
			if source.Eligible || !strings.Contains(source.Problem, "64 MiB") {
				t.Fatalf("oversized tree was not rejected: %+v", source)
			}
			return
		}
	}
	t.Fatalf("oversized Skill was omitted without a diagnostic: %+v", inventory)
}

func TestDiscoverRetainsEveryReferenceToOneRealSkill(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	real := filepath.Join(root, "external", "shared-demo")
	writeSkill(t, real, "shared")
	for _, path := range []string{
		filepath.Join(home, ".claude", "skills", "shared-demo"),
		filepath.Join(home, ".codex", "skills", "shared-demo"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var references []Source
	for _, source := range inventory.Sources {
		if source.Identifier == "shared-demo" {
			references = append(references, source)
		}
	}
	if len(references) != 2 || references[0].RealPath != references[1].RealPath || references[0].Checksum != references[1].Checksum {
		t.Fatalf("references to shared Skill were not preserved: %+v", references)
	}
	groups := groupSources(references)
	if len(groups) != 1 || len(groups[0].Sources) != 2 {
		t.Fatalf("shared references did not group into one content candidate: %+v", groups)
	}
}

func TestDiscoverDiagnosesCyclicMissingAndOverlongLinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	entries := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(entries, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	cycleA, cycleB := filepath.Join(root, "cycle-a"), filepath.Join(root, "cycle-b")
	if err := os.Symlink(cycleB, cycleA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cycleA, cycleB); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cycleA, filepath.Join(entries, "cycle-demo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(entries, "missing-demo")); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(root, "overlong-0")
	for i := 0; i < maxSourceLinkHops+1; i++ {
		next := filepath.Join(root, fmt.Sprintf("overlong-%d", i+1))
		if err := os.Symlink(next, previous); err != nil {
			t.Fatal(err)
		}
		previous = next
	}
	if err := os.Symlink(filepath.Join(root, "overlong-0"), filepath.Join(entries, "deep-demo")); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantProblem := map[string]string{"cycle-demo": "cycle", "missing-demo": "missing", "deep-demo": "exceeds 16 hops"}
	for identifier, fragment := range wantProblem {
		found := false
		for _, source := range inventory.Sources {
			if source.Identifier == identifier {
				found = true
				if source.Eligible || source.MigrationEligible || !strings.Contains(source.Problem, fragment) {
					t.Errorf("%s was not safely blocked: %+v", identifier, source)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from inventory: %+v", identifier, inventory)
		}
	}
}

func TestDiscoverAttachesSkillsCLIOriginMetadata(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	globalSkill := filepath.Join(home, ".agents", "skills", "remote-demo")
	projectSkill := filepath.Join(project, ".agents", "skills", "local-demo")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, globalSkill, "remote")
	writeSkill(t, projectSkill, "local")
	globalLock := `{"version":3,"skills":{"remote-demo":{"source":"acme/skills","sourceType":"github","ref":"main","skillPath":"skills/remote-demo/SKILL.md","skillFolderHash":"0123456789012345678901234567890123456789","installedAt":"2026-10-01T12:00:00Z","updatedAt":"2026-10-02T12:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(home, ".agents", ".skill-lock.json"), []byte(globalLock), 0o600); err != nil {
		t.Fatal(err)
	}
	projectLock := `{"version":1,"skills":{"local-demo":{"source":"../my-skill","sourceType":"local","computedHash":"012345678901234567890123456789012345678901234567890123456789abcd"}}}`
	if err := os.WriteFile(filepath.Join(project, "skills-lock.json"), []byte(projectLock), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Source{}
	for _, source := range inventory.Sources {
		byID[source.Identifier] = source
	}
	remote := byID["remote-demo"]
	if remote.Origin != "acme/skills" || remote.OriginType != "github" || remote.OriginRef != "main" || remote.OriginSkillPath != "skills/remote-demo/SKILL.md" || remote.UpdatedAt == nil {
		t.Fatalf("remote origin = %+v", remote)
	}
	if local := byID["local-demo"]; local.OriginType != "local" || local.Origin == "" {
		t.Fatalf("local origin = %+v", local)
	}
}

func TestDiscoverTracksNewestLocalModificationAcrossDivergentVersions(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(home, ".agents", "skills", "time-demo")
	newer := filepath.Join(project, ".agents", "skills", "time-demo")
	writeSkill(t, older, "older")
	writeSkill(t, newer, "newer")
	olderTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	newerTime := olderTime.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(older, "SKILL.md"), olderTime, olderTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(newer, "SKILL.md"), newerTime, newerTime); err != nil {
		t.Fatal(err)
	}

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	groups := groupSources(inventory.Sources)
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want two divergent candidates", groups)
	}
	newerCount := 0
	for _, group := range groups {
		if group.NewerLocalModification {
			newerCount++
			if !group.ModifiedAt.Equal(newerTime) {
				t.Fatalf("newer group's modified time = %s, want %s", group.ModifiedAt, newerTime)
			}
		}
	}
	if newerCount != 1 {
		t.Fatalf("newer candidate count = %d, groups=%+v", newerCount, groups)
	}
}

func TestGroupSourcesMarksNewerOnlyWhenDivergentTimesAreComparable(t *testing.T) {
	older := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	timeSource := func(id, checksum string, modifiedAt *time.Time) Source {
		return Source{ID: id, Identifier: "demo", Checksum: checksum, Eligible: true, ModifiedAt: modifiedAt}
	}

	t.Run("one content group has no comparison", func(t *testing.T) {
		groups := groupSources([]Source{
			timeSource("global", "same", &older),
			timeSource("project", "same", &newer),
		})
		if len(groups) != 1 || groups[0].NewerLocalModification {
			t.Fatalf("groups = %+v, want one unmarked content group", groups)
		}
	})

	t.Run("a missing candidate timestamp prevents comparison", func(t *testing.T) {
		groups := groupSources([]Source{
			timeSource("known", "content-a", &newer),
			timeSource("unknown", "content-b", nil),
		})
		for _, group := range groups {
			if group.NewerLocalModification {
				t.Fatalf("group %q marked newer without comparable timestamps: %+v", group.Checksum, group)
			}
		}
	})
}

func TestDiscoverRejectsSymlinkAncestorsAndUnsafeTrees(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linkedRoot := filepath.Join(root, "linked-skills")
	writeSkill(t, filepath.Join(linkedRoot, "hidden"), "unsafe ancestor")
	if err := os.Symlink(linkedRoot, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(project, ".agents", "skills", "unsafe-tree")
	writeSkill(t, unsafe, "unsafe tree")
	if err := os.Symlink(filepath.Join(root, "outside.txt"), filepath.Join(unsafe, "linked-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sources) != 1 || inventory.Sources[0].Eligible {
		t.Fatalf("unsafe Skill tree should be visible but ineligible: %+v", inventory.Sources)
	}
	foundAncestorDiagnostic := false
	for _, diagnostic := range inventory.Diagnostics {
		if strings.Contains(diagnostic.Message, "symbolic-link") && strings.Contains(diagnostic.Path, ".claude") {
			foundAncestorDiagnostic = true
		}
	}
	if !foundAncestorDiagnostic {
		t.Fatalf("missing symlink-ancestor diagnostic: %+v", inventory.Diagnostics)
	}
}

func TestDiscoverMarksSkillsWithSpecialFilesIneligible(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(home, ".agents", "skills", "special-tree")
	writeSkill(t, skillPath, "contains a special file")
	listener, err := net.Listen("unix", filepath.Join(skillPath, "socket"))
	if err != nil {
		t.Skipf("Unix domain sockets are unavailable on this platform: %v", err)
	}
	defer listener.Close()

	inventory, err := Discover(home, project, filepath.Join(root, "library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range inventory.Sources {
		if source.Identifier == "special-tree" {
			if source.Eligible || !strings.Contains(source.Problem, "special") {
				t.Fatalf("source with a socket was not rejected: %+v", source)
			}
			return
		}
	}
	t.Fatalf("source with a socket was omitted from inventory: %+v", inventory)
}

func TestDiscoverExcludesRepositoryAndRecognizesManagedActivation(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	repositoryPath := filepath.Join(home, ".agents", "skills")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(root, "source", "managed")
	writeSkill(t, source, "managed")
	repository, err := skillrepo.Open(repositoryPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := repository.AddVersion("managed", source, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	activation, _ := adapter.For(adapter.ClaudeCode)
	link := activation.GlobalSkillPath(home, "managed")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(entry.Path, link); err != nil {
		t.Fatal(err)
	}
	inventory, err := Discover(home, project, repositoryPath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var managedFound bool
	for _, source := range inventory.Sources {
		if source.Identifier == "managed" && source.AlreadyManaged && source.ManagedMaterial == "symlink" {
			managedFound = true
			if source.MigrationEligible {
				t.Fatalf("managed activation was offered for migration: %+v", source)
			}
		}
	}
	if !managedFound {
		t.Fatalf("managed activation was not recognized: %+v", inventory.Sources)
	}
	for _, source := range inventory.Sources {
		if source.Path == entry.Path {
			t.Fatal("configured repository was included as a source")
		}
	}
}

func writeSkill(t *testing.T, path, description string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + filepath.Base(path) + "\ndescription: " + description + "\n---\n\n# instructions\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
