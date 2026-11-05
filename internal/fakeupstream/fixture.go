package fakeupstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Meta is a fixture's meta.json. It is the same file the extractor goldens
// read, so a fixture serves both.
type Meta struct {
	Endpoint   string `json:"endpoint"`
	Status     int    `json:"status"`
	Streamed   bool   `json:"streamed"`
	TTFBMs     int    `json:"ttfb_ms"`
	TTFTMs     int    `json:"ttft_ms"`
	DurationMs int    `json:"duration_ms"`
	ErrorClass string `json:"error_class,omitempty"`
}

// Chunk is one write of the response body: Bytes bytes, sent after waiting
// DelayMs milliseconds. A zero Bytes in the last chunk means the rest.
type Chunk struct {
	Bytes   int `json:"bytes"`
	DelayMs int `json:"delay_ms,omitempty"`
}

// Replay is a fixture's optional replay.json.
type Replay struct {
	Headers map[string]string `json:"headers,omitempty"`
	Chunks  []Chunk           `json:"chunks,omitempty"`
}

// Fixture is one recorded exchange.
type Fixture struct {
	Name     string
	Meta     Meta
	Request  []byte
	Response []byte
	Replay   Replay
}

// FixturesDir is the fixture root, relative to a package two levels below the
// repository root (cmd/llm-proxy, internal/<pkg>).
const FixturesDir = "../../testdata/fixtures"

// Load reads the fixture directory dir.
func Load(dir string) (Fixture, error) {
	f := Fixture{Name: filepath.Base(dir)}
	read := func(name string, optional bool) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !(optional && os.IsNotExist(err)) {
			return nil, fmt.Errorf("fixture %s: %w", f.Name, err)
		}
		return b, nil
	}
	b, err := read("meta.json", false)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f.Meta); err != nil {
		return f, fmt.Errorf("fixture %s: meta.json: %w", f.Name, err)
	}
	if f.Request, err = read("request.json", false); err != nil {
		return f, err
	}
	body := "response.json"
	if f.Meta.Streamed {
		body = "response.sse"
	}
	if f.Response, err = read(body, false); err != nil {
		return f, err
	}
	b, err = read("replay.json", true)
	if err != nil {
		return f, err
	}
	if len(b) > 0 {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f.Replay); err != nil {
			return f, fmt.Errorf("fixture %s: replay.json: %w", f.Name, err)
		}
	}
	return f, nil
}

// chunks splits the response body by the replay plan. A fixture without a plan
// sends one chunk per SSE event, or the whole body when it is not streamed.
func (f Fixture) chunks() [][]byte {
	if len(f.Replay.Chunks) == 0 {
		if !f.Meta.Streamed {
			return [][]byte{f.Response}
		}
		return splitEvents(f.Response)
	}
	var out [][]byte
	rest := f.Response
	for _, c := range f.Replay.Chunks {
		n := c.Bytes
		if n <= 0 || n > len(rest) {
			n = len(rest)
		}
		out = append(out, rest[:n])
		rest = rest[n:]
	}
	if len(rest) > 0 {
		out = append(out, rest)
	}
	return out
}

// delays gives the pause before each chunk of the replay plan.
func (f Fixture) delays(n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := 0; i < n && i < len(f.Replay.Chunks); i++ {
		out[i] = time.Duration(f.Replay.Chunks[i].DelayMs) * time.Millisecond
	}
	return out
}

// splitEvents cuts an SSE body after each blank line. The pieces join back to
// the body.
func splitEvents(b []byte) [][]byte {
	var out [][]byte
	for len(b) > 0 {
		i := bytes.Index(b, []byte("\n\n"))
		if i < 0 {
			out = append(out, b)
			break
		}
		out = append(out, b[:i+2])
		b = b[i+2:]
	}
	return out
}
