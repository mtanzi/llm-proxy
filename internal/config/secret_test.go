package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const (
	dbSecret  = "postgres://user:hunter2-db-password@db/llm"
	apiSecret = "sk-ant-api03-very-secret-value"
)

func secretConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := loadFile(t, "testdata/valid.yaml", fakeOpts(
		map[string]string{"TEST_DATABASE_URL": dbSecret},
		map[string]string{"/run/secrets/anthropic_api_key": apiSecret + "\n"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL.Secret().Reveal() != dbSecret || cfg.Providers[0].Credential.Ref.Secret().Reveal() != apiSecret {
		t.Fatal("secrets were not resolved")
	}
	return cfg
}

func assertNoSecret(t *testing.T, what, out string) {
	t.Helper()
	for _, s := range []string{dbSecret, apiSecret, "hunter2", "very-secret"} {
		if strings.Contains(out, s) {
			t.Errorf("%s leaks %q: %s", what, s, out)
		}
	}
}

func TestResolvedSecretsAreRedacted(t *testing.T) {
	cfg := secretConfig(t)

	values := map[string]any{
		"config":         cfg,
		"config value":   *cfg,
		"database":       cfg.Database,
		"database url":   cfg.Database.URL,
		"provider":       cfg.Providers[0],
		"providers":      cfg.Providers,
		"credential":     cfg.Providers[0].Credential,
		"credential ref": cfg.Providers[0].Credential.Ref,
		"secret":         cfg.Database.URL.Secret(),
		"secret ptr":     func() *Secret { s := cfg.Database.URL.Secret(); return &s }(),
	}
	for name, v := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
			assertNoSecret(t, fmt.Sprintf("%s %s", name, verb), fmt.Sprintf(verb, v))
		}
		assertNoSecret(t, name+" Sprint", fmt.Sprint(v))
		assertNoSecret(t, name+" Sprintln", fmt.Sprintln(v))
	}

	if got := fmt.Sprint(cfg.Database.URL.Secret()); got != redacted {
		t.Errorf("Secret prints %q; want %q", got, redacted)
	}
}

func TestResolvedSecretsStayOutOfSlog(t *testing.T) {
	cfg := secretConfig(t)
	for name, mk := range map[string]func(*bytes.Buffer) *slog.Logger{
		"json": func(b *bytes.Buffer) *slog.Logger { return slog.New(slog.NewJSONHandler(b, nil)) },
		"text": func(b *bytes.Buffer) *slog.Logger { return slog.New(slog.NewTextHandler(b, nil)) },
	} {
		var buf bytes.Buffer
		log := mk(&buf)
		log.Info("loaded", "config", cfg, "database", cfg.Database, "url", cfg.Database.URL, "secret", cfg.Database.URL.Secret(),
			"credential", cfg.Providers[0].Credential, "providers", cfg.Providers)
		log.Info("grouped", slog.Group("db", slog.Any("url", cfg.Database.URL.Secret())))
		if buf.Len() == 0 {
			t.Fatalf("%s: nothing logged", name)
		}
		assertNoSecret(t, "slog "+name, buf.String())
	}
}

func TestResolvedSecretsStayOutOfSerialisation(t *testing.T) {
	cfg := secretConfig(t)
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "json config", string(b))
	b, err = json.Marshal(cfg.Database.URL.Secret())
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "json secret", string(b))
}

func TestViewHoldsNoSecret(t *testing.T) {
	cfg := secretConfig(t)
	b, err := json.Marshal(cfg.View())
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "view json", string(b))
	assertNoSecret(t, "view %+v", fmt.Sprintf("%+v", cfg.View()))
	for _, banned := range []string{hashA, hashB, "/run/secrets", "TEST_DATABASE_URL"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("view leaks %q: %s", banned, b)
		}
	}
}

func TestLoadErrorsNameTheSourceNotTheValue(t *testing.T) {
	text := strings.Replace(base, "{ env: DB }", "{ env: DB, file: /f }", 1)
	_, err := Parse([]byte(text), fakeOpts(map[string]string{"DB": dbSecret, "KEY": apiSecret}, map[string]string{"/f": dbSecret}))
	if err == nil {
		t.Fatal("accepted env and file together")
	}
	assertNoSecret(t, "load error", err.Error())

	// An inlined secret is refused without being echoed.
	_, err = Parse([]byte(strings.Replace(base, "{ env: KEY }", apiSecret, 1)), baseOpts())
	if err == nil {
		t.Fatal("accepted an inlined secret")
	}
	assertNoSecret(t, "inline error", err.Error())
}
