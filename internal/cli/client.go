package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAdminURL is the default listen.admin address (deployment.md).
const DefaultAdminURL = "http://127.0.0.1:9090"

// adminURLVar names the variable that sets the base URL of the admin listener.
const adminURLVar = "SPYGLASS_ADMIN_URL"

// maxBody bounds one response. A session with many calls is the largest.
const maxBody = 64 << 20

// adminClient is a read-only client of the admin API: a base URL and an
// *http.Client. It has no interface; tests run against an httptest.Server.
type adminClient struct {
	base string
	http *http.Client
}

func newAdminClient(env func(string) string) (*adminClient, error) {
	base := strings.TrimSpace(env(adminURLVar))
	if base == "" {
		base = DefaultAdminURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &cmdError{code: 1, typ: "invalid_admin_url",
			msg:  fmt.Sprintf("%s is %q, which is not an http or https URL", adminURLVar, base),
			help: []string{"set " + adminURLVar + " to the admin address, for example " + DefaultAdminURL}}
	}
	return &adminClient{
		base: strings.TrimRight(base, "/"),
		// A long usage range can take several seconds on the server.
		http: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// get issues GET path?q and decodes the JSON body into out.
func (c *adminClient) get(ctx context.Context, path string, q url.Values, out any) error {
	target := c.base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return c.unreachable(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.unreachable(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return c.unreachable(err)
	}
	if resp.StatusCode >= 400 {
		var e errorResponse
		if json.Unmarshal(body, &e) == nil && e.Error.Type != "" {
			return &cmdError{code: 1, typ: e.Error.Type, msg: cutText(e.Error.Message, 200), param: e.Error.Parameter,
				help: []string{"spyglass describe"}}
		}
		return &cmdError{code: 1, typ: "bad_response",
			msg:  fmt.Sprintf("%s answered %d without an API error body", c.base+path, resp.StatusCode),
			help: []string{"check that " + adminURLVar + " points at the Spyglass admin listener"}}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &cmdError{code: 1, typ: "bad_response",
			msg:  fmt.Sprintf("%s answered %d with a body that is not the expected JSON: %v", c.base+path, resp.StatusCode, err),
			help: []string{"check that " + adminURLVar + " points at the Spyglass admin listener"}}
	}
	return nil
}

func (c *adminClient) unreachable(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &cmdError{code: 1, typ: "unreachable",
		msg: fmt.Sprintf("cannot reach the admin API at %s: %v", c.base, err),
		help: []string{
			"start the server with: spyglass serve",
			"or set " + adminURLVar + " to the admin address (for a remote server, an SSH tunnel to its admin port)",
		}}
}

// escapeSegment percent-encodes everything but the RFC 3986 unreserved
// characters, so a session key such as team/a:1 is one path segment.
func escapeSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
