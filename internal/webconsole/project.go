package webconsole

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

var errProjectAlreadyRegistered = errors.New("a different project is already registered for this console session")
var errRegisteredProjectChanged = errors.New("the registered project directory changed")

type registeredProject struct {
	ID       string
	Name     string
	Path     string
	identity os.FileInfo
}

type projectDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Server) registerProject(value string) (projectDTO, error) {
	if strings.TrimSpace(value) == "" {
		return projectDTO{}, errors.New("project path must not be empty")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return projectDTO{}, fmt.Errorf("resolve project path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return projectDTO{}, fmt.Errorf("resolve project directory %q: %w", value, err)
	}
	canonical = filepath.Clean(canonical)
	info, err := os.Stat(canonical)
	if err != nil {
		return projectDTO{}, fmt.Errorf("inspect project directory %q: %w", value, err)
	}
	if !info.IsDir() {
		return projectDTO{}, fmt.Errorf("project path %q is not a directory", value)
	}
	if _, err := os.ReadDir(canonical); err != nil {
		return projectDTO{}, fmt.Errorf("read project directory %q: %w", value, err)
	}
	project := &registeredProject{ID: "pending", Name: filepath.Base(canonical), Path: canonical, identity: info}
	if err := validateProjectJournalLayout(project); err != nil {
		return projectDTO{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project != nil {
		if s.project.Path != canonical {
			return projectDTO{}, errProjectAlreadyRegistered
		}
		if err := s.project.validate(); err != nil {
			return projectDTO{}, err
		}
		return viewProject(s.project), nil
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return projectDTO{}, fmt.Errorf("create project session identifier: %w", err)
	}
	project.ID = hex.EncodeToString(idBytes)
	s.project = project
	return viewProject(project), nil
}

func (project *registeredProject) validate() error {
	if project == nil || project.identity == nil {
		return errRegisteredProjectChanged
	}
	current, err := os.Lstat(project.Path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(project.identity, current) {
		return errors.Join(errRegisteredProjectChanged, err)
	}
	return nil
}

func validateProjectJournalLayout(project *registeredProject) error {
	if err := project.validate(); err != nil {
		return err
	}
	for _, name := range []string{".skill-manager", filepath.Join(".skill-manager", ".skill-manager-journal")} {
		path := filepath.Join(project.Path, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return errors.Join(lifecycle.ErrUnsafePath, fmt.Errorf("inspect project journal path: %w", err))
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.Join(lifecycle.ErrUnsafePath, fmt.Errorf("project journal path %q must be a real directory", name))
		}
	}
	journal := filepath.Join(project.Path, ".skill-manager", "journal.json")
	info, err := os.Lstat(journal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.Join(lifecycle.ErrUnsafePath, fmt.Errorf("inspect project journal file: %w", err))
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.Join(lifecycle.ErrUnsafePath, fmt.Errorf("project journal file must be a regular file"))
	}
	return nil
}

func (s *Server) journalForProject(project *registeredProject) (*operation.Journal, error) {
	if err := validateProjectJournalLayout(project); err != nil {
		return nil, err
	}
	journal := operation.New(filepath.Join(project.Path, ".skill-manager", "journal.json"))
	journal.ValidateEntry = func(entry operation.Entry) error {
		return validateProjectJournalEntry(project, s.libraryPath, entry)
	}
	journal.ValidateUndoEntry = func(entry operation.Entry) error {
		return validateConsoleUndoEntry(project, s.libraryPath, entry)
	}
	journal.BeforeRestorePublish = func(string) error { return validateProjectJournalLayout(project) }
	if s.beforeJournalDirectorySync != nil {
		journal.BeforeJournalDirectorySync = s.beforeJournalDirectorySync
	}
	return journal, nil
}

func validateProjectJournalEntry(project *registeredProject, libraryPath string, entry operation.Entry) error {
	if err := validateProjectJournalLayout(project); err != nil {
		return errors.Join(lifecycle.ErrUnsafePath, err)
	}
	if len(entry.Before) == 0 || len(entry.Before) != len(entry.After) {
		return fmt.Errorf("%w: operation journal snapshots are incomplete", lifecycle.ErrUnsafePath)
	}
	beforePaths, err := validateProjectSnapshots(project, libraryPath, entry, entry.Before)
	if err != nil {
		return err
	}
	afterPaths, err := validateProjectSnapshots(project, libraryPath, entry, entry.After)
	if err != nil {
		return err
	}
	if len(beforePaths) != len(afterPaths) {
		return fmt.Errorf("%w: operation journal snapshot paths do not match", lifecycle.ErrUnsafePath)
	}
	for path := range beforePaths {
		if _, ok := afterPaths[path]; !ok {
			return fmt.Errorf("%w: operation journal snapshot paths do not match", lifecycle.ErrUnsafePath)
		}
	}
	return nil
}

func validateProjectSnapshots(project *registeredProject, libraryPath string, entry operation.Entry, snapshots []operation.Snapshot) (map[string]struct{}, error) {
	paths := make(map[string]struct{}, len(snapshots))
	backupRoot := filepath.Join(project.Path, ".skill-manager", ".skill-manager-journal")
	for _, snapshot := range snapshots {
		path := snapshot.Path
		root, ok := journalSnapshotRoot(project, libraryPath, entry, path)
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !ok {
			return nil, fmt.Errorf("%w: operation journal snapshot path is outside an approved resource root", lifecycle.ErrUnsafePath)
		}
		if _, duplicate := paths[path]; duplicate {
			return nil, fmt.Errorf("%w: operation journal contains a duplicate snapshot path", lifecycle.ErrUnsafePath)
		}
		paths[path] = struct{}{}
		if err := validateSnapshotParents(root, path); err != nil {
			return nil, err
		}
		if !snapshot.Exists {
			if snapshot.Backup != "" {
				return nil, fmt.Errorf("%w: absent operation snapshot unexpectedly has a backup", lifecycle.ErrUnsafePath)
			}
			continue
		}
		backup := snapshot.Backup
		if !filepath.IsAbs(backup) || filepath.Clean(backup) != backup || filepath.Dir(backup) != backupRoot || filepath.Base(backup) == "." {
			return nil, fmt.Errorf("%w: operation journal backup path is outside the project backup directory", lifecycle.ErrUnsafePath)
		}
		if _, err := os.Lstat(backup); err != nil {
			return nil, fmt.Errorf("%w: inspect operation journal backup: %v", lifecycle.ErrUnsafePath, err)
		}
	}
	return paths, nil
}

func validateSnapshotParents(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("%w: resolve operation journal snapshot path: %v", lifecycle.ErrUnsafePath, err)
	}
	for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
		info, err := os.Lstat(filepath.Join(root, parent))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: inspect operation journal snapshot parent: %v", lifecycle.ErrUnsafePath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: operation journal snapshot parent is not a real directory", lifecycle.ErrUnsafePath)
		}
	}
	return nil
}

