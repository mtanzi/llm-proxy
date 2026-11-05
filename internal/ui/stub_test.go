//go:build noui

package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNoUIStub proves the noui build compiles and serves its stub page for
// the root and for a deep link.
func TestNoUIStub(t *testing.T) {
	for _, target := range []string{"/ui/", "/ui/calls/42"} {
		mux := http.NewServeMux()
		Mount(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "noui") {
			t.Errorf("GET %s: status %d, body %q", target, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s: no CSP", target)
		}
	}
}
