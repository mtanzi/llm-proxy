package config

import (
	"slices"
	"strings"
	"testing"
)

// legacyOpts is fakeOpts that also records each deprecated name it reads.
func legacyOpts(env, files map[string]string) (Options, *[]string) {
	var seen []string
	o := fakeOpts(env, files)
	o.Deprecated = func(legacy, name string) { seen = append(seen, legacy+"->"+name) }
	return o, &seen
}

func TestLegacyEnvName(t *testing.T) {
	for in, want := range map[string]string{
		"SPYGLASS_CONFIG":            "LLM_PROXY_CONFIG",
		"SPYGLASS_DATABASE_URL":      "LLM_PROXY_DATABASE_URL",
		"SPYGLASS_LISTEN_DATA":       "LLM_PROXY_LISTEN_DATA",
		"SPYGLASS_TEST_DATABASE_URL": "LLM_PROXY_TEST_DATABASE_URL",
	} {
		if got, ok := LegacyEnvName(in); !ok || got != want {
			t.Errorf("LegacyEnvName(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"DB", "SPYGLASS", "SPYGLASS_", "LLM_PROXY_CONFIG", "XSPYGLASS_CONFIG"} {
		if got, ok := LegacyEnvName(in); ok {
			t.Errorf("LegacyEnvName(%q) = %q, want no legacy name", in, got)
		}
	}
}

// A LLM_PROXY_ override of 0.1.0 still applies when the SPYGLASS_ name is
// unset, and each one is reported as deprecated (ADR 0054).
func TestLegacyEnvOverrides(t *testing.T) {
	env := map[string]string{
		"DB": "postgres://x", "KEY": "k",
		"LLM_PROXY_LISTEN_DATA":          ":9999",
		"LLM_PROXY_SERVER_DRAIN_TIMEOUT": "45s",
		"LLM_PROXY_LOG_LEVEL":            "warn",
		"LLM_PROXY_DATABASE_URL_FILE":    "/run/secrets/db",
	}
	o, seen := legacyOpts(env, map[string]string{"/run/secrets/db": "postgres://f\n"})
	cfg, err := Parse([]byte(base), o)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Data != ":9999" || cfg.Server.DrainTimeout.Std().String() != "45s" || cfg.Log.Level != "warn" ||
		cfg.Database.URL.File != "/run/secrets/db" || cfg.Database.URL.Secret().Reveal() != "postgres://f" {
		t.Errorf("legacy overrides not applied: %+v", cfg)
	}
	want := []string{
		"LLM_PROXY_LISTEN_DATA->SPYGLASS_LISTEN_DATA",
		"LLM_PROXY_DATABASE_URL_FILE->SPYGLASS_DATABASE_URL_FILE",
		"LLM_PROXY_SERVER_DRAIN_TIMEOUT->SPYGLASS_SERVER_DRAIN_TIMEOUT",
		"LLM_PROXY_LOG_LEVEL->SPYGLASS_LOG_LEVEL",
	}
	got := slices.Clone(*seen)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("deprecated = %q, want %q", got, want)
	}
}

// When both names are set the SPYGLASS_ one wins and nothing is deprecated.
func TestNewEnvWinsOverLegacy(t *testing.T) {
	env := map[string]string{
		"DB": "postgres://x", "KEY": "k", "OTHER": "other",
		"SPYGLASS_LOG_LEVEL":          "warn",
		"LLM_PROXY_LOG_LEVEL":         "debug",
		"SPYGLASS_DATABASE_URL_ENV":   "OTHER",
		"LLM_PROXY_DATABASE_URL_FILE": "/run/secrets/db",
	}
	o, seen := legacyOpts(env, nil)
	cfg, err := Parse([]byte(base), o)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log.level = %q, want the SPYGLASS_ value", cfg.Log.Level)
	}
	if r := cfg.Database.URL; r.Env != "OTHER" || r.File != "" || r.Secret().Reveal() != "other" {
		t.Errorf("database.url = %s: the LLM_PROXY_ part was mixed in", r)
	}
	if len(*seen) != 0 {
		t.Errorf("deprecated = %q, want none", *seen)
	}
}

func TestLegacyEnvErrorNamesLegacyVariable(t *testing.T) {
	env := map[string]string{"DB": "x", "KEY": "k", "LLM_PROXY_QUEUE_CAPACITY": "lots"}
	_, err := Parse([]byte(base), fakeOpts(env, nil))
	if err == nil || !strings.Contains(err.Error(), "LLM_PROXY_QUEUE_CAPACITY") {
		t.Errorf("err = %v, want it to name LLM_PROXY_QUEUE_CAPACITY", err)
	}
}

// A secret reference to a SPYGLASS_ variable falls back to its LLM_PROXY_
// name, so a config copied from the new example runs with the old variable.
func TestSecretRefFallsBackToLegacyName(t *testing.T) {
	text := strings.Replace(base, "{ env: DB }", "{ env: SPYGLASS_DATABASE_URL }", 1)
	o, seen := legacyOpts(map[string]string{"KEY": "k", "LLM_PROXY_DATABASE_URL": "postgres://old"}, nil)
	cfg, err := Parse([]byte(text), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Database.URL.Secret().Reveal(); got != "postgres://old" {
		t.Errorf("database.url = %q", got)
	}
	if !slices.Equal(*seen, []string{"LLM_PROXY_DATABASE_URL->SPYGLASS_DATABASE_URL"}) {
		t.Errorf("deprecated = %q", *seen)
	}

	o, seen = legacyOpts(map[string]string{"KEY": "k", "SPYGLASS_DATABASE_URL": "postgres://new", "LLM_PROXY_DATABASE_URL": "postgres://old"}, nil)
	if cfg, err = Parse([]byte(text), o); err != nil || cfg.Database.URL.Secret().Reveal() != "postgres://new" || len(*seen) != 0 {
		t.Errorf("new name must win: err %v, deprecated %q", err, *seen)
	}

	// Only SPYGLASS_ names fall back; any other name is read as written.
	if _, err := Parse([]byte(base), fakeOpts(map[string]string{"KEY": "k", "LLM_PROXY_DB": "x"}, nil)); err == nil {
		t.Error("a non-SPYGLASS_ reference fell back")
	}
}

func TestPathLegacy(t *testing.T) {
	o, seen := legacyOpts(map[string]string{"LLM_PROXY_CONFIG": "/old.yaml"}, nil)
	if p, err := Path("", o); err != nil || p != "/old.yaml" {
		t.Errorf("legacy fallback: %q, %v", p, err)
	}
	if !slices.Equal(*seen, []string{"LLM_PROXY_CONFIG->SPYGLASS_CONFIG"}) {
		t.Errorf("deprecated = %q", *seen)
	}
	o, seen = legacyOpts(map[string]string{"LLM_PROXY_CONFIG": "/old.yaml", "SPYGLASS_CONFIG": "/new.yaml"}, nil)
	if p, err := Path("", o); err != nil || p != "/new.yaml" || len(*seen) != 0 {
		t.Errorf("new name must win: %q, %v, deprecated %q", p, err, *seen)
	}
	if !strings.Contains(func() string { _, err := Path("", fakeOpts(nil, nil)); return err.Error() }(), "SPYGLASS_CONFIG") {
		t.Error("the no-path error does not name SPYGLASS_CONFIG")
	}
}
