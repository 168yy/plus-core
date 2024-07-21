package registry

import "github.com/168yy/plus-core/core/v2/cron"

type CrontabRegistry struct {
	registry[cron.ICron]
}
