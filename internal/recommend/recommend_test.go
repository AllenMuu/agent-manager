package recommend_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/recommend"
)

func TestMatchScopeRanksTagsAboveFieldsAndBody(t *testing.T) {
	skills := []catalog.Skill{
		{Identifier: "a-tagged", Description: "helpers", Body: "general utilities", Tags: []string{"go"}},
		{Identifier: "b-ident", Description: "go utilities", Body: "general text"},
		{Identifier: "c-body", Description: "helpers", Body: "works with Go code"},
	}
	recs := recommend.MatchScope(skills, []string{"go"})
	if len(recs) != 3 {
		t.Fatalf("recommendations = %d, want 3", len(recs))
	}
	if recs[0].Identifier != "a-tagged" || recs[0].Confidence != recommend.ConfidenceHigh {
		t.Fatalf("top = %#v, want a-tagged with high confidence", recs[0])
	}
	if recs[1].Identifier != "b-ident" || recs[1].Confidence != recommend.ConfidenceMedium {
		t.Fatalf("second = %#v, want b-ident with medium confidence", recs[1])
	}
	if recs[2].Identifier != "c-body" || recs[2].Confidence != recommend.ConfidenceLow {
		t.Fatalf("third = %#v, want c-body with low confidence", recs[2])
	}
}

func TestMatchScopeTiesBreakByAscendingIdentifier(t *testing.T) {
	skills := []catalog.Skill{
		{Identifier: "zeta", Description: "go helper"},
		{Identifier: "alpha", Description: "go helper"},
	}
	recs := recommend.MatchScope(skills, []string{"go"})
	if len(recs) != 2 || recs[0].Identifier != "alpha" || recs[1].Identifier != "zeta" {
		t.Fatalf("order = %#v", recs)
	}
}

func TestMatchScopeIgnoresSubstringsAndNonTechnologyTags(t *testing.T) {
	skills := []catalog.Skill{
		{Identifier: "gogo-dancer", Description: "dance moves", Body: "dancing"},
		{Identifier: "web-frontend", Description: "frontend", Body: "frontend", Tags: []string{"frontend", "Go Lang"}},
	}
	recs := recommend.MatchScope(skills, []string{"go"})
	if len(recs) != 0 {
		t.Fatalf("recommendations = %#v, want none", recs)
	}
}

func TestMatchScopeMatchesMultiWordTechnologiesCompletely(t *testing.T) {
	skills := []catalog.Skill{
		{Identifier: "boot", Description: "spring framework notes"},
		{Identifier: "jvm-helper", Description: "spring boot service helper"},
	}
	recs := recommend.MatchScope(skills, []string{"spring-boot"})
	if len(recs) != 1 || recs[0].Identifier != "jvm-helper" {
		t.Fatalf("recommendations = %#v, want only jvm-helper", recs)
	}
}

func TestRecommendReportsScopeStatuses(t *testing.T) {
	library := t.TempDir()
	writeLibrarySkill(t, library, "go-helper", "Go helper", "helps with go", []string{"go"})
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "go.mod"), "module demo\n")
	writeFile(t, filepath.Join(project, "services", "unknown", "notes.txt"), "x")

	result, err := recommend.Recommend(project, library)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 1 {
		t.Fatalf("scopes = %d, want 1", len(result.Scopes))
	}
	scope := result.Scopes[0]
	if scope.Status != recommend.StatusRecommended || scope.NextAction != recommend.ActionReviewRecommendations {
		t.Fatalf("scope = %s/%s", scope.Status, scope.NextAction)
	}
	if len(scope.Recommendations) != 1 || scope.Recommendations[0].Identifier != "go-helper" {
		t.Fatalf("recommendations = %#v", scope.Recommendations)
	}
	if scope.Recommendations[0].Confidence != recommend.ConfidenceHigh {
		t.Fatalf("confidence = %s, want high", scope.Recommendations[0].Confidence)
	}
}

func TestRecommendInsufficientEvidenceAndNoCatalogMatch(t *testing.T) {
	library := t.TempDir()
	writeLibrarySkill(t, library, "go-helper", "Go helper", "helps with go", []string{"go"})

	empty := t.TempDir()
	result, err := recommend.Recommend(empty, library)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scopes[0].Status != recommend.StatusInsufficientEvidence || result.Scopes[0].NextAction != recommend.ActionUseCatalogSearch {
		t.Fatalf("scope = %#v", result.Scopes[0])
	}

	rustProject := t.TempDir()
	writeFile(t, filepath.Join(rustProject, "Cargo.toml"), "[package]\nname = 'demo'\n")
	result, err = recommend.Recommend(rustProject, library)
	if err != nil {
		t.Fatal(err)
	}
	scope := result.Scopes[0]
	if scope.Status != recommend.StatusNoCatalogMatch || scope.NextAction != recommend.ActionAddSkillMetadata {
		t.Fatalf("scope = %s/%s", scope.Status, scope.NextAction)
	}
	if len(scope.Technologies) != 1 || scope.Technologies[0].ID != "rust" {
		t.Fatalf("technologies = %#v", scope.Technologies)
	}
}

func TestRecommendExcludesEntriesWithoutSkillMarkdown(t *testing.T) {
	library := t.TempDir()
	// An entry with matching text but no SKILL.md is not eligible.
	if err := os.MkdirAll(filepath.Join(library, "go-impostor"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "go.mod"), "module demo\n")
	result, err := recommend.Recommend(project, library)
	if err != nil {
		t.Fatal(err)
	}
	if scope := result.Scopes[0]; scope.Status != recommend.StatusNoCatalogMatch {
		t.Fatalf("scope = %#v, want no_catalog_match", scope)
	}
}

func TestRecommendMissingLibraryReturnsEmptyCatalog(t *testing.T) {
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "go.mod"), "module demo\n")
	library := filepath.Join(t.TempDir(), "missing")
	result, err := recommend.Recommend(project, library)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 1 || result.Scopes[0].Status != recommend.StatusNoCatalogMatch || len(result.Scopes[0].Recommendations) != 0 {
		t.Fatalf("result = %#v; want a no-catalog-match scope", result)
	}
	if _, err := os.Lstat(library); !os.IsNotExist(err) {
		t.Fatalf("recommend created missing library: %v", err)
	}
}

func TestRecommendPerformsNoMutation(t *testing.T) {
	library := t.TempDir()
	writeLibrarySkill(t, library, "go-helper", "Go helper", "helps with go", []string{"go"})
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "go.mod"), "module demo\n")
	before := snapshot(t, project)
	if _, err := recommend.Recommend(project, library); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, project)
	if before != after {
		t.Fatalf("project changed:\n%s\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(project, ".skill-manager")); !os.IsNotExist(err) {
		t.Fatal("recommendation wrote a journal or state directory")
	}
}

func writeLibrarySkill(t *testing.T, library, identifier, description, body string, tags []string) {
	t.Helper()
	dir := filepath.Join(library, identifier)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tagLines := ""
	for _, tag := range tags {
		tagLines += "  - " + tag + "\n"
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+identifier+"\ndescription: "+description+"\n---\n"+body+"\n")
	writeFile(t, filepath.Join(dir, ".skill-manager.yaml"), "tags:\n"+tagLines)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			paths = append(paths, path)
		}
		return nil
	})
	out := ""
	for _, p := range paths {
		out += p + "\n"
	}
	return out
}
