//go:build windows

package memory

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openMemoryFileNoFollow(path string) (*os.File, error) {
	handle, info, err := openMemoryHandle(path, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("path is not a direct regular file")
	}
	return os.NewFile(uintptr(handle), path), nil
}

func openMemoryFileAppend(path string) (*os.File, error) {
	// FILE_APPEND_DATA guarantees that each write lands at EOF. Rollback opens
	// a separate GENERIC_WRITE handle through truncateMemoryFile.
	handle, info, err := openMemoryHandle(path, windows.FILE_APPEND_DATA|windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("path is not a direct regular file")
	}
	return os.NewFile(uintptr(handle), path), nil
}

func truncateMemoryFile(_ *os.File, path string, identity os.FileInfo, size int64) error {
	handle, info, err := openMemoryHandle(path, windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	truncateFile := os.NewFile(uintptr(handle), path)
	if truncateFile == nil {
		_ = windows.CloseHandle(handle)
		return fmt.Errorf("unable to create truncate handle")
	}
	defer truncateFile.Close()
	pathInfo, err := os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("Memory store is not a direct regular file")
	}
	openedInfo, err := truncateFile.Stat()
	if err != nil || identity == nil || !os.SameFile(identity, openedInfo) || !os.SameFile(pathInfo, openedInfo) {
		return fmt.Errorf("Memory store identity changed before truncate")
	}
	if err := truncateFile.Truncate(size); err != nil {
		return err
	}
	return truncateFile.Sync()
}

func openMemoryHandle(path string, access uint32) (windows.Handle, windows.ByHandleFileInformation, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, windows.ByHandleFileInformation{}, err
	}
	handle, err := windows.CreateFile(
		pointer,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return 0, windows.ByHandleFileInformation{}, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, windows.ByHandleFileInformation{}, err
	}
	return handle, info, nil
}
