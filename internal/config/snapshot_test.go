package config

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// memFS is a file system whose files change between reloads, safe for
// concurrent use.
type memFS struct {
	mu    sync.Mutex
	files map[string]string
}

func (m *memFS) set(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = content
}

func (m *memFS) opts() Options {
	return Options{
		LookupEnv: func(k string) (string, bool) {
			if k == "DB" {
				return "postgres://x", true
			}
			if k == "KEY" {
				return "k", true
			}
			return "", false
		},
		ReadFile: func(p string) ([]byte, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if v, ok := m.files[p]; ok {
				return []byte(v), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	}
}

const cfgPath = "/etc/llm-proxy.yaml"

func newHolder(t *testing.T, content string) (*Holder, *memFS, *bytes.Buffer) {
	t.Helper()
	m := &memFS{files: map[string]string{cfgPath: content}}
	var logs bytes.Buffer
	h := NewHolder(cfgPath, m.opts(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	return h, m, &logs
}

func TestHolderLoadAndCurrent(t *testing.T) {
	m := &memFS{files: map[string]string{cfgPath: base}}
	h := NewHolder(cfgPath, m.opts(), nil)
	if h.Current() != nil {
		t.Fatal("Current before Load is not nil")
	}
	if err := h.Reload(); err == nil {
		t.Error("Reload before Load succeeded")
	}
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	s := h.Current()
	if len(s.Config().Keys) != 2 {
		t.Errorf("snapshot has %d keys", len(s.Config().Keys))
	}
	if err := h.Load(); err == nil {
		t.Error("second Load succeeded")
	}
}

func TestLoadFailureLeavesHolderEmpty(t *testing.T) {
	m := &memFS{files: map[string]string{cfgPath: "nonsense: true"}}
	h := NewHolder(cfgPath, m.opts(), nil)
	if err := h.Load(); err == nil {
		t.Fatal("Load accepted a bad file")
	}
	if h.Current() != nil {
		t.Error("Current is set after a failed Load")
	}
}

func TestOldSnapshotUnaffectedBySwap(t *testing.T) {
	h, m, _ := newHolder(t, base)
	old := h.Current()

	m.set(cfgPath, strings.Replace(base, "sha256: "+hashB, "sha256: "+hashB+", disabled: true", 1)+
		"  - { provider: anthropic, model: m2, from: 2026-10-02, input: 3, output: 4 }\n")
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}

	cur := h.Current()
	if cur == old {
		t.Fatal("snapshot not swapped")
	}
	if !cur.Config().Keys[1].Disabled || len(cur.Config().Pricing) != 2 {
		t.Error("new snapshot lacks the edit")
	}
	if old.Config().Keys[1].Disabled || len(old.Config().Pricing) != 1 {
		t.Error("old snapshot changed after the swap")
	}
}

func TestReloadRotatesSecrets(t *testing.T) {
	m := &memFS{files: map[string]string{
		cfgPath: strings.Replace(base, "{ env: KEY }", "{ file: /s }", 1),
		"/s":    "v1\n",
	}}
	h := NewHolder(cfgPath, m.opts(), nil)
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	old := h.Current()
	m.set("/s", "v2\n")
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	secret := func(s *Snapshot) string { return s.Config().Providers[0].Credential.Ref.Secret().Reveal() }
	if got := secret(h.Current()); got != "v2" {
		t.Errorf("new secret = %q", got)
	}
	if got := secret(old); got != "v1" {
		t.Errorf("old snapshot secret = %q; want v1", got)
	}
}

func TestBadFileKeepsOldSnapshot(t *testing.T) {
	h, m, logs := newHolder(t, base)
	old := h.Current()

	bad := map[string]string{
		"invalid":       strings.Replace(base, "sha256: "+hashB, "sha256: "+hashA, 1),
		"unknown field": base + "bogus: 1\n",
		"missing file":  "",
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			if name == "missing file" {
				m.mu.Lock()
				delete(m.files, cfgPath)
				m.mu.Unlock()
			} else {
				m.set(cfgPath, content)
			}
			if err := h.Reload(); err == nil {
				t.Fatal("Reload accepted a bad file")
			}
			if h.Current() != old {
				t.Error("snapshot replaced by a rejected reload")
			}
		})
	}
	if got := h.Reloads(ResultFailure); got != 3 {
		t.Errorf("failure count = %d; want 3", got)
	}
	if got := h.Reloads(ResultSuccess); got != 0 {
		t.Errorf("success count = %d; want 0", got)
	}
	if !strings.Contains(logs.String(), "config reload rejected") || !strings.Contains(logs.String(), "duplicate sha256") {
		t.Errorf("failure log lacks message or cause:\n%s", logs)
	}

	m.set(cfgPath, base)
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if h.Reloads(ResultSuccess) != 1 || h.Current() == old {
		t.Error("holder did not recover after a good file")
	}
}

func TestRestartOnlyChangeWarnsAndIsIgnored(t *testing.T) {
	file := `
listen: { data: ":8080", admin: "127.0.0.1:9090" }
queue_capacity: 100
database: { url: { env: DB } }
` + strings.SplitN(base, "\n", 3)[2]
	h, m, logs := newHolder(t, file)
	old := h.Current()

	changed := strings.NewReplacer(
		`":8080"`, `":8081"`,
		`"127.0.0.1:9090"`, `"127.0.0.1:9091"`,
		"queue_capacity: 100", "queue_capacity: 5",
		"{ env: DB }", "{ file: /db }",
		"credential: { env: KEY }", "credential: { env: KEY }\n    idle_timeout: 30s",
	).Replace(file)
	changed = "listen: { data: \":8081\", admin: \"127.0.0.1:9091\", tls: { cert_file: /c, key_file: /k } }\n" +
		strings.SplitN(changed, "\n", 3)[2]
	m.set("/db", "postgres://other")
	m.set(cfgPath, changed)
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}

	cfg := h.Current().Config()
	if cfg.Listen != old.Config().Listen || cfg.QueueCapacity != 100 || cfg.Database.URL.String() != "env:DB" {
		t.Errorf("restart-only settings changed: %+v %d %s", cfg.Listen, cfg.QueueCapacity, cfg.Database.URL)
	}
	if got := cfg.Providers[0].IdleTimeout; got != Duration(30*time.Second) {
		t.Errorf("reloadable setting not applied: idle_timeout = %v", got)
	}
	out := logs.String()
	for _, want := range []string{"level=WARN", "listen.data", "listen.admin", "listen.tls.cert_file", "listen.tls.key_file", "database.url", "queue_capacity"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "postgres://") {
		t.Errorf("log leaks the database URL:\n%s", out)
	}

	// The file still differs from the running values, so each reload warns again.
	logs.Reset()
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || h.Current().Config().QueueCapacity != 100 {
		t.Errorf("second reload did not warn and keep the running values:\n%s", logs)
	}
}

