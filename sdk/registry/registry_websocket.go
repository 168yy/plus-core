package registry

import (
	"github.com/168yy/plus-core/pkg/v2/ws"
)

type WebSocketRegistry struct {
	registry[*ws.Instance]
}
