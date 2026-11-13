package postgres

import "github.com/prometheus/client_golang/prometheus"

// Metric names of the retention job (ADR 0026).
const (
	MetricRetentionDeleted     = "spyglass_retention_deleted_total"
	MetricRetentionLastRunTime = "spyglass_retention_last_run_timestamp_seconds"
)

var (
	descRetentionDeleted = prometheus.NewDesc(MetricRetentionDeleted,
		"Rows the retention job deleted from calls and tool_events since start.", nil, nil)
	descRetentionLastRun = prometheus.NewDesc(MetricRetentionLastRunTime,
		"Unix time the last retention run finished without error; 0 before the first run.", nil, nil)
)

// RetentionCollector returns the Prometheus collector of the retention job.
// It reads RetentionStats when scraped.
func (s *Store) RetentionCollector() prometheus.Collector { return retentionCollector{s} }

type retentionCollector struct{ s *Store }

func (retentionCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descRetentionDeleted
	ch <- descRetentionLastRun
}

func (c retentionCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.s.RetentionStats()
	var last float64
	if !st.LastRun.IsZero() {
		last = float64(st.LastRun.UnixNano()) / 1e9
	}
	ch <- prometheus.MustNewConstMetric(descRetentionDeleted, prometheus.CounterValue, float64(st.Deleted))
	ch <- prometheus.MustNewConstMetric(descRetentionLastRun, prometheus.GaugeValue, last)
}
