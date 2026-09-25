//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package adapter

import (
	"errors"
	"fmt"
)

// Keep the interface implementation explicit on unsupported platforms.
var _ anchoredPublicationDirectory = (*unsupportedPublicationDirectory)(nil)

type unsupportedPublicationDirectory struct{}

func (*unsupportedPublicationDirectory) Close() error  { return nil }
func (*unsupportedPublicationDirectory) Current() bool { return false }
func (*unsupportedPublicationDirectory) Exists(string) (bool, error) {
	return false, errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) Readlink(string) (string, error) {
	return "", errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) ReadFile(string) ([]byte, error) {
	return nil, errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) Symlink(string, string) error {
	return errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) WriteFile(string, []byte) error {
	return errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) Remove(string) error { return errors.New("unsupported") }
func (*unsupportedPublicationDirectory) RenameFrom(string, string) error {
	return errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) NewStage() (anchoredPublicationStage, error) {
	return nil, errors.New("unsupported")
}
func (*unsupportedPublicationDirectory) Path() string { return "" }

func openAnchoredPublicationDirectory(_, path string) (anchoredPublicationDirectory, error) {
	return nil, errors.Join(ErrUnsafePath, fmt.Errorf("guarded filesystem publication is unsupported on this operating system for %q", path))
}

func openAnchoredPublicationDirectoryNoCreate(project, path string) (anchoredPublicationDirectory, error) {
	return openAnchoredPublicationDirectory(project, path)
}
