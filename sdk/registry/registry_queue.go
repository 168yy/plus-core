package registry

import queueLib "github.com/168yy/plus-core/core/v2/queue"

type QueueRegistry struct {
	registry[queueLib.IQueue]
}
