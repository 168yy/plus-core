package registry

import (
	"github.com/168yy/plus-core/core/v2/task"
)

type MemoryServiceRegistry struct {
	registry[task.MemoryService]
}
