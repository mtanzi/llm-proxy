//go:build !noui

package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEmbeddedUI serves the real build: make ui-build must run first.
func TestEmbeddedUI(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/sessions/x", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
}
