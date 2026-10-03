// Package initcmd owns the explicit global-baseline initialization operation.
package initcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

const operatorIdentifier = "skill-manager-operator"
const ownershipMarker = ".skill-manager-owner"
const ownershipValue = "skill-manager/operator/v1\n"
const operatorSkill = "---\nname: Agent Manager Operator\ndescription: Guide safe local Agent Manager use.\n---\nUse structured `agent-manager search --json` before choosing a skill. Explain each recommendation and its rationale to the human. Obtain explicit human confirmation before any mutation, then invoke the Agent Manager CLI.\n"

// Service performs the only operation allowed to modify global agent locations.
type Service struct {
	Home    string
	Journal *operation.Journal
	confirm func(operation.Plan) bool
	verify  func() error
	// BeforePublish is an optional fault-injection seam.
	BeforePublish func(string) error
}

func New(home, journalPath string, confirm func(operation.Plan) bool, verify func() error) *Service {
	return &Service{Home: home, Journal: operation.New(journalPath), confirm: confirm, verify: verify}
}
func (s *Service) Initialize() (operation.Plan, error) {
	if s.verify == nil {
		return operation.Plan{}, errors.New("CLI availability verifier is not configured")
	}
	if err := s.verify(); err != nil {
		return operation.Plan{}, fmt.Errorf("verify CLI availability: %w", err)
	}
	plan := operation.NewPlan("init")
	paths := make([]string, 0, 2)
	snapshotPaths := make([]string, 0, 4)
	for _, a := range adapter.Supported() {
		path := filepath.Join(a.GlobalSkillPath(s.Home, operatorIdentifier), "SKILL.md")
		if err := safeOperatorPath(filepath.Dir(path)); err != nil {
			return plan, err
		}
		paths = append(paths, path)
		snapshotPaths = append(snapshotPaths, path, filepath.Join(filepath.Dir(path), ownershipMarker))
		plan.Changes = append(plan.Changes, operation.Change{Path: path, Action: "install or update Operator skill"})
	}
	if s.confirm == nil || !s.confirm(plan) {
		return plan, operation.ErrNotConfirmed
	}
	before, err := s.Journal.Capture(snapshotPaths)
	if err != nil {
		return plan, err
	}
	newAgentRoots := []string{}
	for _, path := range paths {
		root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			newAgentRoots = append(newAgentRoots, root)
		}
	}
	type publication struct {
		before operation.Snapshot
		info   os.FileInfo
		digest string
	}
	published := make([]publication, 0, len(before))
	rollback := func(err error) error {
		var restore error
		var owned []operation.Snapshot
		for _, item := range published {
			info, statErr := os.Lstat(item.before.Path)
			digest, digestErr := operation.FingerprintPath(item.before.Path)
			if statErr != nil || digestErr != nil || !os.SameFile(item.info, info) || digest != item.digest {
				restore = errors.Join(restore, fmt.Errorf("preserving changed publication %s: %w", item.before.Path, operation.ErrUnexpectedState))
				continue
			}
			owned = append(owned, item.before)
		}
		restore = errors.Join(restore, s.Journal.Restore(owned))
		for _, root := range newAgentRoots {
			if removeErr := removeEmptyDirectories(root); removeErr != nil {
				restore = errors.Join(restore, removeErr)
			}
		}
		return errors.Join(err, restore)
	}
	publish := func(stage, path string, snapshot operation.Snapshot) error {
		info, err := os.Lstat(stage)
		if err != nil {
			return err
		}
		digest, err := operation.FingerprintPath(stage)
		if err != nil {
			return err
		}
		if err := os.Rename(stage, path); err != nil {
			return err
		}
		published = append(published, publication{snapshot, info, digest})
		return nil
	}
	type staged struct{ skill, marker string }
	stages := make([]staged, len(paths))
	writeStage := func(directory, prefix string, contents []byte) (string, error) {
		f, err := os.CreateTemp(directory, prefix)
		if err != nil {
			return "", err
		}
		name := f.Name()
		if _, err = f.Write(contents); err == nil {
			err = f.Close()
		} else {
			_ = f.Close()
		}
		if err != nil {
			_ = os.Remove(name)
		}
		return name, err
	}
	for i, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return plan, rollback(err)
		}
		skill, err := writeStage(filepath.Dir(path), ".skill-manager-operator-", []byte(operatorSkill))
		if err != nil {
			return plan, rollback(err)
		}
		marker, err := writeStage(filepath.Dir(path), ".skill-manager-owner-", []byte(ownershipValue))
		if err != nil {
			_ = os.Remove(skill)
			return plan, rollback(err)
		}
		stages[i] = staged{skill, marker}
	}
	defer func() {
		for _, stage := range stages {
			_ = os.Remove(stage.skill)
			_ = os.Remove(stage.marker)
		}
		for _, root := range newAgentRoots {
			_ = removeEmptyDirectories(root)
		}
	}()
	for i, path := range paths {
		if s.BeforePublish != nil {
			if err := s.BeforePublish(path); err != nil {
				return plan, rollback(err)
			}
		}
		// Revalidate after the user has confirmed: a concurrent unmanaged
		// replacement must not be overwritten by this global operation.
		if err := safeOperatorPath(filepath.Dir(path)); err != nil {
			return plan, rollback(err)
		}
		if err := publish(stages[i].skill, path, before[2*i]); err != nil {
			return plan, rollback(err)
		}
		stages[i].skill = ""
		if err := publish(stages[i].marker, filepath.Join(filepath.Dir(path), ownershipMarker), before[2*i+1]); err != nil {
			return plan, rollback(err)
		}
		stages[i].marker = ""
	}
	after, err := s.Journal.Capture(snapshotPaths)
	if err != nil {
		return plan, rollback(err)
	}
	if err := s.Journal.RecordPlan(plan, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return plan, err
		}
		return plan, rollback(err)
	}
	return plan, nil
}

