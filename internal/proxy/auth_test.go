package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/mtanzi/llm-proxy/internal/core"
)

type fakeKeys map[string]core.Key

func (f fakeKeys) Lookup(h string) (core.Key, bool) { k, ok := f[h]; return k, ok }

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestAuthenticate(t *testing.T) {
	keys := fakeKeys{
		hashOf("lp_good"): {Name: "good"},
		hashOf("lp_off"):  {Name: "off", Disabled: true},
	}
	hdr := func(kv ...string) http.Header {
		h := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return h
	}
	cases := []struct {
		name     string
		h        http.Header
		wantName string
		wantMsg  string // empty means success
	}{
		{"bearer", hdr("Authorization", "Bearer lp_good"), "good", ""},
		{"bearer lowercase scheme", hdr("Authorization", "bearer lp_good"), "good", ""},
		{"x-api-key", hdr("X-Api-Key", "lp_good"), "good", ""},
		{"bearer wins over x-api-key", hdr("Authorization", "Bearer lp_good", "X-Api-Key", "lp_nope"), "good", ""},
		{"non-bearer authorization falls back to x-api-key", hdr("Authorization", "Basic abc", "X-Api-Key", "lp_good"), "good", ""},
		{"unknown", hdr("Authorization", "Bearer lp_nope"), "", "invalid x-api-key"},
		{"disabled", hdr("X-Api-Key", "lp_off"), "", "this API key is disabled"},
		{"missing", hdr(), "", "invalid x-api-key"},
		{"empty bearer", hdr("Authorization", "Bearer "), "", "invalid x-api-key"},
		{"non-bearer only", hdr("Authorization", "Basic abc"), "", "invalid x-api-key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, resp, ok := Authenticate(c.h, keys, "req_1")
			if c.wantMsg == "" {
				if !ok || name != c.wantName {
					t.Fatalf("got %q ok=%v; want %q", name, ok, c.wantName)
				}
				return
			}
			if ok || name != "" {
				t.Fatalf("got %q ok=%v; want failure", name, ok)
			}
			if resp.Status != http.StatusUnauthorized {
				t.Errorf("status = %d", resp.Status)
			}
			if !strings.Contains(string(resp.Body), c.wantMsg) {
				t.Errorf("body %s lacks %q", resp.Body, c.wantMsg)
			}
			if resp.Header.Get("X-Request-Id") != "req_1" {
				t.Errorf("request id header = %q", resp.Header.Get("X-Request-Id"))
			}
			if strings.Contains(string(resp.Body), "lp_") {
				t.Errorf("body echoes a key: %s", resp.Body)
			}
		})
	}
}
