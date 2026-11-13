package proxy

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mtanzi/spyglass/internal/config"
)

// Upstream timeouts. The response-header timeout is long because a
// non-streaming request sends no header until the model has finished.
const (
	dialTimeout           = 10 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 10 * time.Minute
)

// Transports holds one http.Transport per provider, so each provider keeps its
// own connection pool. It is safe for concurrent use.
type Transports struct {
	mu     sync.Mutex
	byName map[string]transportEntry
}

type transportEntry struct {
	baseURL string
	rt      *http.Transport
}

// NewTransports returns an empty set. A transport is made on first use.
func NewTransports() *Transports {
	return &Transports{byName: map[string]transportEntry{}}
}

// For returns the transport of provider p. When a reload changed the provider's
// base URL, For swaps in a new transport and closes the idle connections of the
// old one; a request already in flight finishes on the transport it started
// with. There is no retry and no fallback at this layer.
func (t *Transports) For(p config.Provider) http.RoundTripper {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.byName[p.Name]; ok {
		if e.baseURL == p.BaseURL {
			return e.rt
		}
		e.rt.CloseIdleConnections()
	}
	rt := newTransport()
	t.byName[p.Name] = transportEntry{baseURL: p.BaseURL, rt: rt}
	return rt
}

// newTransport is a plain http.Transport with the default pool and HTTP/2
// settings, plus a dial timeout and a time-to-headers timeout. It does not read
// proxy settings from the environment.
func newTransport() *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}
}
