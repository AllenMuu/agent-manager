//go:build windows

package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows does not expose directory-relative symlink creation through the Go
// API. Holding every directory in the resolved chain without FILE_SHARE_DELETE
// provides the equivalent guarantee: none of the path components can be
// renamed or replaced until publication and rollback are complete.
type windowsPublicationDirectory struct {
	path    string
	handles []windows.Handle
}

type windowsPublicationStage struct {
	parent *windowsPublicationDirectory
	root   string
	trash  string
	closed bool
}

func openAnchoredPublicationDirectory(_ string, path string) (anchoredPublicationDirectory, error) {
	return openAnchoredPublicationDirectoryMode(path, true)
}

func openAnchoredPublicationDirectoryNoCreate(_ string, path string) (anchoredPublicationDirectory, error) {
	return openAnchoredPublicationDirectoryMode(path, false)
}

func openAnchoredPublicationDirectoryMode(path string, create bool) (anchoredPublicationDirectory, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	volume := filepath.VolumeName(abs)
	root := volume + string(filepath.Separator)
	if volume == "" || !filepath.IsAbs(abs) {
		return nil, errors.Join(ErrUnsafePath, fmt.Errorf("placement parent is not absolute: %q", path))
	}
	current := root
	handles := make([]windows.Handle, 0, 8)
	closeHandles := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(handles[i])
		}
	}
	openCurrent := func() error {
		handle, openErr := openWindowsDirectoryNoFollow(current)
		if openErr != nil {
			return openErr
		}
		handles = append(handles, handle)
		return nil
	}
	if err := openCurrent(); err != nil {
		return nil, errors.Join(ErrUnsafePath, fmt.Errorf("open placement volume root: %w", err))
	}
	relative := strings.TrimPrefix(abs, root)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if create {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				closeHandles()
				return nil, fmt.Errorf("create guarded placement parent %q: %w", component, err)
			}
		} else if _, err := os.Lstat(current); err != nil {
			closeHandles()
			return nil, errors.Join(ErrUnsafePath, os.ErrNotExist, fmt.Errorf("placement parent %q is missing", component))
		}
		if err := openCurrent(); err != nil {
			closeHandles()
			return nil, errors.Join(ErrUnsafePath, fmt.Errorf("open guarded placement parent %q without following links: %w", component, err))
		}
	}
	return &windowsPublicationDirectory{path: abs, handles: handles}, nil
}

func openWindowsDirectoryNoFollow(path string) (windows.Handle, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, fmt.Errorf("path is not a real directory")
	}
	return handle, nil
}

func (d *windowsPublicationDirectory) Close() error {
	var result error
	for i := len(d.handles) - 1; i >= 0; i-- {
		result = errors.Join(result, windows.CloseHandle(d.handles[i]))
	}
	d.handles = nil
	return result
}

func (d *windowsPublicationDirectory) Current() bool {
	info, err := os.Lstat(d.path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func (d *windowsPublicationDirectory) Exists(name string) (bool, error) {
	_, err := os.Lstat(filepath.Join(d.path, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (d *windowsPublicationDirectory) Readlink(name string) (string, error) {
	return os.Readlink(filepath.Join(d.path, name))
}

func (d *windowsPublicationDirectory) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.path, name))
}

func (d *windowsPublicationDirectory) Symlink(target, name string) error {
	return os.Symlink(target, filepath.Join(d.path, name))
}

func (d *windowsPublicationDirectory) WriteFile(name string, content []byte) error {
	file, err := os.OpenFile(filepath.Join(d.path, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(filepath.Join(d.path, name))
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(filepath.Join(d.path, name))
		return err
	}
	return file.Close()
}

func (d *windowsPublicationDirectory) Remove(name string) error {
	return os.RemoveAll(filepath.Join(d.path, name))
}

func (d *windowsPublicationDirectory) RenameFrom(source, name string) error {
	return os.Rename(source, filepath.Join(d.path, name))
}

func (d *windowsPublicationDirectory) NewStage() (anchoredPublicationStage, error) {
	root, err := os.MkdirTemp(d.path, ".skill-manager-replace-")
	if err != nil {
		return nil, err
	}
	return &windowsPublicationStage{parent: d, root: root}, nil
}

func (d *windowsPublicationDirectory) Path() string { return d.path }

func (s *windowsPublicationStage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := os.Remove(s.root)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) {
		return nil
	}
	return err
}

func (s *windowsPublicationStage) MoveFrom(name string) error {
	return os.Rename(filepath.Join(s.parent.path, name), filepath.Join(s.root, "existing"))
}

func (s *windowsPublicationStage) Matches(backup string) bool {
	return pathsMatch(filepath.Join(s.root, "existing"), backup)
}

func (s *windowsPublicationStage) RestoreAs(name string) error {
	from := filepath.Join(s.root, "existing")
	if s.trash != "" {
		from = s.trash
	}
	err := os.Rename(from, filepath.Join(s.parent.path, name))
	if err == nil {
		s.trash = ""
	}
	return err
}

func (s *windowsPublicationStage) Discard() error {
	if s.closed {
		return nil
	}
	// Move the original to an atomic trash sibling before cleanup. A failed
	// cleanup leaves the complete original at trash for recovery.
	trash := s.root + ".trash"
	if err := os.Rename(filepath.Join(s.root, "existing"), trash); err != nil {
		return err
	}
	s.trash = trash
	if err := os.Remove(s.root); err != nil {
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	s.trash = ""
	s.closed = true
	return nil
}

func (s *windowsPublicationStage) Path() string {
	if s.trash != "" {
		return s.trash
	}
	return filepath.Join(s.root, "existing")
}
