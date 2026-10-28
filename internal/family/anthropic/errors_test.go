package anthropic

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testRequestID = "0198a5c0-1234-7abc-8def-0123456789ab"

func TestErrorBodiesGolden(t *testing.T) {
	tests := []struct {
		name    string
		resp    ErrorResponse
		status  int
		errType string
	}{
		{"invalid_key", MissingOrInvalidKeyError(testRequestID), 401, "authentication_error"},
		{"disabled_key", DisabledKeyError(testRequestID), 401, "authentication_error"},
		{"oversize_body", OversizeBodyError(testRequestID), 413, "request_too_large"},
		{"malformed_request", MalformedRequestError(testRequestID), 400, "invalid_request_error"},
		{"not_found", NotFoundError(testRequestID), 404, "not_found_error"},
		{"upstream_unreachable", UpstreamUnreachableError(testRequestID), 502, "api_error"},
		{"upstream_timeout", UpstreamTimeoutError(testRequestID), 504, "timeout_error"},
		{"internal", InternalError(testRequestID), 500, "api_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resp.Status != tt.status {
				t.Errorf("status = %d, want %d", tt.resp.Status, tt.status)
			}
			if got := tt.resp.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content-type = %q", got)
			}
			if got := tt.resp.Header.Get("X-Request-Id"); got != testRequestID {
				t.Errorf("x-request-id = %q", got)
			}

			// The documented shape: top-level type "error", nested type and message.
			var b struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(tt.resp.Body, &b); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if b.Type != "error" || b.Error.Type != tt.errType || b.Error.Message == "" || b.RequestID != testRequestID {
				t.Errorf("body shape = %+v", b)
			}

			path := filepath.Join("testdata", "error_"+tt.name+".golden.json")
			if *update {
				if err := os.WriteFile(path, append(tt.resp.Body, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(tt.resp.Body) + "\n"; got != string(want) {
				t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestErrorWithoutRequestID(t *testing.T) {
	r := InternalError("")
	if _, ok := r.Header["X-Request-Id"]; ok {
		t.Error("x-request-id set without a request id")
	}
	if want := `{"type":"error","error":{"type":"api_error","message":"internal error"}}`; string(r.Body) != want {
		t.Errorf("body = %s", r.Body)
	}
}
