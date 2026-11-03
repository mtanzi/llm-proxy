package proxy

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metric names of the collectors this package updates (ADR 0026).
const (
	MetricRequests        = "llm_proxy_requests_total"
	MetricRequestDuration = "llm_proxy_request_duration_seconds"
	MetricTTFT            = "llm_proxy_ttft_seconds"
	MetricOverhead        = "llm_proxy_proxy_overhead_seconds"
	MetricInflightStreams = "llm_proxy_inflight_streams"
)

// Metrics are the proxy's Prometheus collectors. Create it with NewMetrics and
// register it where the metrics are served. A nil *Metrics records nothing.
//
// Label values are bounded: family and provider come from the config and the
// code, status is an HTTP status code and error_class is the closed set of
// core.ErrorClass. No key name, session, model or call id is a label.
type Metrics struct {
	requests *prometheus.CounterVec
	duration prometheus.Histogram
	ttft     prometheus.Histogram
	overhead prometheus.Histogram
	inflight prometheus.Gauge
}

// NewMetrics returns the collectors of the proxy.
func NewMetrics() *Metrics {
	return &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricRequests,
			Help: "Forwarded calls that finished, by family, provider, HTTP status and error class.",
		}, []string{"family", "provider", "status", "error_class"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    MetricRequestDuration,
			Help:    "Seconds from the start of the upstream exchange to the end of the response.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600},
		}),
		ttft: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    MetricTTFT,
			Help:    "Seconds from the start of the upstream exchange to the first content of the response.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
		}),
		overhead: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    MetricOverhead,
			Help:    "Seconds the proxy itself takes between request arrival and the upstream request being sent, excluding the client body upload and the upstream connection wait.",
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
		}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: MetricInflightStreams,
			Help: "Streaming requests being forwarded now.",
		}),
	}
}

// Describe implements prometheus.Collector.
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	m.requests.Describe(ch)
	m.duration.Describe(ch)
	m.ttft.Describe(ch)
	m.overhead.Describe(ch)
	m.inflight.Describe(ch)
}

// Collect implements prometheus.Collector.
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	m.requests.Collect(ch)
	m.duration.Collect(ch)
	m.ttft.Collect(ch)
	m.overhead.Collect(ch)
	m.inflight.Collect(ch)
}

func (m *Metrics) finished(family, provider string, status int, errorClass string, duration, ttft time.Duration) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(family, provider, strconv.Itoa(status), errorClass).Inc()
	m.duration.Observe(duration.Seconds())
	if ttft > 0 {
		m.ttft.Observe(ttft.Seconds())
	}
}

func (m *Metrics) observeOverhead(d time.Duration) {
	if m != nil {
		m.overhead.Observe(max(d, 0).Seconds())
	}
}

func (m *Metrics) streamStarted() {
	if m != nil {
		m.inflight.Inc()
	}
}

func (m *Metrics) streamEnded() {
	if m != nil {
		m.inflight.Dec()
	}
}
