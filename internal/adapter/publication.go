package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

// anchoredPublicationDirectory owns a live reference to the selected
// destination parent. Implementations either publish relative to that reference
// or prevent the directory and its ancestors from being replaced while path
// based operations run.
type anchoredPublicationDirectory interface {
	Close() error
	Current() bool
	Exists(name string) (bool, error)
	Readlink(name string) (string, error)
	ReadFile(name string) ([]byte, error)
	Symlink(target, name string) error
	WriteFile(name string, content []byte) error
	Remove(name string) error
	RenameFrom(source, name string) error
	NewStage() (anchoredPublicationStage, error)
	Path() string
}

type anchoredPublicationStage interface {
	Close() error
	MoveFrom(name string) error
	Matches(backup string) bool
	RestoreAs(name string) error
	Discard() error
	Path() string
}

func publishLink(project, destination, source string, replace bool, snapshot operation.Snapshot, beforeRemove, beforeFinalPublish, beforeDiscard func() error) error {
	parent, err := openAnchoredPublicationDirectory(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()

	name := filepath.Base(destination)
	if !replace {
		if beforeFinalPublish != nil {
			if err := beforeFinalPublish(); err != nil {
				return err
			}
		}
		if !parent.Current() {
			return errors.Join(ErrUnsafePath, errLateConflict)
		}
		if target, err := parent.Readlink(name); err == nil && target == source {
			return nil
		}
		if err := parent.Symlink(source, name); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errors.Join(ErrUnsafePath, errLateConflict)
			}
			return err
		}
		if !parent.Current() {
			removeErr := parent.Remove(name)
			return errors.Join(ErrUnsafePath, errLateConflict, removeErr)
		}
		return nil
	}

	stage, err := parent.NewStage()
	if err != nil {
		return err
	}
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
	}()
	if err := stage.MoveFrom(name); err != nil {
		_ = stage.Discard()
		stageClosed = true
		return err
	}

	preserve := func(cause error) error {
		exists, inspectErr := parent.Exists(name)
		if inspectErr != nil {
			return &PreservedPathError{Path: destination, Err: errors.Join(cause, fmt.Errorf("preserve staged destination: %w (original remains at %s)", inspectErr, stage.Path()))}
		}
		if !exists {
			if restoreErr := stage.RestoreAs(name); restoreErr != nil {
				return &PreservedPathError{Path: destination, Err: errors.Join(cause, fmt.Errorf("preserve staged destination: %w (original remains at %s)", restoreErr, stage.Path()))}
			}
			if closeErr := stage.Close(); closeErr != nil {
				return &PreservedPathError{Path: destination, Err: errors.Join(cause, closeErr)}
			}
			stageClosed = true
			return &PreservedPathError{Path: destination, Err: errors.Join(cause, errStagePreserved)}
		}

		recoveryBase := fmt.Sprintf("%s.skill-manager-recovery-%d", name, time.Now().UnixNano())
		for i := 0; ; i++ {
			recoveryName := recoveryBase
			if i > 0 {
				recoveryName = fmt.Sprintf("%s-%d", recoveryBase, i)
			}
			recoveryExists, recoveryErr := parent.Exists(recoveryName)
			if recoveryErr != nil {
				return &PreservedPathError{Path: destination, Err: errors.Join(cause, fmt.Errorf("preserve staged destination: inspect recovery path: %w (original remains at %s)", recoveryErr, stage.Path()))}
			}
			if recoveryExists {
				continue
			}
			if restoreErr := stage.RestoreAs(recoveryName); restoreErr != nil {
				return &PreservedPathError{Path: destination, Err: errors.Join(cause, fmt.Errorf("preserve staged destination: %w (original remains at %s)", restoreErr, stage.Path()))}
			}
			if closeErr := stage.Close(); closeErr != nil {
				return &PreservedPathError{Path: destination, Err: errors.Join(cause, closeErr)}
			}
			stageClosed = true
			return &PreservedPathError{Path: destination, Err: errors.Join(cause, errStagePreserved, fmt.Errorf("preserved staged destination at %s", filepath.Join(parent.Path(), recoveryName)))}
		}
	}

	// Keep the staged original until publication succeeds. If an error occurs
	// while the destination is free, put it back; if a new owner appears, move
	// it to an explicit sibling recovery path.
	if beforeRemove != nil {
		if err := beforeRemove(); err != nil {
			return preserve(err)
		}
	}
	if exists, err := parent.Exists(name); err != nil {
		return preserve(err)
	} else if exists {
		return preserve(errors.Join(ErrUnsafePath, errLateConflict))
	}
	if !stage.Matches(snapshot.Backup) {
		return preserve(errors.Join(ErrUnsafePath, errLateConflict))
	}
	if beforeFinalPublish != nil {
		if err := beforeFinalPublish(); err != nil {
			return preserve(err)
		}
	}
	if !parent.Current() {
		return preserve(errors.Join(ErrUnsafePath, errLateConflict))
	}
	if exists, err := parent.Exists(name); err != nil {
		return preserve(err)
	} else if exists {
		return preserve(errors.Join(ErrUnsafePath, errLateConflict))
	}
	if err := parent.Symlink(source, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return preserve(errors.Join(ErrUnsafePath, errLateConflict))
		}
		return preserve(err)
	}
	if !parent.Current() {
		removeErr := parent.Remove(name)
		return preserve(errors.Join(ErrUnsafePath, errLateConflict, removeErr))
	}
	var discardErr error
	if beforeDiscard != nil {
		discardErr = beforeDiscard()
	} else {
		discardErr = stage.Discard()
	}
	if discardErr != nil {
		// The replacement is already published, but the staged original could
		// not be discarded. Remove the new link only when it is still anchored
		// here, then restore the staged original or move it to a recovery name.
		// Discard deliberately leaves the stage usable on failure.
		var removeErr error
		if target, readErr := parent.Readlink(name); readErr == nil && target == source {
			removeErr = parent.Remove(name)
		}
		return preserve(errors.Join(discardErr, removeErr))
	}
	stageClosed = true
	return nil
}
