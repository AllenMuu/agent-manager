//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package adapter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type unixPublicationDirectory struct {
	file *os.File
	path string
}

type unixPublicationStage struct {
	parent     *unixPublicationDirectory
	file       *os.File
	name       string
	trash      string
	closed     bool
	fileClosed bool
}

func openAnchoredPublicationDirectory(project, path string) (anchoredPublicationDirectory, error) {
	return openAnchoredPublicationDirectoryMode(project, path, true)
}

func openAnchoredPublicationDirectoryNoCreate(project, path string) (anchoredPublicationDirectory, error) {
	return openAnchoredPublicationDirectoryMode(project, path, false)
}

func openAnchoredPublicationDirectoryMode(project, path string, create bool) (anchoredPublicationDirectory, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	project = filepath.Clean(project)
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	parentRel, err := filepath.Rel(project, abs)
	if err != nil || parentRel == ".." || strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
		return nil, errors.Join(ErrUnsafePath, fmt.Errorf("placement parent escapes project root"))
	}

	anchor := project
	missing := make([]string, 0, 4)
	for {
		info, inspectErr := os.Lstat(anchor)
		if inspectErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, errors.Join(ErrUnsafePath, fmt.Errorf("project ancestor %q must be a real directory", anchor))
			}
			break
		}
		if !errors.Is(inspectErr, os.ErrNotExist) {
			return nil, inspectErr
		}
		next := filepath.Dir(anchor)
		if next == anchor {
			return nil, errors.Join(ErrUnsafePath, fmt.Errorf("no real project ancestor for %q", project))
		}
		missing = append(missing, filepath.Base(anchor))
		anchor = next
	}

	fd, err := unix.Open(anchor, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.Join(ErrUnsafePath, fmt.Errorf("open project ancestor %q: %w", anchor, err))
	}
	components := make([]string, 0, len(missing)+4)
	for i := len(missing) - 1; i >= 0; i-- {
		components = append(components, missing[i])
	}
	if parentRel != "." {
		components = append(components, strings.Split(parentRel, string(filepath.Separator))...)
	}
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if !create {
				_ = unix.Close(fd)
				return nil, errors.Join(ErrUnsafePath, os.ErrNotExist, fmt.Errorf("placement parent %q is missing", component))
			}
			if mkdirErr := unix.Mkdirat(fd, component, 0o755); mkdirErr != nil {
				_ = unix.Close(fd)
				if errors.Is(mkdirErr, unix.EEXIST) {
					return nil, errors.Join(ErrUnsafePath, errLateConflict, fmt.Errorf("placement parent %q appeared concurrently", component))
				}
				return nil, fmt.Errorf("create guarded placement parent %q: %w", component, mkdirErr)
			}
			next, openErr = unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			_ = unix.Close(fd)
			return nil, errors.Join(ErrUnsafePath, fmt.Errorf("open guarded placement parent %q without following links: %w", component, openErr))
		}
		_ = unix.Close(fd)
		fd = next
	}
	return &unixPublicationDirectory{file: os.NewFile(uintptr(fd), abs), path: abs}, nil
}

func (d *unixPublicationDirectory) Close() error { return d.file.Close() }

func (d *unixPublicationDirectory) Current() bool {
	anchored, err := d.file.Stat()
	if err != nil {
		return false
	}
	current, err := os.Lstat(d.path)
	return err == nil && current.Mode()&os.ModeSymlink == 0 && current.IsDir() && os.SameFile(anchored, current)
}

func (d *unixPublicationDirectory) Exists(name string) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(int(d.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	return err == nil, err
}

func (d *unixPublicationDirectory) Readlink(name string) (string, error) {
	return readlinkAt(int(d.file.Fd()), name)
}

func (d *unixPublicationDirectory) ReadFile(name string) ([]byte, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(d.path, name))
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	return content, errors.Join(readErr, closeErr)
}

func (d *unixPublicationDirectory) Symlink(target, name string) error {
	return unix.Symlinkat(target, int(d.file.Fd()), name)
}

func (d *unixPublicationDirectory) WriteFile(name string, content []byte) error {
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(d.path, name))
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(d.file.Fd()), name, 0)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(d.file.Fd()), name, 0)
		return err
	}
	return file.Close()
}

func (d *unixPublicationDirectory) Remove(name string) error {
	return removeTreeAt(int(d.file.Fd()), name)
}

func (d *unixPublicationDirectory) RenameFrom(source, name string) error {
	return unix.Renameat(unix.AT_FDCWD, source, int(d.file.Fd()), name)
}

func (d *unixPublicationDirectory) NewStage() (anchoredPublicationStage, error) {
	base := fmt.Sprintf(".skill-manager-replace-%d", time.Now().UnixNano())
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		if err := unix.Mkdirat(int(d.file.Fd()), name, 0o700); err != nil {
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			return nil, err
		}
		fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			_ = unix.Unlinkat(int(d.file.Fd()), name, unix.AT_REMOVEDIR)
			return nil, err
		}
		return &unixPublicationStage{parent: d, file: os.NewFile(uintptr(fd), filepath.Join(d.path, name)), name: name}, nil
	}
}

func (d *unixPublicationDirectory) Path() string { return d.path }

