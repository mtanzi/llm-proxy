package fakeupstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// pingEvent is the SSE keep-alive Anthropic sends between events.
const pingEvent = "event: ping\ndata: {\"type\":\"ping\"}\n\n"

// Reply is one scripted response. The zero value of every field after Fixture
// means "as the fixture says".
type Reply struct {
	Fixture Fixture

	// Status overrides the fixture's status.
	Status int
	// Headers are added to the fixture's response headers, replacing equal names.
	Headers map[string]string

	// Split, when positive, sends the body in chunks of Split bytes, which cuts
	// SSE events and even UTF-8 sequences in the middle.
	Split int
	// Delay is the pause before every chunk, on top of the fixture's own plan.
	Delay time.Duration
	// Pings inserts a ping event after every chunk of a streamed body. It
	// needs event-sized chunks, so it ignores Split.
	Pings bool
	// AbortAfter, when positive, cuts the connection after that many chunks
	// have been sent: the client sees a body that ends early.
	AbortAfter int
}

// Request is what the upstream received.
type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// Server is the fake upstream. Script it with Enqueue, then point the
// provider's base_url at URL.
type Server struct {
	*httptest.Server
	t testing.TB

	mu       sync.Mutex
	replies  []Reply
	requests []Request
}

// New starts the server. It stops with the test.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{t: t}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Enqueue adds replies. The server answers requests in this order, one reply
// each; a request with no reply left is answered 500 and fails the test.
func (s *Server) Enqueue(r ...Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies = append(s.replies, r...)
}

// Requests returns the requests received so far, oldest first.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	var rp Reply
	ok := len(s.replies) > 0
	if ok {
		rp, s.replies = s.replies[0], s.replies[1:]
	}
	s.mu.Unlock()
	if !ok {
		s.t.Errorf("fakeupstream: unexpected request %s %s with no reply queued", r.Method, r.URL.Path)
		http.Error(w, "no reply queued", http.StatusInternalServerError)
		return
	}
	rp.write(w, r)
}

func (rp Reply) write(w http.ResponseWriter, r *http.Request) {
	f := rp.Fixture
	h := w.Header()
	if f.Meta.Streamed {
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
	} else {
		h.Set("Content-Type", "application/json")
	}
	for k, v := range f.Replay.Headers {
		h.Set(k, v)
	}
	for k, v := range rp.Headers {
		h.Set(k, v)
	}
	status := f.Meta.Status
	if rp.Status != 0 {
		status = rp.Status
	}
	if status == 0 {
		status = http.StatusOK
	}

	chunks, delays := f.chunks(), f.delays(len(f.chunks()))
	switch {
	case rp.Split > 0:
		chunks, delays = splitEvery(f.Response, rp.Split), nil
	case rp.Pings && f.Meta.Streamed:
		chunks = withPings(chunks)
		delays = nil
	}

	w.WriteHeader(status)
	flush := http.NewResponseController(w).Flush
	_ = flush()
	for i, c := range chunks {
		if rp.AbortAfter > 0 && i >= rp.AbortAfter {
			panic(http.ErrAbortHandler)
		}
		d := rp.Delay
		if i < len(delays) {
			d += delays[i]
		}
		if d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		if _, err := w.Write(c); err != nil {
			return
		}
		_ = flush()
	}
	if rp.AbortAfter >= len(chunks) && rp.AbortAfter > 0 {
		panic(http.ErrAbortHandler)
	}
}

func splitEvery(b []byte, n int) [][]byte {
	var out [][]byte
	for len(b) > n {
		out = append(out, b[:n])
		b = b[n:]
	}
	if len(b) > 0 {
		out = append(out, b)
	}
	return out
}

// withPings puts a ping event after every chunk but the last, so the stream
// still ends with the event the fixture ends with.
func withPings(chunks [][]byte) [][]byte {
	var out [][]byte
	for i, c := range chunks {
		out = append(out, c)
		if i < len(chunks)-1 {
			out = append(out, []byte(pingEvent))
		}
	}
	return out
}