func journalSnapshotRoot(project *registeredProject, libraryPath string, entry operation.Entry, path string) (string, bool) {
	if pathWithin(project.Path, path) {
		return project.Path, true
	}
	if entry.Operation != "adopt" || entry.ResourceKind != string(resource.Skill) || !isLibrarySkillPath(libraryPath, path) {
		return "", false
	}
	root, err := filepath.Abs(libraryPath)
	if err != nil {
		return "", false
	}
	return filepath.Clean(root), true
}

func isLibrarySkillPath(libraryPath, path string) bool {
	root, err := filepath.Abs(libraryPath)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil || filepath.Clean(path) != path || filepath.Dir(path) != filepath.Clean(root) {
		return false
	}
	return adapter.ValidateIdentifier(filepath.Base(path)) == nil
}

func validateConsoleUndoEntry(project *registeredProject, libraryPath string, entry operation.Entry) error {
	for _, snapshot := range entry.Before {
		if !isConsoleUndoPath(project, libraryPath, entry, snapshot.Path) {
			return fmt.Errorf("%w: operation journal includes a path outside console-managed resource locations", lifecycle.ErrUnsafePath)
		}
	}
	return nil
}

func isConsoleUndoPath(project *registeredProject, libraryPath string, entry operation.Entry, path string) bool {
	switch entry.ResourceKind {
	case string(resource.Skill):
		if _, _, ok := adapter.MatchProjectSkillPath(project.Path, path); ok {
			return true
		}
		if entry.Operation == "update managed-link Git guidance" && path == filepath.Join(project.Path, ".gitignore") {
			return true
		}
		// The console is project-scoped and never restores a project journal
		// snapshot into the shared Skill library.
		return false
	case string(resource.SubAgent):
		if entry.Operation != "place resource" && entry.Operation != "remove SubAgent" {
			return false
		}
		relative, err := filepath.Rel(project.Path, path)
		if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false
		}
		parent, name := filepath.Dir(relative), filepath.Base(relative)
		id, extension := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
		return (parent == filepath.Join(".claude", "agents") && extension == ".md" || parent == filepath.Join(".codex", "agents") && extension == ".toml") && adapter.ValidateIdentifier(id) == nil
	default:
		return false
	}
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func viewProject(project *registeredProject) projectDTO {
	return projectDTO{ID: project.ID, Name: project.Name, Path: project.Path}
}