func (s *unixPublicationStage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if !s.fileClosed {
		closeErr = s.file.Close()
		s.fileClosed = true
	}
	removeErr := unix.Unlinkat(int(s.parent.file.Fd()), s.name, unix.AT_REMOVEDIR)
	if errors.Is(removeErr, unix.ENOENT) || errors.Is(removeErr, unix.ENOTEMPTY) || errors.Is(removeErr, unix.EEXIST) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func (s *unixPublicationStage) MoveFrom(name string) error {
	return unix.Renameat(int(s.parent.file.Fd()), name, int(s.file.Fd()), "existing")
}

func (s *unixPublicationStage) Matches(backup string) bool {
	return pathsMatchAt(int(s.file.Fd()), "existing", backup)
}

func (s *unixPublicationStage) RestoreAs(name string) error {
	// Use the anchored parent descriptor so recovery remains possible even if
	// the stage directory handle was closed while discard was failing.
	from := filepath.Join(s.name, "existing")
	if s.trash != "" {
		from = s.trash
	}
	err := unix.Renameat(int(s.parent.file.Fd()), from, int(s.parent.file.Fd()), name)
	if err == nil {
		s.trash = ""
	}
	return err
}

func (s *unixPublicationStage) Discard() error {
	if s.closed {
		return nil
	}
	// Atomically move the original out of the stage before cleanup. If cleanup
	// fails, the trash name remains recoverable instead of deleting the backup
	// piecemeal beneath its original stage path.
	closeErr := s.file.Close()
	s.fileClosed = true
	if closeErr != nil {
		return closeErr
	}
	trash := fmt.Sprintf("%s.skill-manager-trash-%d", s.name, time.Now().UnixNano())
	if err := unix.Renameat(int(s.parent.file.Fd()), filepath.Join(s.name, "existing"), int(s.parent.file.Fd()), trash); err != nil {
		return err
	}
	s.trash = trash
	if err := unix.Unlinkat(int(s.parent.file.Fd()), s.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	if err := removeTreeAt(int(s.parent.file.Fd()), trash); err != nil {
		return err
	}
	s.trash = ""
	s.closed = true
	return nil
}

func (s *unixPublicationStage) Path() string {
	if s.trash != "" {
		return filepath.Join(s.parent.path, s.trash)
	}
	return filepath.Join(s.parent.path, s.name, "existing")
}

func removeTreeAt(parentFD int, name string) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if uint32(stat.Mode)&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, name, 0)
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), name)
	entries, readErr := dir.ReadDir(-1)
	if readErr == nil {
		for _, entry := range entries {
			if err := removeTreeAt(fd, entry.Name()); err != nil {
				readErr = err
				break
			}
		}
	}
	closeErr := dir.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}

func pathsMatchAt(parentFD int, name, backup string) bool {
	var currentStat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &currentStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false
	}
	backupInfo, err := os.Lstat(backup)
	if err != nil || fileModeFromUnix(uint32(currentStat.Mode)) != backupInfo.Mode() {
		return false
	}
	switch uint32(currentStat.Mode) & unix.S_IFMT {
	case unix.S_IFLNK:
		currentTarget, err := readlinkAt(parentFD, name)
		if err != nil {
			return false
		}
		backupTarget, err := os.Readlink(backup)
		return err == nil && currentTarget == backupTarget
	case unix.S_IFDIR:
		fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return false
		}
		dir := os.NewFile(uintptr(fd), name)
		currentEntries, currentErr := dir.ReadDir(-1)
		backupEntries, backupErr := os.ReadDir(backup)
		if currentErr != nil || backupErr != nil || len(currentEntries) != len(backupEntries) {
			_ = dir.Close()
			return false
		}
		sort.Slice(currentEntries, func(i, j int) bool { return currentEntries[i].Name() < currentEntries[j].Name() })
		for i, entry := range currentEntries {
			if entry.Name() != backupEntries[i].Name() || !pathsMatchAt(fd, entry.Name(), filepath.Join(backup, entry.Name())) {
				_ = dir.Close()
				return false
			}
		}
		return dir.Close() == nil
	case unix.S_IFREG:
		fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return false
		}
		current := os.NewFile(uintptr(fd), name)
		currentBytes, currentErr := io.ReadAll(current)
		closeErr := current.Close()
		backupBytes, backupErr := os.ReadFile(backup)
		return currentErr == nil && closeErr == nil && backupErr == nil && bytes.Equal(currentBytes, backupBytes)
	default:
		return true
	}
}

func readlinkAt(parentFD int, name string) (string, error) {
	buffer := make([]byte, 128)
	for {
		n, err := unix.Readlinkat(parentFD, name, buffer)
		if err != nil {
			return "", err
		}
		if n < len(buffer) {
			return string(buffer[:n]), nil
		}
		buffer = make([]byte, len(buffer)*2)
	}
}

func fileModeFromUnix(mode uint32) os.FileMode {
	result := os.FileMode(mode & 0o777)
	if mode&unix.S_ISUID != 0 {
		result |= os.ModeSetuid
	}
	if mode&unix.S_ISGID != 0 {
		result |= os.ModeSetgid
	}
	if mode&unix.S_ISVTX != 0 {
		result |= os.ModeSticky
	}
	switch mode & unix.S_IFMT {
	case unix.S_IFBLK:
		result |= os.ModeDevice
	case unix.S_IFCHR:
		result |= os.ModeDevice | os.ModeCharDevice
	case unix.S_IFDIR:
		result |= os.ModeDir
	case unix.S_IFIFO:
		result |= os.ModeNamedPipe
	case unix.S_IFLNK:
		result |= os.ModeSymlink
	case unix.S_IFSOCK:
		result |= os.ModeSocket
	}
	return result
}
