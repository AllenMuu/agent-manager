package webconsole

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillsync"
)

const upstreamUpdateOperation = "update upstream Skills"

func (s *Server) newUpstreamJournal(project *registeredProject) (*operation.Journal, error) {
	if err := validateUpstreamJournalLayout(s.homeDir); err != nil {
		return nil, err
	}
	journalPath := filepath.Join(s.homeDir, ".agent-manager", "journal", "upstream-skills.json")
	journal := operation.New(journalPath)
	validate := func(entry operation.Entry) error {
		return validateUpstreamJournalEntry(s.homeDir, project, journalPath, entry)
	}
	journal.ValidateEntry = validate
	journal.ValidateUndoEntry = validate
	if s.beforeJournalDirectorySync != nil {
		journal.BeforeJournalDirectorySync = s.beforeJournalDirectorySync
	}
	return journal, nil
}

func validateUpstreamJournalLayout(home string) error {
	for _, path := range []string{
		filepath.Join(home, ".agent-manager"),
		filepath.Join(home, ".agent-manager", "journal"),
		filepath.Join(home, ".agent-manager", "journal", ".skill-manager-journal"),
	} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.Join(operation.ErrUnsafePath, fmt.Errorf("upstream Skill journal directory %q must be a real directory", path))
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".agent-manager", "journal", "upstream-skills.json"),
		filepath.Join(home, ".agent-manager", "journal", "upstream-skills.json.lock"),
	} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.Join(operation.ErrUnsafePath, fmt.Errorf("upstream Skill journal file %q must be a regular file", path))
		}
	}
	return nil
}

func validateUpstreamJournalEntry(home string, project *registeredProject, journalPath string, entry operation.Entry) error {
	if filepath.Clean(journalPath) != filepath.Join(home, ".agent-manager", "journal", "upstream-skills.json") || entry.Operation != upstreamUpdateOperation || entry.ResourceKind != "skill" || len(entry.Materializations) != 0 || len(entry.Before) == 0 || len(entry.Before) != len(entry.After) {
		return errors.Join(operation.ErrUnsafePath, fmt.Errorf("invalid upstream Skill update journal entry"))
	}
	backupRoot := filepath.Join(filepath.Dir(journalPath), ".skill-manager-journal")
	before := map[string]operation.Snapshot{}
	after := map[string]operation.Snapshot{}
	for _, snapshot := range entry.Before {
		if err := validateUpdateSnapshotPath(home, project, backupRoot, snapshot); err != nil {
			return err
		}
		if _, exists := before[snapshot.Path]; exists {
			return operation.ErrUnsafePath
		}
		before[snapshot.Path] = snapshot
	}
	for _, snapshot := range entry.After {
		if err := validateUpdateSnapshotPath(home, project, backupRoot, snapshot); err != nil {
			return err
		}
		if _, exists := after[snapshot.Path]; exists {
			return operation.ErrUnsafePath
		}
		after[snapshot.Path] = snapshot
	}
	for path, snapshot := range before {
		other, exists := after[path]
		if !exists || !snapshot.Exists || !other.Exists {
			return operation.ErrUnsafePath
		}
	}
	return nil
}

func validateUpdateSnapshotPath(home string, project *registeredProject, backupRoot string, snapshot operation.Snapshot) error {
	if !filepath.IsAbs(snapshot.Path) || filepath.Clean(snapshot.Path) != snapshot.Path {
		return operation.ErrUnsafePath
	}
	if strings.HasPrefix(snapshot.Path, backupRoot+string(filepath.Separator)) || snapshot.Path == backupRoot {
		return operation.ErrUnsafePath
	}
	if snapshot.Exists {
		if !filepath.IsAbs(snapshot.Backup) || filepath.Clean(snapshot.Backup) != snapshot.Backup {
			return operation.ErrUnsafePath
		}
		relative, err := filepath.Rel(backupRoot, snapshot.Backup)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return operation.ErrUnsafePath
		}
	} else if snapshot.Backup != "" {
		return operation.ErrUnsafePath
	}
	if isSupportedUpdateSkillPath(home, project, snapshot.Path) || filepath.Clean(snapshot.Path) == filepath.Clean(skillsyncGlobalLockPath(home)) {
		return nil
	}
	if project != nil && snapshot.Path == filepath.Join(project.Path, "skills-lock.json") {
		return nil
	}
	return operation.ErrUnsafePath
}

func skillsyncGlobalLockPath(home string) string {
	// Keep the journal boundary aligned with Skills CLI's XDG-aware global lock.
	return skillsync.GlobalSkillLockPath(home)
}

func isSupportedUpdateSkillPath(home string, project *registeredProject, value string) bool {
	roots := []string{filepath.Join(home, ".agents", "skills")}
	if project != nil {
		if project.validate() != nil {
			return false
		}
		roots = append(roots, filepath.Join(project.Path, ".agents", "skills"))
	}
	for _, root := range roots {
		relative, err := filepath.Rel(root, value)
		if err != nil || relative == "." || filepath.Dir(relative) != "." || relative == ".." || strings.ContainsAny(relative, `/\\`) {
			continue
		}
		return true
	}
	return false
}
