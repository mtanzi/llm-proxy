package recorder

import "github.com/prometheus/client_golang/prometheus"

// Metric names of the collectors this package updates (ADR 0026).
const (
	MetricQueueDepth        = "spyglass_call_queue_depth"
	MetricCallsDropped      = "spyglass_calls_dropped_total"
	MetricWriterErrors      = "spyglass_writer_errors_total"
	MetricLiveEventsDropped = "spyglass_live_events_dropped_total"
	MetricSystematicFailure = "spyglass_writer_systematic_failure"
)

var (
	descQueueDepth = prometheus.NewDesc(MetricQueueDepth,
		"Finished calls waiting for the batch writer.", nil, nil)
	descCallsDropped = prometheus.NewDesc(MetricCallsDropped,
		"Finished calls dropped before they were written, by reason (queue_full, shutdown, unwritable). Should stay zero.",
		[]string{"reason"}, nil)
	descWriterErrors = prometheus.NewDesc(MetricWriterErrors,
		"Failed batch writes to the store.", nil, nil)
	descLiveDropped = prometheus.NewDesc(MetricLiveEventsDropped,
		"Live-tail events lost by subscribers that read too slowly.", nil, nil)
	descSystematicFailure = prometheus.NewDesc(MetricSystematicFailure,
		"1 while the writer holds a batch in which every record failed as unwritable, else 0. Should stay zero.", nil, nil)
)

// Collector returns the Prometheus collector of the recorder. It reads the
// recorder's own counters when scraped, so the hot path pays nothing extra.
// Every drop reason is always exported, at zero until it happens.
func (r *Recorder) Collector() prometheus.Collector { return collector{r} }

type collector struct{ r *Recorder }

func (collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descQueueDepth
	ch <- descCallsDropped
	ch <- descWriterErrors
	ch <- descLiveDropped
	ch <- descSystematicFailure
}

func (c collector) Collect(ch chan<- prometheus.Metric) {
	r := c.r
	ch <- prometheus.MustNewConstMetric(descQueueDepth, prometheus.GaugeValue, float64(r.QueueDepth()))
	for _, reason := range []string{ReasonQueueFull, ReasonShutdown, ReasonUnwritable} {
		ch <- prometheus.MustNewConstMetric(descCallsDropped, prometheus.CounterValue, float64(r.CallsDropped(reason)), reason)
	}
	ch <- prometheus.MustNewConstMetric(descWriterErrors, prometheus.CounterValue, float64(r.WriterErrors()))
	ch <- prometheus.MustNewConstMetric(descLiveDropped, prometheus.CounterValue, float64(r.LiveEventsDropped()))
	systematic := 0.0
	if r.SystematicFailure() {
		systematic = 1
	}
	ch <- prometheus.MustNewConstMetric(descSystematicFailure, prometheus.GaugeValue, systematic)
}
