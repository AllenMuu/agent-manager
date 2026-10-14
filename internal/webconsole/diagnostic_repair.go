package webconsole

import (
	"errors"
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/diagnostic"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

const (
	doctorReconcilePlan = "doctor-reconcile"
	doctorGitignorePlan = "doctor-gitignore"

	doctorRepairReconcile = "reconcile"
	doctorRepairGitignore = "add_gitignore"
)

type diagnosticRepairRequest struct {
	Path string `json:"path"`
}

type doctorFindingAdvice struct {
	RepairAction string
	Guidance     string
	GuidanceRef  *messageRefDTO
}

func (s *Server) doctorFindingPresentation(project *registeredProject, journal *operation.Journal, finding diagnostic.Finding) doctorFindingAdvice {
	switch finding.Code {
	case "doctor.invalid_catalog_entry":
		return doctorFindingAdvice{Guidance: "Repair the invalid Skill entry in the configured library, then run the scan again.", GuidanceRef: messageRef("doctor.guidance.invalid_catalog_entry", nil)}
	case "doctor.unmanaged_resource":
		return doctorFindingAdvice{Guidance: "This resource is not managed by Agent Manager and was left unchanged. Review it before adopting or removing it.", GuidanceRef: messageRef("doctor.guidance.unmanaged_resource", nil)}
	case "doctor.orphaned_managed_link":
		if _, _, err := s.previewDiagnosticRepairForFinding(project, journal, doctorReconcilePlan, finding); err == nil {
			return doctorFindingAdvice{RepairAction: doctorRepairReconcile}
		}
		return doctorFindingAdvice{Guidance: "No eligible Skill with this identifier is available to restore the managed link. Restore or correct the configured library entry, then rescan.", GuidanceRef: messageRef("doctor.guidance.orphaned_unrepairable", nil)}
	case "doctor.ambiguous_dangling_link":
		return doctorFindingAdvice{Guidance: "Ownership of this dangling link cannot be verified. Inspect its target and ownership before changing it.", GuidanceRef: messageRef("doctor.guidance.ambiguous_dangling_link", nil)}
	case "doctor.unsupported_skill_location":
		return doctorFindingAdvice{Guidance: "This agent Skill location is not supported for managed operations. Move or manage the resource through a supported location.", GuidanceRef: messageRef("doctor.guidance.unsupported_skill_location", nil)}
	case "doctor.git_worktree_unavailable":
		return doctorFindingAdvice{Guidance: "Git could not inspect this worktree. Repair or initialize the worktree, then run Doctor again.", GuidanceRef: messageRef("doctor.guidance.git_worktree_unavailable", nil)}
	case "doctor.git_managed_link_status":
		switch finding.Args["status"] {
		case "would be tracked":
			if _, _, err := s.previewDiagnosticRepairForFinding(project, journal, doctorGitignorePlan, finding); err == nil {
				return doctorFindingAdvice{RepairAction: doctorRepairGitignore}
			}
			return doctorFindingAdvice{Guidance: "An exact ignore rule could not be planned safely. Inspect the project .gitignore and managed link, then rescan.", GuidanceRef: messageRef("doctor.guidance.gitignore_unavailable", nil)}
		case "tracked":
			return doctorFindingAdvice{Guidance: "This link is already in Git's index. An ignore rule will not untrack it; review the index manually.", GuidanceRef: messageRef("doctor.guidance.git_tracked", nil)}
		case "ignored":
			return doctorFindingAdvice{Guidance: "This managed link is already ignored by Git.", GuidanceRef: messageRef("doctor.guidance.git_ignored", nil)}
		}
	}
	return doctorFindingAdvice{}
}

func (s *Server) previewDiagnosticRepair(project *registeredProject, kind string, request diagnosticRepairRequest) (operation.Plan, string, error) {
	if request.Path == "" {
		return operation.Plan{}, "", errors.New("a selected Doctor finding path is required")
	}
	journal, err := s.journalForProject(project)
	if err != nil {
		return operation.Plan{}, "", err
	}
	findings, err := diagnostic.Scan(s.libraryPath, project.Path, journal)
	if err != nil {
		return operation.Plan{}, "", err
	}
	code := repairFindingCode(kind)
	for _, finding := range findings {
		if finding.Path == request.Path && finding.Code == code {
			return s.previewDiagnosticRepairForFinding(project, journal, kind, finding)
		}
	}
	return operation.Plan{}, "", errors.New("the selected Doctor finding is no longer available for repair")
}

func (s *Server) previewDiagnosticRepairForFinding(project *registeredProject, journal *operation.Journal, kind string, finding diagnostic.Finding) (operation.Plan, string, error) {
	var plan operation.Plan
	switch kind {
	case doctorReconcilePlan:
		if finding.Code != "doctor.orphaned_managed_link" {
			return plan, "", errors.New("only an orphaned managed link can be reconciled")
		}
		_, err := diagnostic.ReconcilePath(s.libraryPath, project.Path, journal, finding.Path, func(candidate operation.Plan) bool {
			plan = candidate
			return false
		})
		if !errors.Is(err, operation.ErrNotConfirmed) {
			if err == nil {
				return plan, "", errors.New("the selected link no longer needs reconciliation")
			}
			return plan, "", err
		}
	case doctorGitignorePlan:
		if finding.Code != "doctor.git_managed_link_status" || finding.Args["status"] != "would be tracked" {
			return plan, "", errors.New("only an unignored managed link that Git would track can receive an ignore rule")
		}
		candidate, err := diagnostic.AddGitignore(project.Path, []string{finding.Path}, func(value operation.Plan) bool {
			plan = value
			return false
		}, journal)
		if err == nil {
			plan = candidate
			return plan, "", errors.New("the selected link no longer needs an ignore rule")
		}
		if !errors.Is(err, operation.ErrNotConfirmed) {
			return plan, "", err
		}
	default:
		return plan, "", fmt.Errorf("unsupported Doctor repair %q", kind)
	}
	if len(plan.Changes) == 0 {
		return plan, "", errors.New("the selected Doctor finding has no safe repair plan")
	}
	digest, err := diagnosticRepairFingerprint(project.ID, kind, finding, plan, journal)
	return plan, digest, err
}

func (s *Server) executeDoctorRepair(project *registeredProject, stored *storedPlan) error {
	if stored == nil || stored.doctorRepair == nil {
		return operation.ErrUnexpectedState
	}
	journal, err := s.journalForProject(project)
	if err != nil {
		return err
	}
	confirmed := false
	confirm := func(candidate operation.Plan) bool {
		current, currentFingerprint, err := s.previewDiagnosticRepair(project, stored.kind, *stored.doctorRepair)
		if err != nil {
			return false
		}
		candidateFingerprint, err := fingerprint(candidate)
		if err != nil {
			return false
		}
		currentPlanFingerprint, err := fingerprint(current)
		if err != nil {
			return false
		}
		confirmed = currentFingerprint == stored.fingerprint && candidateFingerprint == currentPlanFingerprint
		return confirmed
	}

	switch stored.kind {
	case doctorReconcilePlan:
		_, err = diagnostic.ReconcilePath(s.libraryPath, project.Path, journal, stored.doctorRepair.Path, confirm)
	case doctorGitignorePlan:
		_, err = diagnostic.AddGitignore(project.Path, []string{stored.doctorRepair.Path}, confirm, journal)
	default:
		return fmt.Errorf("unsupported Doctor repair %q", stored.kind)
	}
	if errors.Is(err, operation.ErrNotConfirmed) || (err == nil && !confirmed) {
		return operation.ErrUnexpectedState
	}
	return err
}

func diagnosticRepairFingerprint(projectID, kind string, finding diagnostic.Finding, plan operation.Plan, journal *operation.Journal) (string, error) {
	entry, exists, err := journal.Latest()
	if err != nil {
		return "", err
	}
	var latest *operation.Entry
	if exists {
		latest = &entry
	}
	return fingerprint(struct {
		ProjectID string
		Kind      string
		Finding   diagnostic.Finding
		Plan      operation.Plan
		Latest    *operation.Entry
	}{projectID, kind, finding, plan, latest})
}

func repairFindingCode(kind string) string {
	switch kind {
	case doctorReconcilePlan:
		return "doctor.orphaned_managed_link"
	case doctorGitignorePlan:
		return "doctor.git_managed_link_status"
	default:
		return ""
	}
}