// removeEmptyDirectories removes only directories made empty by rollback; an
// unrelated concurrent file leaves its containing tree intact.
func removeEmptyDirectories(root string) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil
		}
		if err := removeEmptyDirectories(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return os.Remove(root)
	}
	return nil
}

func safeOperatorPath(directory string) error {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing unmanaged global Operator path %s", directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".skill-manager-operator-") || strings.HasPrefix(entry.Name(), ".skill-manager-owner-") {
			continue
		}
		filtered = append(filtered, entry)
	}
	entries = filtered
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > 2 {
		return fmt.Errorf("refusing unmanaged global Operator directory %s", directory)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Name()] = true
	}
	if !seen["SKILL.md"] || (len(entries) == 2 && !seen[ownershipMarker]) {
		return fmt.Errorf("refusing unmanaged global Operator directory %s", directory)
	}
	owned := seen[ownershipMarker]
	if owned {
		markerPath := filepath.Join(directory, ownershipMarker)
		markerInfo, err := os.Lstat(markerPath)
		if err != nil {
			return err
		}
		if !markerInfo.Mode().IsRegular() {
			return fmt.Errorf("refusing unmanaged global Operator marker %s", directory)
		}
		marker, err := os.ReadFile(markerPath)
		if err != nil {
			return err
		}
		if string(marker) != ownershipValue {
			return fmt.Errorf("refusing unmanaged global Operator marker %s", directory)
		}
	}
	file := filepath.Join(directory, "SKILL.md")
	info, err = os.Lstat(file)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing unmanaged global Operator file %s", file)
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if !owned && (len(contents) == 0 || string(contents) != operatorSkill) {
		return fmt.Errorf("refusing unmanaged global Operator file %s", file)
	}
	return nil
}

// VerifyCLI confirms that the executable can actually be invoked.
func VerifyCLI(path string) func() error {
	return func() error { return exec.Command(path, "--help").Run() }
}
