package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The example configs and the compose file in the repository root are what the
// deployment guide tells people to copy (ADR 0037). TestExampleFile in
// config_test.go loads config.example.yaml; these tests cover the personal
// example and the compose file, so a change that breaks them fails CI.

const repoRoot = "../.."

func TestPersonalExampleLoads(t *testing.T) {
	// The personal setup needs no provider credential file.
	o := fakeOpts(map[string]string{"LLM_PROXY_DATABASE_URL": "postgres://u:p@127.0.0.1:5432/llmproxy"}, nil)
	cfg, err := loadFile(t, filepath.Join(repoRoot, "config.personal.example.yaml"), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].AuthMode; got != AuthClientPassthrough {
		t.Errorf("auth_mode = %q, want %q", got, AuthClientPassthrough)
	}
	for _, addr := range []string{cfg.Listen.Data, cfg.Listen.Admin} {
		if !IsLoopback(addr) {
			t.Errorf("listen address %q is not loopback", addr)
		}
	}
}

// The compose file must give the container the config path, the secret files
// the example config reads, and a grace period that matches the drain timeout.
func TestComposeMatchesExampleConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			Secrets     []string          `yaml:"secrets"`
			Ports       []string          `yaml:"ports"`
			GracePeriod string            `yaml:"stop_grace_period"`
		} `yaml:"services"`
		Secrets map[string]any `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	svc, ok := compose.Services["llm-proxy"]
	if !ok {
		t.Fatal("docker-compose.yml has no llm-proxy service")
	}

	cfgPath := svc.Environment["LLM_PROXY_CONFIG"]
	mounted := false
	for _, v := range svc.Volumes {
		mounted = mounted || strings.HasSuffix(v, ":"+cfgPath+":ro")
	}
	if cfgPath == "" || !mounted {
		t.Errorf("config path %q is not mounted read-only: volumes %v", cfgPath, svc.Volumes)
	}

	cfg, err := loadFile(t, filepath.Join(repoRoot, "config.example.yaml"), fakeOpts(nil, map[string]string{
		"/run/secrets/database_url":      "postgres://u:p@postgres:5432/llmproxy",
		"/run/secrets/anthropic_api_key": "sk-ant-placeholder",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{cfg.Database.URL.File, cfg.Providers[0].Credential.Ref.File} {
		name := strings.TrimPrefix(f, "/run/secrets/")
		if _, ok := compose.Secrets[name]; !ok || !contains(svc.Secrets, name) {
			t.Errorf("config reads %s, but the llm-proxy service has no secret %q", f, name)
		}
	}

	if want := cfg.Server.DrainTimeout.String(); !strings.HasPrefix(want, "10m") || svc.GracePeriod != "10m" {
		t.Errorf("stop_grace_period = %q, drain_timeout = %s; want 10m and 10m", svc.GracePeriod, want)
	}
	for _, p := range svc.Ports {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			t.Errorf("port %q is not bound to the host loopback", p)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
