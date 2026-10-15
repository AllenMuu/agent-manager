package webconsole

import (
	"path/filepath"

	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func (s *Server) lifecycleService(journal *operation.Journal, confirm lifecycle.ConfirmFunc) *lifecycle.Service {
	service := lifecycle.New(s.libraryPath, journal, confirm)
	if s.homeDir != "" {
		service.BatchStore = operation.NewBatchStore(filepath.Join(s.homeDir, ".agent-manager", "skill-sync", "batches"))
	}
	return service
}