func TestConcurrentReadersDuringSwap(t *testing.T) {
	h, m, _ := newHolder(t, base)
	old := h.Current()
	variants := []string{
		base,
		strings.Replace(base, "sha256: "+hashB, "sha256: "+hashB+", disabled: true", 1),
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				cfg := h.Current().Config()
				if len(cfg.Keys) != 2 || len(cfg.Providers) != 1 || cfg.Providers[0].Credential.Ref.Secret().Reveal() != "k" {
					t.Error("reader saw a torn snapshot")
					return
				}
			}
		}()
	}
	for i := range 200 {
		m.set(cfgPath, variants[i%2])
		if err := h.Reload(); err != nil {
			t.Error(err)
			break
		}
	}
	stop.Store(true)
	wg.Wait()
	if h.Reloads(ResultSuccess) != 200 || h.Current() == old {
		t.Errorf("success = %d, snapshot swapped = %t", h.Reloads(ResultSuccess), h.Current() != old)
	}
}

func TestWatchReload(t *testing.T) {
	h, m, logs := newHolder(t, base)
	sigs := make(chan os.Signal)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.WatchReload(ctx, sigs); close(done) }()

	m.set(cfgPath, "nonsense: true")
	sigs <- syscall.SIGHUP
	m.set(cfgPath, base)
	sigs <- syscall.SIGHUP
	cancel()
	<-done

	if h.Reloads(ResultFailure) != 1 || h.Reloads(ResultSuccess) != 1 {
		t.Errorf("failure = %d, success = %d; want 1 and 1", h.Reloads(ResultFailure), h.Reloads(ResultSuccess))
	}
	if !strings.Contains(logs.String(), "config reload rejected") || !strings.Contains(logs.String(), "config reloaded") {
		t.Errorf("handler did not log both outcomes:\n%s", logs)
	}
}

func TestReloadOnSIGHUP(t *testing.T) {
	h, m, _ := newHolder(t, base)
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGHUP)
	defer signal.Stop(guard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.ReloadOnSIGHUP(ctx); close(done) }()

	m.set(cfgPath, base+"\n")
	deadline := time.Now().Add(2 * time.Second)
	for h.Reloads(ResultSuccess) == 0 && time.Now().Before(deadline) {
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if h.Reloads(ResultSuccess) == 0 {
		t.Error("SIGHUP did not trigger a reload")
	}
}

type mapKeys map[string]core.Key

func (m mapKeys) Lookup(h string) (core.Key, bool) { k, ok := m[h]; return k, ok }

func buildMapKeys(keys []core.Key) (KeyIndex, error) {
	m := mapKeys{}
	for _, k := range keys {
		m[k.SHA256] = k
	}
	return m, nil
}

func newKeyedHolder(t *testing.T, build func([]core.Key) (KeyIndex, error)) (*Holder, *memFS) {
	t.Helper()
	m := &memFS{files: map[string]string{cfgPath: base}}
	o := m.opts()
	o.BuildKeys = build
	h := NewHolder(cfgPath, o, nil)
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	return h, m
}

func TestKeyIndexTravelsWithSnapshot(t *testing.T) {
	h, m := newKeyedHolder(t, buildMapKeys)
	old := h.Current()
	if k, ok := old.Keys().Lookup(hashB); !ok || k.Disabled {
		t.Fatalf("lookup before reload: %+v %v", k, ok)
	}

	m.set(cfgPath, strings.Replace(base, "sha256: "+hashB, "sha256: "+hashB+", disabled: true", 1))
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if k, ok := h.Current().Keys().Lookup(hashB); !ok || !k.Disabled {
		t.Errorf("new snapshot index lacks the edit: %+v %v", k, ok)
	}
	if k, _ := old.Keys().Lookup(hashB); k.Disabled {
		t.Error("old snapshot index changed after the swap")
	}
}

func TestNoBuilderMeansNoKeyIndex(t *testing.T) {
	h, _, _ := newHolder(t, base)
	if h.Current().Keys() != nil {
		t.Error("key index without BuildKeys")
	}
}

func TestKeyBuildFailureRejectsReload(t *testing.T) {
	fail := false
	h, m := newKeyedHolder(t, func(k []core.Key) (KeyIndex, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return buildMapKeys(k)
	})
	old := h.Current()
	fail = true
	m.set(cfgPath, base+"\n")
	if err := h.Reload(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Reload = %v", err)
	}
	if h.Current() != old {
		t.Error("snapshot swapped despite key build failure")
	}
	if h.Reloads(ResultFailure) != 1 {
		t.Errorf("failures = %d", h.Reloads(ResultFailure))
	}
}
