package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const FileProviderID = "file"

var (
	// ErrProviderUnavailable identifies a configured provider that cannot be
	// discovered locally. The returned status includes an operator-facing
	// reason so callers can report remediation without exposing references.
	ErrProviderUnavailable = errors.New("memory provider unavailable")
	// ErrFileProviderPromotionUnsupported identifies an uninitialized provider
	// that cannot accept explicit promotion.
	ErrFileProviderPromotionUnsupported = errors.New("file-backed memory promotion is not enabled")
)

// FileProvider is a local capability boundary for a user-owned file-backed
// Memory store. Discovery is read-only; promotion is the only explicit write
// path and never resolves network URLs, starts a server, or installs deps.
type FileProvider struct {
	path     string
	identity os.FileInfo
	status   ProviderStatus
}

var (
	fileAppendWrite    = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	fileAppendSync     = func(file *os.File) error { return file.Sync() }
	fileAppendClose    = func(file *os.File) error { return file.Close() }
	fileAppendTruncate = func(file *os.File, size int64) error { return file.Truncate(size) }
)

// NewFileProvider validates and discovers a local file-backed provider. On an
// unavailable provider it returns the status-bearing provider alongside the
// error so a caller can present actionable diagnostics without retrying.
func NewFileProvider(config ProviderConfig) (*FileProvider, error) {
	status, identity, err := discoverFileProvider(config)
	provider := &FileProvider{status: status, identity: identity}
	if err == nil {
		provider.path = config.Configuration.Name
	}
	return provider, err
}

// DiscoverFileProvider checks only local filesystem state and reports the
// capabilities and scopes declared by a valid file-backed configuration.
func DiscoverFileProvider(config ProviderConfig) (ProviderStatus, error) {
	status, _, err := discoverFileProvider(config)
	return status, err
}

func discoverFileProvider(config ProviderConfig) (ProviderStatus, os.FileInfo, error) {
	if err := config.Validate(); err != nil {
		return unavailableStatus(fmt.Sprintf("invalid Memory provider configuration: %v", err)), nil, err
	}
	if config.Provider != FileProviderID {
		err := fmt.Errorf("%w: provider %q is not the local file provider", ErrProviderUnavailable, config.Provider)
		return ProviderStatus{Unsupported: true, Reason: "configured provider is not the local file provider"}, nil, err
	}
	if config.Configuration.Kind != "file" {
		err := fmt.Errorf("%w: file provider requires a file configuration reference", ErrProviderUnavailable)
		return unavailableStatus("file provider requires configuration.kind=file"), nil, err
	}
	if !filepath.IsAbs(config.Configuration.Name) {
		err := fmt.Errorf("%w: file provider path must be absolute", ErrProviderUnavailable)
		return unavailableStatus("file provider path must be absolute; update the external configuration reference"), nil, err
	}
	info, err := os.Lstat(config.Configuration.Name)
	if err != nil {
		reason := "file-backed Memory store is unavailable; create it or update the external configuration reference"
		return unavailableStatus(reason), nil, fmt.Errorf("%w: file-backed Memory store is unavailable", ErrProviderUnavailable)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		err := fmt.Errorf("%w: file-backed Memory store must not be a symlink", ErrProviderUnavailable)
		return unavailableStatus("file-backed Memory store must be a direct regular file, not a symlink"), nil, err
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("%w: file-backed Memory store is not a regular file", ErrProviderUnavailable)
		return unavailableStatus("file-backed Memory store must be a regular file"), nil, err
	}
	file, err := openMemoryFileNoFollow(config.Configuration.Name)
	if err != nil {
		reason := "file-backed Memory store cannot be opened; check its permissions"
		return unavailableStatus(reason), nil, fmt.Errorf("%w: file-backed Memory store cannot be opened", ErrProviderUnavailable)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return unavailableStatus("file-backed Memory store could not be inspected after opening"), nil, fmt.Errorf("%w: file-backed Memory store could not be inspected", ErrProviderUnavailable)
	}
	if openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return unavailableStatus("file-backed Memory store changed while it was being opened; retry after restoring a direct regular file"), nil, fmt.Errorf("%w: file-backed Memory store identity changed during discovery", ErrProviderUnavailable)
	}
	if err := file.Close(); err != nil {
		return unavailableStatus("file-backed Memory store could not be closed after discovery"), nil, fmt.Errorf("%w: file-backed Memory store could not be closed", ErrProviderUnavailable)
	}
	return ProviderStatus{
		Available:    true,
		Capabilities: append([]Capability(nil), config.Capabilities...),
		Scopes:       append([]Scope(nil), config.Scopes...),
	}, openedInfo, nil
}

