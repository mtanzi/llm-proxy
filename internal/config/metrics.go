package config

import "github.com/prometheus/client_golang/prometheus"

// MetricConfigReloads is the name of the reload counter (ADR 0026).
const MetricConfigReloads = "spyglass_config_reloads_total"

var descReloads = prometheus.NewDesc(MetricConfigReloads,
	"Config reloads by result (success, failure). A failure keeps the previous snapshot.",
	[]string{"result"}, nil)

// Collector returns the Prometheus collector of the reload counts. Both
// results are always exported, at zero until they happen.
func (h *Holder) Collector() prometheus.Collector { return reloadCollector{h} }

type reloadCollector struct{ h *Holder }

func (reloadCollector) Describe(ch chan<- *prometheus.Desc) { ch <- descReloads }

func (c reloadCollector) Collect(ch chan<- prometheus.Metric) {
	for _, result := range []string{ResultSuccess, ResultFailure} {
		ch <- prometheus.MustNewConstMetric(descReloads, prometheus.CounterValue, float64(c.h.Reloads(result)), result)
	}
}
