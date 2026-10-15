package skillsync

import (
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
