package registry

import (
	cacheLib "github.com/168yy/plus-core/core/v2/cache"
)

type CacheRegistry struct {
	registry[cacheLib.ICache]
}
