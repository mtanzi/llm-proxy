package recorder

import "github.com/prometheus/client_golang/prometheus"

// Metric names of the collectors this package updates (ADR 0026).
const (
	MetricQueueDepth        = "llm_proxy_call_queue_depth"
	MetricCallsDropped      = "llm_proxy_calls_dropped_total"
	MetricWriterErrors      = "llm_proxy_writer_errors_total"
	MetricLiveEventsDropped = "llm_proxy_live_events_dropped_total"
)

var (
	descQueueDepth = prometheus.NewDesc(MetricQueueDepth,
		"Finished calls waiting for the batch writer.", nil, nil)
	descCallsDropped = prometheus.NewDesc(MetricCallsDropped,
		"Finished calls dropped before they were written, by reason (queue_full, shutdown). Should stay zero.",
		[]string{"reason"}, nil)
	descWriterErrors = prometheus.NewDesc(MetricWriterErrors,
		"Failed batch writes to the store.", nil, nil)
	descLiveDropped = prometheus.NewDesc(MetricLiveEventsDropped,
		"Live-tail events lost by subscribers that read too slowly.", nil, nil)
)

// Collector returns the Prometheus collector of the recorder. It reads the
// recorder's own counters when scraped, so the hot path pays nothing extra.
// Both drop reasons are always exported, at zero until they happen.
func (r *Recorder) Collector() prometheus.Collector { return collector{r} }

type collector struct{ r *Recorder }

func (collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descQueueDepth
	ch <- descCallsDropped
	ch <- descWriterErrors
	ch <- descLiveDropped
}

func (c collector) Collect(ch chan<- prometheus.Metric) {
	r := c.r
	ch <- prometheus.MustNewConstMetric(descQueueDepth, prometheus.GaugeValue, float64(r.QueueDepth()))
	for _, reason := range []string{ReasonQueueFull, ReasonShutdown} {
		ch <- prometheus.MustNewConstMetric(descCallsDropped, prometheus.CounterValue, float64(r.CallsDropped(reason)), reason)
	}
	ch <- prometheus.MustNewConstMetric(descWriterErrors, prometheus.CounterValue, float64(r.WriterErrors()))
	ch <- prometheus.MustNewConstMetric(descLiveDropped, prometheus.CounterValue, float64(r.LiveEventsDropped()))
}