func unavailableStatus(reason string) ProviderStatus {
	return ProviderStatus{Reason: reason}
}

func (p *FileProvider) Status() ProviderStatus {
	if p == nil {
		return unavailableStatus("file-backed Memory provider is not initialized")
	}
	status := p.status
	status.Capabilities = append([]Capability(nil), status.Capabilities...)
	status.Scopes = append([]Scope(nil), status.Scopes...)
	return status
}

// Promote appends one explicitly requested knowledge item to the provider.
// It requires both a declared write capability and the requested scope. The
// caller owns confirmation; this method only performs the provider mutation.
func (p *FileProvider) Promote(scope Scope, knowledge string) error {
	if p == nil || p.path == "" {
		return ErrFileProviderPromotionUnsupported
	}
	if !p.status.Available {
		return ErrProviderUnavailable
	}
	if !containsScope(p.status.Scopes, scope) {
		return fmt.Errorf("memory provider does not support scope %q", scope)
	}
	if !containsCapability(p.status.Capabilities, CapabilityWrite) {
		return fmt.Errorf("memory provider does not support write capability")
	}
	if strings.TrimSpace(knowledge) == "" {
		return fmt.Errorf("knowledge must not be empty")
	}

	file, err := openMemoryFileAppend(p.path)
	if err != nil {
		return fmt.Errorf("open Memory store for promotion: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = fileAppendClose(file)
		return fmt.Errorf("inspect Memory store for promotion: %w", err)
	}
	pathInfo, err := os.Lstat(p.path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, info) || (p.identity != nil && !os.SameFile(p.identity, info)) {
		_ = fileAppendClose(file)
		return fmt.Errorf("Memory store changed while preparing promotion")
	}
	originalOffset := info.Size()
	record := []byte(knowledge)
	if !strings.HasSuffix(knowledge, "\n") {
		record = append(record, '\n')
	}
	for written := 0; written < len(record); {
		n, writeErr := fileAppendWrite(file, record[written:])
		if n < 0 || n > len(record)-written {
			writeErr = fmt.Errorf("invalid short write count %d", n)
			n = 0
		}
		written += n
		if writeErr != nil {
			return rollbackPromotion(file, p.path, p.identity, originalOffset, originalOffset+int64(written), fmt.Errorf("append Memory knowledge: %w", writeErr))
		}
		if n == 0 {
			return rollbackPromotion(file, p.path, p.identity, originalOffset, originalOffset+int64(written), fmt.Errorf("append Memory knowledge: %w", io.ErrShortWrite))
		}
	}
	if err := fileAppendSync(file); err != nil {
		return rollbackPromotion(file, p.path, p.identity, originalOffset, originalOffset+int64(len(record)), fmt.Errorf("sync Memory knowledge: %w", err))
	}
	if err := fileAppendClose(file); err != nil {
		return rollbackPromotion(file, p.path, p.identity, originalOffset, originalOffset+int64(len(record)), fmt.Errorf("close Memory store after promotion: %w", err))
	}
	return nil
}

func rollbackPromotion(file *os.File, path string, identity os.FileInfo, originalOffset, expectedEnd int64, primary error) error {
	rollbackFile := file
	info, statErr := rollbackFile.Stat()
	if statErr != nil {
		rollbackFile, statErr = openMemoryFileAppend(path)
		if statErr == nil {
			info, statErr = rollbackFile.Stat()
		}
	}
	if statErr != nil {
		if rollbackFile != nil {
			_ = fileAppendClose(rollbackFile)
		}
		return fmt.Errorf("%w; recovery warning: unable to re-open Memory store for rollback: %v; preserving current data", primary, statErr)
	}
	pathInfo, pathErr := os.Lstat(path)
	if pathErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || identity == nil || !os.SameFile(identity, info) || !os.SameFile(pathInfo, info) || info.Size() != expectedEnd {
		_ = fileAppendClose(rollbackFile)
		return fmt.Errorf("%w; recovery warning: Memory store identity or size changed before rollback; preserving current data", primary)
	}
	var rollbackErr error
	if err := truncateMemoryFile(rollbackFile, path, identity, originalOffset); err != nil {
		rollbackErr = err
	}
	if err := fileAppendSync(rollbackFile); err != nil && rollbackErr == nil {
		rollbackErr = err
	}
	if err := fileAppendClose(rollbackFile); err != nil && rollbackErr == nil {
		rollbackErr = err
	}
	if rollbackErr != nil {
		return fmt.Errorf("%w; failed to restore original Memory store: %v", primary, rollbackErr)
	}
	return primary
}
