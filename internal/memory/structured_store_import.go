package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// LegacyImportRequest treats each nonblank legacy line as inert knowledge.
// Source is declared by the operator; no historical evidence is invented.
type LegacyImportRequest struct {
	Path                  string        `json:"path"`
	Owner                 Owner         `json:"owner"`
	Source                string        `json:"source"`
	Type                  KnowledgeType `json:"type"`
	Layer                 Layer         `json:"layer,omitempty"`
	OperationID           string        `json:"operationId,omitempty"`
	ExpectedContentSHA256 string        `json:"expectedContentSha256,omitempty"`
}

// ImportConfirmation is separate from the proposal and binds confirmation to
// the exact owner/source. It does not establish authenticated caller authority.
type ImportConfirmation struct {
	Confirmed bool
	Owner     Owner
	Source    string
}

// Implementations must reject a nonempty ExpectedContentSHA256 that does not
// match the exact inert bytes read, before any canonical mutation.
type LegacyImporter interface {
	ImportLegacy(context.Context, LegacyImportRequest, ImportConfirmation) ([]Record, error)
}

func (s *StructuredStore) ImportLegacy(ctx context.Context, input LegacyImportRequest, confirmation ImportConfirmation) ([]Record, error) {
	if err := operationContext(ctx); err != nil {
		return nil, err
	}
	if !utf8.ValidString(input.Path) {
		return nil, fmt.Errorf("%w: legacy path must be valid UTF-8", ErrInvalidInput)
	}
	if !confirmation.Confirmed || confirmation.Owner != input.Owner || confirmation.Source != input.Source || strings.TrimSpace(input.Source) == "" {
		return nil, fmt.Errorf("%w: separately confirmed owner and source required", ErrInvalidInput)
	}
	sample := NewRecord{Owner: input.Owner, Type: input.Type, Content: "import", Source: input.Source, Layer: input.Layer}
	if err := sample.validate(); err != nil {
		return nil, err
	}
	storeDir, err := s.directory()
	if err != nil {
		return nil, err
	}
	defer storeDir.Close()
	storeInfo, err := storeDir.Stat()
	if err != nil {
		return nil, err
	}
	// Resolve parent directories once, then read a direct regular file through an
	// anchored handle. A replaced directory cannot redirect this read.
	info, err := os.Lstat(input.Path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: direct regular legacy file required", ErrInvalidInput)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(input.Path))
	if err != nil {
		return nil, err
	}
	dir, err := openStoreDirectory(parent)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	sourceParentInfo, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	// Compare anchored identities, so an ancestor alias cannot turn canonical
	// storage into a legacy source that the import itself subsequently rewrites.
	if os.SameFile(sourceParentInfo, storeInfo) && filepath.Base(input.Path) == "memory.json" {
		return nil, fmt.Errorf("%w: legacy source is the canonical destination", ErrInvalidInput)
	}
	if err := rejectCanonicalImportSource(storeDir, dir, filepath.Base(input.Path)); err != nil {
		return nil, err
	}
	data, err := readStoreFile(dir, filepath.Base(input.Path))
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: legacy content must be valid UTF-8", ErrInvalidInput)
	}
	if input.ExpectedContentSHA256 != "" {
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != input.ExpectedContentSHA256 {
			return nil, ErrConflict
		}
	}
	intent := mutationIntent("import", struct {
		Request LegacyImportRequest `json:"request"`
		Content string              `json:"content"`
	}{input, string(data)})
	return s.transactionBatch(ctx, input.OperationID, intent, func(state *storeState) ([]Record, error) {
		// Recheck while the storage transaction holds its lock: a cooperating
		// writer cannot change the canonical inode between these identity checks.
		if err := rejectCanonicalImportSource(storeDir, dir, filepath.Base(input.Path)); err != nil {
			return nil, err
		}
		records := []Record{}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			recordInput := sample
			recordInput.Content = line
			record, err := canonical(recordInput)
			if err != nil {
				return nil, err
			}
			saveRecord(state, record)
			records = append(records, record)
		}
		return records, nil
	})
}

// ImportLegacy dispatches the optional confirmed local importer.
func ImportLegacy(ctx context.Context, p StructuredProvider, input LegacyImportRequest, confirmation ImportConfirmation) ([]Record, error) {
	if p == nil {
		return nil, ErrUnavailable
	}
	importer, ok := p.(LegacyImporter)
	if err := providerReady(ctx, p, ok && p.Capabilities().ImportLegacy); err != nil {
		return nil, err
	}
	return importer.ImportLegacy(ctx, input, confirmation)
}

// Conservatively reject any direct file sharing the current canonical inode,
// including case-insensitive filename aliases and hardlinks outside the root.
func rejectCanonicalImportSource(storeDir, sourceDir *os.File, name string) error {
	sourceInfo, err := inspectStoreFile(sourceDir, name)
	if err != nil {
		return err
	}
	canonicalInfo, err := inspectStoreFile(storeDir, "memory.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if os.SameFile(sourceInfo, canonicalInfo) {
		return fmt.Errorf("%w: legacy source aliases canonical storage", ErrInvalidInput)
	}
	return nil
}
