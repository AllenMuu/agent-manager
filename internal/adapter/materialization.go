package adapter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

func MaterializationAction(mode MaterializationMode, replacing bool) string {
	if mode == MaterializeCopyOnly {
		if replacing {
			return "replace conflicting path with a Skill copy"
		}
		return "copy Skill tree"
	}
	if replacing {
		return "replace conflicting path with absolute link"
	}
	return "create absolute link"
}

func MaterializationDetail(source string, mode MaterializationMode) string {
	switch mode {
	case MaterializeAuto:
		return source + " (auto: symlink first, copy fallback)"
	case MaterializeSymlinkOnly:
		return source + " (symlink-only)"
	case MaterializeCopyOnly:
		return source + " (copy-only)"
	default:
		return source
	}
}

func destinationCopyMatches(destination, source string) (bool, error) {
	if err := validatePlainSkillTree(source); err != nil {
		return false, err
	}
	destinationInfo, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if err := validatePlainSkillTree(destination); err != nil {
		return false, nil
	}
	sourceDigest, err := operation.FingerprintPath(source)
	if err != nil {
		return false, err
	}
	destinationDigest, err := operation.FingerprintPath(destination)
	if err != nil {
		return false, err
	}
	return sourceDigest == destinationDigest, nil
}

func publishSkillCopy(project, destination, source string, replace bool, snapshot operation.Snapshot, beforeFinalPublish func() error) error {
	sourceDigest, err := fingerprintPlainSkillTree(source)
	if err != nil {
		return err
	}
	parent, err := openAnchoredPublicationDirectory(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if !parent.Current() {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	name := filepath.Base(destination)
	stage, err := parent.NewStage()
	if err != nil {
		return err
	}
	defer stage.Close()
	stageRoot := filepath.Dir(stage.Path())
	candidate := filepath.Join(stageRoot, "skill-copy")
	if err := copyPlainSkillTree(source, candidate); err != nil {
		return err
	}
	if err := verifyCopyCandidate(source, candidate, sourceDigest); err != nil {
		return err
	}

	if !replace {
		if exists, err := parent.Exists(name); err != nil {
			return err
		} else if exists {
			return errors.Join(ErrUnsafePath, ErrLateConflict)
		}
		if beforeFinalPublish != nil {
			if err := beforeFinalPublish(); err != nil {
				return err
			}
		}
		if !parent.Current() {
			return errors.Join(ErrUnsafePath, ErrLateConflict)
		}
		if exists, err := parent.Exists(name); err != nil {
			return err
		} else if exists {
			return errors.Join(ErrUnsafePath, ErrLateConflict)
		}
		if err := parent.RenameFrom(candidate, name); err != nil {
			return err
		}
		if !parent.Current() {
			if matches, matchErr := fingerprintMatches(filepath.Join(parent.Path(), name), sourceDigest); matchErr == nil && matches {
				_ = parent.Remove(name)
			}
			return errors.Join(ErrUnsafePath, ErrLateConflict)
		}
		return nil
	}

	if !snapshot.Exists || filepath.Clean(snapshot.Path) != filepath.Clean(destination) || !matchesSnapshot(snapshot) {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	if err := stage.MoveFrom(name); err != nil {
		return err
	}
	restoreOriginal := func(cause error) error {
		exists, inspectErr := parent.Exists(name)
		if inspectErr == nil && !exists && parent.Current() {
			if restoreErr := stage.RestoreAs(name); restoreErr == nil {
				return errors.Join(cause, ErrLateConflict)
			} else {
				return errors.Join(cause, ErrLateConflict, fmt.Errorf("restore prior Skill destination: %w (original remains at %s)", restoreErr, stage.Path()))
			}
		}
		recoveryName := fmt.Sprintf("%s.skill-manager-recovery-%d", name, time.Now().UnixNano())
		if recoveryErr := stage.RestoreAs(recoveryName); recoveryErr != nil {
			return errors.Join(cause, ErrLateConflict, fmt.Errorf("preserve prior Skill destination: %w (original remains at %s)", recoveryErr, stage.Path()))
		}
		return errors.Join(cause, ErrLateConflict, fmt.Errorf("preserved prior Skill destination at %s", filepath.Join(parent.Path(), recoveryName)))
	}
	if !parent.Current() || !stage.Matches(snapshot.Backup) {
		return restoreOriginal(errors.Join(ErrUnsafePath, ErrLateConflict))
	}
	if beforeFinalPublish != nil {
		if err := beforeFinalPublish(); err != nil {
			return restoreOriginal(err)
		}
	}
	if exists, err := parent.Exists(name); err != nil {
		return restoreOriginal(err)
	} else if exists {
		return restoreOriginal(errors.Join(ErrUnsafePath, ErrLateConflict))
	}
	if err := verifyCopyCandidate(source, candidate, sourceDigest); err != nil {
		return restoreOriginal(err)
	}
	if err := parent.RenameFrom(candidate, name); err != nil {
		return restoreOriginal(err)
	}
	if !parent.Current() {
		if matches, matchErr := fingerprintMatches(filepath.Join(parent.Path(), name), sourceDigest); matchErr == nil && matches {
			_ = parent.Remove(name)
		}
		return restoreOriginal(errors.Join(ErrUnsafePath, ErrLateConflict))
	}
	if err := stage.Discard(); err != nil {
		if matches, matchErr := fingerprintMatches(filepath.Join(parent.Path(), name), sourceDigest); matchErr == nil && matches && parent.Current() {
			_ = parent.Remove(name)
		}
		return restoreOriginal(fmt.Errorf("discard replaced Skill destination: %w", err))
	}
	return nil
}

// RemoveManagedCopy removes a copy only after moving it into an anchored
// recovery stage and verifying its exact journal-recorded fingerprint.
func RemoveManagedCopy(project, destination, expectedFingerprint string) error {
	if expectedFingerprint == "" {
		return ErrUnsafePath
	}
	parent, err := openAnchoredPublicationDirectoryNoCreate(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if !parent.Current() {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	name := filepath.Base(destination)
	stage, err := parent.NewStage()
	if err != nil {
		return err
	}
	defer stage.Close()
	if exists, err := parent.Exists(name); err != nil {
		return err
	} else if !exists {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	if err := stage.MoveFrom(name); err != nil {
		return err
	}
	restore := func(cause error) error {
		if !parent.Current() {
			return errors.Join(cause, ErrUnsafePath, ErrLateConflict, fmt.Errorf("managed copy remains staged at %s", stage.Path()))
		}
		if exists, inspectErr := parent.Exists(name); inspectErr == nil && !exists {
			if restoreErr := stage.RestoreAs(name); restoreErr == nil {
				return errors.Join(cause, ErrUnsafePath)
			} else {
				return errors.Join(cause, ErrUnsafePath, fmt.Errorf("restore managed copy: %w (staged copy remains at %s)", restoreErr, stage.Path()))
			}
		}
		recoveryName := fmt.Sprintf("%s.skill-manager-recovery-%d", name, time.Now().UnixNano())
		if restoreErr := stage.RestoreAs(recoveryName); restoreErr != nil {
			return errors.Join(cause, ErrUnsafePath, fmt.Errorf("preserve managed copy: %w (staged copy remains at %s)", restoreErr, stage.Path()))
		}
		return errors.Join(cause, ErrUnsafePath, fmt.Errorf("preserved managed copy at %s", filepath.Join(parent.Path(), recoveryName)))
	}
	if !parent.Current() {
		return restore(errors.Join(ErrUnsafePath, ErrLateConflict))
	}
	actual, err := fingerprintPlainSkillTree(stage.Path())
	if err != nil || actual != expectedFingerprint {
		return restore(errors.Join(ErrUnsafePath, err))
	}
	if exists, err := parent.Exists(name); err != nil {
		return restore(err)
	} else if exists {
		return restore(errors.Join(ErrUnsafePath, ErrLateConflict))
	}
	if err := stage.Discard(); err != nil {
		return fmt.Errorf("discard verified managed copy: %w (copy may remain staged at %s)", err, stage.Path())
	}
	return nil
}

func fingerprintMatches(path, digest string) (bool, error) {
	actual, err := fingerprintPlainSkillTree(path)
	if err != nil {
		return false, err
	}
	return actual == digest, nil
}

func verifyCopyCandidate(source, candidate, expectedSourceDigest string) error {
	actualSource, err := fingerprintPlainSkillTree(source)
	if err != nil {
		return err
	}
	if actualSource != expectedSourceDigest {
		return errors.Join(ErrUnsafePath, fmt.Errorf("Skill source changed while it was copied"))
	}
	actualCopy, err := fingerprintPlainSkillTree(candidate)
	if err != nil {
		return err
	}
	if actualCopy != expectedSourceDigest {
		return errors.Join(ErrUnsafePath, fmt.Errorf("staged Skill copy does not match its source"))
	}
	return nil
}

func fingerprintPlainSkillTree(path string) (string, error) {
	if err := validatePlainSkillTree(path); err != nil {
		return "", err
	}
	return operation.FingerprintPath(path)
}

func validatePlainSkillTree(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(ErrUnsafePath, fmt.Errorf("Skill copy source must be a real directory"))
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		childInfo, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrUnsafePath, fmt.Errorf("Skill copy tree contains a symbolic link at %s", child))
		}
		if childInfo.IsDir() {
			if err := validatePlainSkillTree(child); err != nil {
				return err
			}
			continue
		}
		if !childInfo.Mode().IsRegular() {
			return errors.Join(ErrUnsafePath, fmt.Errorf("Skill copy tree contains a special file at %s", child))
		}
	}
	return nil
}

func copyPlainSkillTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		entryInfo, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if entryInfo.IsDir() && entryInfo.Mode()&os.ModeSymlink == 0 {
			if err := copyPlainSkillTree(from, to); err != nil {
				return err
			}
			continue
		}
		if !entryInfo.Mode().IsRegular() || entryInfo.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrUnsafePath, fmt.Errorf("Skill copy tree contains an unsafe entry at %s", from))
		}
		in, err := os.Open(from)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entryInfo.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := errors.Join(in.Close(), out.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	return os.Chmod(destination, info.Mode().Perm())
}
