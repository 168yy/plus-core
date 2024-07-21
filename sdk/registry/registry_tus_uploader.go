package registry

import (
	"github.com/168yy/plus-core/pkg/v2/tus"
)

type TusRegistry struct {
	registry[*tus.Uploader]
}
