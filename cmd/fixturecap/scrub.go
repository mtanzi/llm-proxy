package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// keepKeys are the JSON keys whose string values are structure, not content:
// types, roles, ids and names the proxy records as facts. Every other string
// value of a request or response is replaced.
var keepKeys = map[string]bool{
	"type": true, "role": true, "model": true, "name": true, "id": true,
	"tool_use_id": true, "media_type": true, "stop_reason": true, "stop_sequence": true,
	"service_tier": true, "request_id": true, "event": true, "ttl": true, "speed": true,
}

// requestHeaderKeep lists the request headers a fixture keeps, in canonical
// form. Credentials and everything not named here are dropped.
var requestHeaderKeep = []string{"User-Agent", "Anthropic-Version", "Anthropic-Beta", "Content-Type", "Traceparent"}

// idHeaders carry a real identifier, replaced by a stable fake: the same input
// always gives the same output, so exchanges of one session still match.
var idHeaders = map[string]string{
	"X-Claude-Code-Session-Id":      "session",
	"X-Claude-Code-Agent-Id":        "agent",
	"X-Claude-Code-Parent-Agent-Id": "agent",
	"X-Claude-Code-Prompt-Id":       "prompt",
}

// plainHeaders are Claude Code hint headers kept as they are: they hold
// categories and counters, not content.
var plainHeaders = []string{
	"X-Claude-Code-Agent-Type", "X-Claude-Code-Request-Class", "X-Claude-Code-Compaction",
	"X-Claude-Code-Context-Compacted", "X-Claude-Code-Prev-Tool-Durations",
}

// responseHeaderKeep lists the response headers a fixture replays.
var responseHeaderKeep = []string{"Request-Id", "Retry-After", "Anthropic-Ratelimit-Requests-Limit", "Anthropic-Ratelimit-Requests-Remaining", "Anthropic-Ratelimit-Tokens-Limit", "Anthropic-Ratelimit-Tokens-Remaining"}

// leakPatterns must match nothing in a scrubbed fixture.
var leakPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\blp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)"?(authorization|x-api-key|x-llm-proxy-key|cookie)"?\s*[:=]\s*"?[A-Za-z0-9]`),
	regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
	regexp.MustCompile(`/(Users|home)/[A-Za-z0-9._-]+/`),
}

