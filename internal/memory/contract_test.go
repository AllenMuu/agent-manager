package memory_test

import (
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/memorytest"
)

func TestInMemoryProviderContract(t *testing.T) {
	memorytest.Run(t, func(t *testing.T) memory.StructuredProvider { return memory.NewInMemoryProvider() })
}
