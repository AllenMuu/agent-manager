package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LegacyImportRequest treats each nonblank legacy line as inert knowledge.
// Source is declared by the operator; no historical evidence is invented.
type LegacyImportRequest struct {
	Path        string        `json:"path"`
	Owner       Owner         `json:"owner"`
	Source      string        `json:"source"`
	Type        KnowledgeType `json:"type"`
	Layer       Layer         `json:"layer,omitempty"`
	OperationID string        `json:"operationId,omitempty"`
}

// ImportConfirmation is separate from the proposal and binds confirmation to
// the exact owner/source. It does not establish authenticated caller authority.
type ImportConfirmation struct {
	Confirmed bool
	Owner     Owner
	Source    string
}
type LegacyImporter interface {
	ImportLegacy(context.Context, LegacyImportRequest, ImportConfirmation) ([]Record, error)
}

func (s *StructuredStore) ImportLegacy(ctx context.Context, input LegacyImportRequest, confirmation ImportConfirmation) ([]Record, error) {
	if err := operationContext(ctx); err != nil {
		return nil, err
	}
	if !confirmation.Confirmed || confirmation.Owner != input.Owner || confirmation.Source != input.Source || strings.TrimSpace(input.Source) == "" {
		return nil, fmt.Errorf("%w: separately confirmed owner and source required", ErrInvalidInput)
	}
	sample := NewRecord{Owner: input.Owner, Type: input.Type, Content: "import", Source: input.Source, Layer: input.Layer}
	if err := sample.validate(); err != nil {
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
	data, err := readStoreFile(dir, filepath.Base(input.Path))
	if err != nil {
		return nil, err
	}
	intent := mutationIntent("import", struct {
		Request LegacyImportRequest `json:"request"`
		Content string              `json:"content"`
	}{input, string(data)})
	return s.transactionBatch(ctx, input.OperationID, intent, func(state *storeState) ([]Record, error) {
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