func runScrub(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scrub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "raw exchange directory written by record (required)")
	out := fs.String("out", "", "fixture directory to write, under testdata/fixtures (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "fixturecap scrub: -in and -out are required")
		return 2
	}
	if err := scrubExchange(*in, *out); err != nil {
		_, _ = fmt.Fprintf(stderr, "fixturecap scrub: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, `wrote %s
Next:
  1. Read every file in it: the scrub is mechanical, you are the review.
  2. make goldens          (writes record.golden.json for the new fixture)
  3. Read the golden, add a case to TestFixtureFacts if it shows a new rule.
  4. Commit the fixture. Never commit the raw capture.
`, *out)
	return 0
}

// scrubExchange reads the raw exchange in and writes the fixture out. It
// writes nothing when the result still looks like it leaks.
func scrubExchange(in, out string) error {
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(in, name))
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	metaRaw, err := read("meta.json")
	if err != nil {
		return err
	}
	var meta rawMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return fmt.Errorf("meta.json: %w", err)
	}
	reqRaw, err := read("request.json")
	if err != nil {
		return err
	}
	respName := "response.json"
	if meta.Streamed {
		respName = "response.sse"
	}
	respRaw, err := read(respName)
	if err != nil {
		return err
	}

	reqBody, err := scrubJSON(reqRaw)
	if err != nil {
		return fmt.Errorf("request.json: %w", err)
	}
	var respBody []byte
	if meta.Streamed {
		respBody, err = scrubSSE(respRaw)
	} else {
		respBody, err = scrubJSON(respRaw)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", respName, err)
	}

	files := map[string][]byte{
		"meta.json":    mustJSON(meta),
		"request.json": reqBody,
		respName:       respBody,
	}
	if raw, err := read("request.headers.json"); err == nil {
		h, err := scrubRequestHeaders(raw)
		if err != nil {
			return fmt.Errorf("request.headers.json: %w", err)
		}
		files["request.headers.json"] = h
	}
	if raw, err := read("response.headers.json"); err == nil {
		h, err := replayHeaders(raw)
		if err != nil {
			return fmt.Errorf("response.headers.json: %w", err)
		}
		if h != nil {
			files["replay.json"] = h
		}
	}

	for name, data := range files {
		if err := checkLeaks(name, data); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// checkLeaks fails when a scrubbed file still matches a leak pattern.
func checkLeaks(name string, data []byte) error {
	for _, re := range leakPatterns {
		if m := re.Find(data); m != nil {
			return fmt.Errorf("%s still matches %s (%q): nothing written; scrub the raw capture by hand or drop that exchange", name, re, m)
		}
	}
	return nil
}

// scrubJSON replaces the content strings of a JSON document and writes it
// indented, the way the committed fixtures are.
func scrubJSON(raw []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return marshal(scrubValue("", v), " ")
}

// scrubSSE scrubs the data line of every event and keeps the rest of the
// stream (event names, ping events, blank lines, comments) as it is.
func scrubSSE(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			out.WriteString(line + "\n")
			continue
		}
		payload = strings.TrimSpace(payload)
		var v any
		dec := json.NewDecoder(strings.NewReader(payload))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("data line is not JSON: %w", err)
		}
		b, err := marshal(scrubValue("", v), "")
		if err != nil {
			return nil, err
		}
		out.WriteString("data: " + string(bytes.TrimSpace(b)) + "\n")
	}
	return out.Bytes(), sc.Err()
}

func marshal(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scrubValue walks a decoded JSON value. A string under a key in keepKeys
// stays; every other string becomes placeholder text of the same byte length,
// so sizes the proxy records (argument and result bytes) survive.
func scrubValue(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			x[k] = scrubValue(k, child)
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = scrubValue(key, child)
		}
		return x
	case string:
		if keepKeys[key] {
			return x
		}
		return placeholder(x)
	}
	return v
}

const placeholderPrefix = "FAKEPROMPT-"

// placeholder returns text with the byte length of s that carries no content:
// the marker prefix the other fixtures use, then x. An empty string stays empty.
func placeholder(s string) string {
	n := len(s)
	if n == 0 {
		return ""
	}
	if n < len(placeholderPrefix) {
		return strings.Repeat("x", n)
	}
	return placeholderPrefix + strings.Repeat("x", n-len(placeholderPrefix))
}

// fakeID maps a real identifier to a stable fake of the same kind.
func fakeID(kind, v string) string {
	sum := sha256.Sum256([]byte(v))
	return kind + "-" + hex.EncodeToString(sum[:])[:8]
}

// fakeTraceparent keeps the W3C shape with ids derived from the real ones.
func fakeTraceparent(v string) string {
	sum := sha256.Sum256([]byte(v))
	h := hex.EncodeToString(sum[:])
	return "00-" + h[:32] + "-" + h[32:48] + "-01"
}

// scrubRequestHeaders keeps the headers a fixture needs, with identifiers
// replaced.
func scrubRequestHeaders(raw []byte) ([]byte, error) {
	var h http.Header
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, name := range requestHeaderKeep {
		if v := h.Get(name); v != "" {
			if name == "Traceparent" {
				v = fakeTraceparent(v)
			}
			out[name] = v
		}
	}
	for name, kind := range idHeaders {
		if v := h.Get(name); v != "" {
			out[name] = fakeID(kind, v)
		}
	}
	for _, name := range plainHeaders {
		if v := h.Get(name); v != "" {
			out[name] = v
		}
	}
	return marshalSorted(out)
}

// replayHeaders turns the raw response headers into a replay.json, or nil when
// none is worth keeping.
func replayHeaders(raw []byte) ([]byte, error) {
	var h http.Header
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	keep := map[string]string{}
	for _, name := range responseHeaderKeep {
		if v := h.Get(name); v != "" {
			keep[name] = v
		}
	}
	if len(keep) == 0 {
		return nil, nil
	}
	return marshal(map[string]any{"headers": keep}, " ")
}

func marshalSorted(m map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		buf.WriteString(" " + string(kb) + ": " + string(vb))
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
