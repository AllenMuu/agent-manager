package cli

import (
	"os"
	"path/filepath"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func newInstallationService(libraryPath string, mode adapter.MaterializationMode) *installation.Service {
	service := installation.NewWithMaterialization(libraryPath, mode)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		service.BatchStore = operation.NewBatchStore(filepath.Join(home, ".agent-manager", "skill-sync", "batches"))
	}
	return service
}
