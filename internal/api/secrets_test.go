package api

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/mtanzi/spyglass/internal/config"
	"github.com/mtanzi/spyglass/internal/core"
	"github.com/mtanzi/spyglass/internal/core/coretest"
)

// TestNoSecretInAnyResponse loads a real config whose credential, database URL
// and key hashes are known, serves every admin route, and checks that none of
// those values, nor a path to a secret file, appears in a response.
func TestNoSecretInAnyResponse(t *testing.T) {
	const (
		credential = "sk-ant-api03-SUPERSECRET"
		dbURL      = "postgres://llm:hunter2@db.internal:5432/proxy"
	)
	yamlDoc, err := os.ReadFile("../config/testdata/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(yamlDoc, config.Options{
		LookupEnv: func(k string) (string, bool) { return dbURL, k == "TEST_DATABASE_URL" },
		ReadFile: func(p string) ([]byte, error) {
			if p == "/run/secrets/anthropic_api_key" {
				return []byte(credential + "\n"), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		credential, "hunter2", "db.internal", dbURL,
		"/run/secrets", "anthropic_api_key", "TEST_DATABASE_URL", "tls.crt", "tls.key",
		"credential", "sha256",
	}
	for _, k := range cfg.Keys {
		forbidden = append(forbidden, k.SHA256)
	}

	id := core.NewID()
	rd := &coretest.Reader{
		Calls:   []core.CallSummary{{ID: id, KeyName: "alice"}},
		Details: map[core.ID]core.CallDetail{id: {CallRecord: core.CallRecord{ID: id, KeyName: "alice"}}},
	}
	h := newTestMux(t, rd, cfg.View())
	for _, target := range []string{
		"/admin/config", "/admin/calls", "/admin/calls/" + id.String(), "/admin/openapi.json",
		"/admin/calls?key=alice", "/admin/nope", "/admin/calls?bad=1",
	} {
		body := get(h, target).Body.String()
		for _, f := range forbidden {
			// The OpenAPI document and its descriptions name these words; only the
			// data endpoints must be free of them.
			if target == "/admin/openapi.json" && (f == "credential" || f == "sha256") {
				continue
			}
			if strings.Contains(body, f) {
				t.Errorf("%s contains %q", target, f)
			}
		}
	}
}
