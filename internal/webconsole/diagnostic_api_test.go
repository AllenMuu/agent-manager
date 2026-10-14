package webconsole

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestDoctorEndpointExposesFindingCodesRepairActionsAndManualGuidance(t *testing.T) {
	server, project, library, skillID := operationFixture(t)
	project = server.project.Path
	if err := os.Mkdir(filepath.Join(library, "invalid-entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	unmanagedPath := projectSkillPath(t, project, adapter.Codex, "unmanaged")
	if err := os.Mkdir(unmanagedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	unsupportedRoot := filepath.Join(project, ".unsupported", "skills")
	if err := os.MkdirAll(unsupportedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanPath := projectSkillPath(t, project, adapter.Codex, skillID)
	oldTarget := filepath.Join(t.TempDir(), "old-library", skillID)
	if err := os.MkdirAll(oldTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldTarget, "SKILL.md"), []byte("---\nname: old\ndescription: old\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	before, err := journal.Capture([]string{orphanPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldTarget, orphanPath); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{orphanPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(oldTarget)); err != nil {
		t.Fatal(err)
	}

	response := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/diagnostics", "")
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Findings []struct {
			Path         string `json:"path"`
			Code         string `json:"code"`
			RepairAction string `json:"repairAction"`
			Guidance     string `json:"guidance"`
			GuidanceRef  *struct {
				Key string `json:"key"`
			} `json:"guidanceRef"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body.String(), "adapter capabilities") || strings.Contains(response.Body.String(), "doctor.adapter_capabilities") {
		t.Fatalf("adapter capabilities leaked into Doctor findings: %s", response.Body.String())
	}
	var orphanFound, invalidFound, unmanagedFound, unsupportedFound bool
	for _, finding := range envelope.Findings {
		switch finding.Code {
		case "doctor.orphaned_managed_link":
			orphanFound = finding.Path == orphanPath && finding.RepairAction == "reconcile"
		case "doctor.invalid_catalog_entry":
			invalidFound = finding.Guidance != "" && finding.GuidanceRef != nil && finding.GuidanceRef.Key == "doctor.guidance.invalid_catalog_entry"
		case "doctor.unmanaged_resource":
			unmanagedFound = finding.Path == unmanagedPath && finding.RepairAction == "" && finding.GuidanceRef != nil && finding.GuidanceRef.Key == "doctor.guidance.unmanaged_resource"
		case "doctor.unsupported_skill_location":
			unsupportedFound = finding.Path == filepath.Dir(unsupportedRoot) && finding.RepairAction == "" && finding.GuidanceRef != nil && finding.GuidanceRef.Key == "doctor.guidance.unsupported_skill_location"
		}
	}
	if !orphanFound || !invalidFound || !unmanagedFound || !unsupportedFound {
		t.Fatalf("findings = %#v; want one actionable orphan and manual catalog, unmanaged-resource, and unsupported-location guidance", envelope.Findings)
	}
}

func TestDoctorReconcilePlanReviewsOneFindingAndJournalsExecution(t *testing.T) {
	server, project, library, skillID := operationFixture(t)
	project = server.project.Path
	link := projectSkillPath(t, project, adapter.Codex, skillID)
	oldTarget := filepath.Join(t.TempDir(), "old-library", skillID)
	if err := os.MkdirAll(oldTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldTarget, "SKILL.md"), []byte("---\nname: old\ndescription: old\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	before, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldTarget, link); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(oldTarget)); err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]string{"path": link})
	if err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/doctor-reconcile", string(body))
	if preview.Code != http.StatusCreated {
		t.Fatalf("preview status = %d, body = %s", preview.Code, preview.Body.String())
	}
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Plan.ID == "" || len(envelope.Plan.Changes) != 1 || envelope.Plan.Changes[0].Path != link {
		t.Fatalf("plan = %#v; want one change for %s", envelope.Plan, link)
	}
	if got, err := os.Readlink(link); err != nil || got != oldTarget {
		t.Fatalf("preview changed link to %q, err=%v", got, err)
	}
	journalAfterPreview, err := os.ReadFile(journal.Path)
	if err != nil || string(journalAfterPreview) != string(journalBefore) {
		t.Fatalf("preview changed journal: %v", err)
	}

	execute := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if execute.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body = %s", execute.Code, execute.Body.String())
	}
	if got, err := os.Readlink(link); err != nil || got != filepath.Join(library, skillID) {
		t.Fatalf("reconciled link = %q, err=%v", got, err)
	}
	latest, ok, err := journal.Latest()
	if err != nil || !ok || latest.Operation != "reconcile" {
		t.Fatalf("latest operation = %#v, ok=%v, err=%v; want journaled reconcile", latest, ok, err)
	}
}

func TestDoctorGitignorePlanAppendsOnlySelectedManagedLinkAfterConfirmation(t *testing.T) {
	server, project, library, skillID := operationFixture(t)
	project = server.project.Path
	if output, err := exec.Command("git", "-C", project, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	link := projectSkillPath(t, project, adapter.Codex, skillID)
	if err := os.Symlink(filepath.Join(library, skillID), link); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"path": link})
	if err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/doctor-gitignore", string(body))
	if preview.Code != http.StatusCreated {
		t.Fatalf("preview status = %d, body = %s", preview.Code, preview.Body.String())
	}
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Plan.Changes) != 1 || envelope.Plan.Changes[0].Path != filepath.Join(project, ".gitignore") {
		t.Fatalf("plan = %#v; want one project .gitignore change", envelope.Plan)
	}
	if _, err := os.Stat(filepath.Join(project, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("preview changed .gitignore, stat error = %v", err)
	}

	execute := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if execute.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body = %s", execute.Code, execute.Body.String())
	}
	contents, err := os.ReadFile(filepath.Join(project, ".gitignore"))
	if err != nil || string(contents) != "/.codex/skills/"+skillID+"\n" {
		t.Fatalf(".gitignore = %q, err=%v", contents, err)
	}
	latest, err := server.journalForProject(server.project)
	if err != nil {
		t.Fatal(err)
	}
	entry, exists, err := latest.Latest()
	if err != nil || !exists || entry.Operation != "update managed-link Git guidance" {
		t.Fatalf("latest operation = %#v, exists=%v, err=%v; want journaled Git guidance", entry, exists, err)
	}
}

func TestDoctorRepairRejectsForgedPathAndStaleLink(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	project = server.project.Path
	link := projectSkillPath(t, project, adapter.Codex, skillID)
	oldTarget := filepath.Join(t.TempDir(), "old-library", skillID)
	if err := os.MkdirAll(oldTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldTarget, "SKILL.md"), []byte("---\nname: old\ndescription: old\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	before, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldTarget, link); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(oldTarget)); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	body, _ := json.Marshal(map[string]string{"path": outside})
	forged := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/doctor-reconcile", string(body))
	if forged.Code < http.StatusBadRequest {
		t.Fatalf("forged repair status = %d, body = %s", forged.Code, forged.Body.String())
	}

	body, _ = json.Marshal(map[string]string{"path": link})
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/doctor-reconcile", string(body))
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("preview = %d %s; err=%v", preview.Code, preview.Body.String(), err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "replacement"), link); err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	execute := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if execute.Code != http.StatusConflict || !strings.Contains(execute.Body.String(), "stale_plan") {
		t.Fatalf("execute status = %d, body = %s; want stale-plan conflict", execute.Code, execute.Body.String())
	}
	if target, err := os.Readlink(link); err != nil || target == filepath.Join(project, ".codex", "skills", skillID) {
		t.Fatalf("changed link = %q, err=%v", target, err)
	}
	journalAfter, err := os.ReadFile(journal.Path)
	if err != nil || string(journalAfter) != string(journalBefore) {
		t.Fatalf("stale execution changed journal: %v", err)
	}
}

func projectSkillPath(t *testing.T, project string, target adapter.Target, identifier string) string {
	t.Helper()
	placement, ok := adapter.For(target)
	if !ok {
		t.Fatalf("adapter %q not found", target)
	}
	path := placement.ProjectSkillPath(project, identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
