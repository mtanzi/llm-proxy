package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/httpmw"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testConfig() *config.Config {
	return &config.Config{
		Listen: config.Listen{Data: "127.0.0.1:0", Admin: "127.0.0.1:0"},
		Server: config.Server{MaxRequestBytes: 1024},
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestBuildServersRefusesNonLoopbackAdmin(t *testing.T) {
	for _, addr := range []string{":9090", "0.0.0.0:9090", "10.0.0.5:9090", "[::]:9090", "example.com:9090"} {
		cfg := testConfig()
		cfg.Listen.Admin = addr
		s, err := BuildServers(cfg, testLogger())
		if err == nil || s != nil {
			t.Errorf("admin %q: got servers %v, err %v; want a refusal", addr, s, err)
		} else if !strings.Contains(err.Error(), "loopback") {
			t.Errorf("admin %q: error %q does not mention loopback", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9090", "[::1]:9090", "localhost:9090"} {
		cfg := testConfig()
		cfg.Listen.Admin = addr
		if _, err := BuildServers(cfg, testLogger()); err != nil {
			t.Errorf("admin %q: %v", addr, err)
		}
	}
}

func TestServerTimeouts(t *testing.T) {
	s, err := BuildServers(testConfig(), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	for name, srv := range map[string]*http.Server{"data": s.Data, "admin": s.Admin} {
		if srv.ReadHeaderTimeout <= 0 || srv.IdleTimeout <= 0 {
			t.Errorf("%s: ReadHeaderTimeout %v, IdleTimeout %v; want both set", name, srv.ReadHeaderTimeout, srv.IdleTimeout)
		}
		if srv.WriteTimeout != 0 {
			t.Errorf("%s: WriteTimeout = %v, want none", name, srv.WriteTimeout)
		}
	}
}

func TestPlanesServeTheirRoutes(t *testing.T) {
	s, err := BuildServers(testConfig(), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	dataLn, adminLn := listen(t), listen(t)
	go func() { _ = s.ServeData(dataLn) }()
	go func() { _ = s.ServeAdmin(adminLn) }()
	t.Cleanup(func() { _ = s.Data.Close(); _ = s.Admin.Close() })

	tests := []struct {
		method, base, path string
		want               int
	}{
		{"POST", dataLn.Addr().String(), "/v1/messages", http.StatusNotImplemented},
		{"HEAD", dataLn.Addr().String(), "/api/hello", http.StatusNotImplemented},
		{"GET", dataLn.Addr().String(), "/api/hello", http.StatusMethodNotAllowed},
		{"GET", dataLn.Addr().String(), "/admin/requests", http.StatusNotFound},
		{"GET", adminLn.Addr().String(), "/admin/requests", http.StatusNotImplemented},
		{"GET", adminLn.Addr().String(), "/metrics", http.StatusNotImplemented},
		{"GET", adminLn.Addr().String(), "/v1/messages", http.StatusNotFound},
	}
	for _, tt := range tests {
		req, _ := http.NewRequest(tt.method, "http://"+tt.base+tt.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Errorf("%s %s%s = %d, want %d", tt.method, tt.base, tt.path, resp.StatusCode, tt.want)
		}
		if resp.Header.Get("x-request-id") == "" {
			t.Errorf("%s %s%s: no x-request-id", tt.method, tt.base, tt.path)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s%s: CORS header set", tt.method, tt.base, tt.path)
		}
	}
}

func TestOversizeBodyRejected(t *testing.T) {
	s, err := BuildServers(testConfig(), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ln := listen(t)
	go func() { _ = s.ServeData(ln) }()
	t.Cleanup(func() { _ = s.Data.Close() })

	resp, err := http.Post("http://"+ln.Addr().String()+"/v1/messages", "application/json", strings.NewReader(strings.Repeat("x", 2048)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), `"request_too_large"`) {
		t.Errorf("status %d, body %s; want 413 request_too_large", resp.StatusCode, body)
	}
}

// streamingServer serves a response that lasts 6 chunks x 50ms, far past every
// timeout set on the server. clear says whether the handler clears its write
// deadline, as a streaming handler must.
func streamingServer(t *testing.T, clear bool) string {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if clear {
			if err := httpmw.ClearWriteDeadline(w); err != nil {
				t.Errorf("ClearWriteDeadline: %v", err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 6; i++ {
			_, _ = io.WriteString(w, "data: chunk\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})
	srv := newServer("", httpmw.Wrap(h, httpmw.Options{Logger: testLogger()}), testLogger())
	srv.ReadHeaderTimeout = 50 * time.Millisecond
	srv.IdleTimeout = 50 * time.Millisecond
	srv.WriteTimeout = 100 * time.Millisecond // the production server sets none
	ln := listen(t)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

func TestStreamingOutlastsServerTimeouts(t *testing.T) {
	t.Run("handler clears its deadline", func(t *testing.T) {
		resp, err := http.Get(streamingServer(t, true))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("stream cut: %v", err)
		}
		if n := strings.Count(string(body), "data: chunk"); n != 6 {
			t.Errorf("got %d chunks, want 6", n)
		}
	})
	t.Run("control: without clearing the timeout cuts it", func(t *testing.T) {
		resp, err := http.Get(streamingServer(t, false))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err == nil && strings.Count(string(body), "data: chunk") == 6 {
			t.Error("stream completed despite the write timeout; the test proves nothing")
		}
	})
}

func TestDataPlaneServesTLS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeSelfSigned(t, dir)
	cfg := testConfig()
	cfg.Listen.TLS = config.TLS{CertFile: certFile, KeyFile: keyFile}
	s, err := BuildServers(cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ln := listen(t)
	go func() { _ = s.ServeData(ln) }()
	t.Cleanup(func() { _ = s.Data.Close() })

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Head("https://" + ln.Addr().String() + "/api/hello")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.TLS == nil || resp.Header.Get("x-request-id") == "" {
		t.Errorf("TLS %v, x-request-id %q", resp.TLS, resp.Header.Get("x-request-id"))
	}
}

func TestBuildServersRefusesHalfTLS(t *testing.T) {
	cfg := testConfig()
	cfg.Listen.TLS = config.TLS{CertFile: "cert.pem"}
	if _, err := BuildServers(cfg, testLogger()); err == nil {
		t.Error("want an error for cert_file without key_file")
	}
}

func writeSelfSigned(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	write := func(path, typ string, b []byte) {
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(certFile, "CERTIFICATE", der)
	write(keyFile, "EC PRIVATE KEY", keyDER)
	return certFile, keyFile
}
