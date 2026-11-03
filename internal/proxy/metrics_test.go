package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/httpmw"
)

func gather(t *testing.T, m *Metrics) map[string]*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(m)
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*dto.MetricFamily{}
	for _, f := range fams {
		out[f.GetName()] = f
	}
	return out
}

func labelsOf(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range m.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

func TestMetricsRecordForwardedCalls(t *testing.T) {
	m := NewMetrics()
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.Contains(r.URL.RawQuery, "fail") {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}), nil, func(e *env) { e.metrics = m })

	_ = readAll(t, e.do(t, "POST", "/v1/messages", "{}", auth()...).Body)
	e.rec.next(t)
	_ = readAll(t, e.do(t, "POST", "/v1/messages?fail=1", "{}", auth()...).Body)
	e.rec.next(t)

	fams := gather(t, m)
	got := map[string]float64{}
	for _, s := range fams[MetricRequests].GetMetric() {
		l := labelsOf(s)
		got[l["family"]+"/"+l["provider"]+"/"+l["status"]+"/"+l["error_class"]] = s.GetCounter().GetValue()
	}
	if len(got) != 2 || got["anthropic/anthropic/200/"] != 1 || got["anthropic/anthropic/429/rate_limited"] != 1 {
		t.Errorf("requests_total = %v", got)
	}
	if n := fams[MetricRequestDuration].GetMetric()[0].GetHistogram().GetSampleCount(); n != 2 {
		t.Errorf("duration samples = %d, want 2", n)
	}
	ov := fams[MetricOverhead].GetMetric()[0].GetHistogram()
	if ov.GetSampleCount() != 2 {
		t.Errorf("overhead samples = %d, want 2", ov.GetSampleCount())
	}
	if avg := ov.GetSampleSum() / float64(ov.GetSampleCount()); avg > 0.5 {
		t.Errorf("mean overhead = %gs on loopback; the measurement includes something it should not", avg)
	}
	if v := fams[MetricInflightStreams].GetMetric()[0].GetGauge().GetValue(); v != 0 {
		t.Errorf("inflight_streams = %g after both calls, want 0", v)
	}
}

func TestMetricsInflightStreamsCountsWhileStreaming(t *testing.T) {
	m := NewMetrics()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-release
	}), nil, func(e *env) { e.metrics = m })

	resp := e.do(t, "POST", "/v1/messages", `{"stream":true}`, auth()...)
	<-started
	if v := gather(t, m)[MetricInflightStreams].GetMetric()[0].GetGauge().GetValue(); v != 1 {
		t.Errorf("inflight_streams = %g while a stream is open, want 1", v)
	}
	close(release)
	_ = readAll(t, resp.Body)
	e.rec.next(t)
	if v := gather(t, m)[MetricInflightStreams].GetMetric()[0].GetGauge().GetValue(); v != 0 {
		t.Errorf("inflight_streams = %g after the stream ended, want 0", v)
	}
}

func TestMetricsOverheadExcludesBodyUpload(t *testing.T) {
	m := NewMetrics()
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}), nil, func(e *env) { e.metrics = m })

	pr, pw := io.Pipe()
	req, _ := http.NewRequest("POST", e.proxy.URL+"/v1/messages", pr)
	req.Header.Set("Authorization", "Bearer "+clientKey)
	go func() {
		_, _ = pw.Write([]byte("{"))
		// A slow client: the proxy waits for the rest of the body.
		time.Sleep(300 * time.Millisecond)
		_, _ = pw.Write([]byte("}"))
		_ = pw.Close()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = readAll(t, resp.Body)
	e.rec.next(t)
	ov := gather(t, m)[MetricOverhead].GetMetric()[0].GetHistogram()
	if ov.GetSampleCount() != 1 || ov.GetSampleSum() > 0.2 {
		t.Errorf("overhead = %gs over %d samples; a 300ms client upload must not count", ov.GetSampleSum(), ov.GetSampleCount())
	}
}

func TestNilMetricsIsSafe(t *testing.T) {
	var m *Metrics
	m.finished("a", "b", 200, "", 0, 0)
	m.observeOverhead(0)
	m.streamStarted()
	m.streamEnded()
}

type noSnap struct{}

func (noSnap) Current() *config.Snapshot { return nil }

func TestDataPlaneHealthAndReadiness(t *testing.T) {
	e := newEnv(t, http.NotFoundHandler(), nil)
	probe := func(snaps Snapshots, drain *httpmw.Drain, path string) int {
		rr := httptest.NewRecorder()
		NewMux(NewHandler(snaps, Options{}), drain).ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr.Code
	}
	drain := &httpmw.Drain{}
	if c := probe(e.holder, drain, "/readyz"); c != 200 {
		t.Errorf("readyz with config = %d, want 200", c)
	}
	if c := probe(noSnap{}, drain, "/readyz"); c != 503 {
		t.Errorf("readyz without config = %d, want 503", c)
	}
	if c := probe(noSnap{}, drain, "/healthz"); c != 200 {
		t.Errorf("healthz without config = %d, want 200: alive is not ready", c)
	}
	drain.Start()
	if c := probe(e.holder, drain, "/readyz"); c != 503 {
		t.Errorf("readyz while draining = %d, want 503", c)
	}
	if c := probe(e.holder, drain, "/healthz"); c != 200 {
		t.Errorf("healthz while draining = %d, want 200", c)
	}
}
