package registry

import metrics "github.com/168yy/gf-metrics"

type MetricsRegistry struct {
	registry[*metrics.Monitor]
}
