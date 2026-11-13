package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/mtanzi/spyglass/internal/core"
)

// MaxBodyBytes bounds the buffer for a non-streaming response body. Past it
// the scanner reads nothing more and the facts are marked partial.
const MaxBodyBytes = 8 << 20

// maxFieldLen bounds a provider or client string kept in a record, such as a
// model, a tool id or a stop reason.
const maxFieldLen = 256

// boundedString returns r when it is a JSON string within maxFieldLen. Anything
// else is malformed and yields the empty string, which the store writes as null.
func boundedString(r gjson.Result) string {
	if r.Type != gjson.String || len(r.Str) > maxFieldLen {
		return ""
	}
	return r.Str
}

// Usage holds the usage fields of a response as the provider sent them. A nil
// field was not sent. Normalising them into disjoint columns is the
// extractor's work.
type Usage struct {
	InputTokens              *int
	OutputTokens             *int
	CacheReadInputTokens     *int
	CacheCreationInputTokens *int
	Ephemeral5mInputTokens   *int
	Ephemeral1hInputTokens   *int
}

// Facts is what a Scanner kept from one response. Fields it could not read
// stay zero or nil. It holds no body text.
type Facts struct {
	Usage      Usage
	UsageRaw   json.RawMessage // provider usage fields, later events over earlier ones
	StopReason string
	ErrorType  string // the provider's error type; its message is not kept
	ToolUses   []core.ToolUse
	// ToolResults holds the results of server-side tools, which arrive complete
	// in the response.
	ToolResults []core.ToolResult
	// Complete says the response ended the way the provider ends one: a
	// message_stop event, or a whole non-streaming body. A stream cut short is
	// not complete.
	Complete bool
	// Partial says a bound was passed or an event could not be read, so some
	// facts may be missing.
	Partial bool
}

// Scanner reads one response incrementally and keeps only its facts. It is
// pure code over byte chunks. Not safe for concurrent use.
type Scanner interface {
	// Write feeds the next chunk of the response body.
	Write(p []byte)
	// ContentStarted reports whether a content event has been seen: the
	// moment the time to first token is measured.
	ContentStarted() bool
	// Facts returns what was kept. Call it once the body has ended; it may be
	// called again with the same result.
	Facts() Facts
}

// RequestStreams reports whether a /v1/messages request body asks for a
// streamed response.
func RequestStreams(body []byte) bool {
	return gjson.GetBytes(body, "stream").Bool()
}

// NewScanner returns a Scanner for an SSE response when streamed is true, and
// for a single JSON body otherwise.
func NewScanner(streamed bool) Scanner {
	if streamed {
		return newStreamScanner()
	}
	return &bodyScanner{}
}

// streamScanner interprets Anthropic SSE events.
type streamScanner struct {
	split   *SSESplitter
	facts   Facts
	usage   map[string]json.RawMessage
	started bool
	blocks  map[int64]*toolBlock // open tool_use blocks by content block index
}

// toolBlock tracks one tool_use block while its arguments stream in.
type toolBlock struct {
	at       int // index into Facts.ToolUses
	initial  int // size of the input in content_block_start
	deltaLen int // bytes of partial_json so far
}

func newStreamScanner() *streamScanner {
	s := &streamScanner{blocks: map[int64]*toolBlock{}}
	s.split = NewSSESplitter(s.event)
	return s
}

func (s *streamScanner) Write(p []byte)       { s.split.Write(p) }
func (s *streamScanner) ContentStarted() bool { return s.started }

func (s *streamScanner) Facts() Facts {
	f := s.facts
	f.UsageRaw = mergeUsageRaw(s.usage)
	if len(s.blocks) > 0 {
		// A cut stream leaves tool blocks open: report the size seen so far.
		f.ToolUses = append([]core.ToolUse(nil), f.ToolUses...)
		for _, b := range s.blocks {
			f.ToolUses[b.at].ArgBytes = b.size()
		}
	}
	return f
}

func (s *streamScanner) event(ev Event) {
	if ev.Oversize {
		s.facts.Partial = true
		return
	}
	if !gjson.ValidBytes(ev.Data) {
		// An event without data, such as a bare ping, has nothing to read.
		if len(ev.Data) > 0 {
			s.facts.Partial = true
		}
		return
	}
	d := gjson.ParseBytes(ev.Data)
	typ := d.Get("type").String()
	if typ == "" {
		typ = ev.Name
	}
	switch typ {
	case "message_start":
		s.addUsage(d.Get("message.usage"))
	case "content_block_start":
		s.started = true
		s.blockStart(d)
	case "content_block_delta":
		s.started = true
		if d.Get("delta.type").String() == "input_json_delta" {
			if b := s.blocks[d.Get("index").Int()]; b != nil {
				b.deltaLen += len(d.Get("delta.partial_json").String())
			}
		}
	case "content_block_stop":
		s.blockStop(d.Get("index").Int())
	case "message_delta":
		if r := boundedString(d.Get("delta.stop_reason")); r != "" {
			s.facts.StopReason = r
		}
		s.addUsage(d.Get("usage"))
	case "message_stop":
		s.facts.Complete = true
	case "error":
		if t := boundedString(d.Get("error.type")); t != "" {
			s.facts.ErrorType = t
		}
	}
}

func (s *streamScanner) blockStart(d gjson.Result) {
	cb := d.Get("content_block")
	blockType := cb.Get("type").String()
	if isServerToolResult(blockType) {
		s.facts.ToolResults = append(s.facts.ToolResults, serverToolResult(cb))
		return
	}
	src, ok := toolSource(blockType)
	if !ok {
		return
	}
	s.facts.ToolUses = append(s.facts.ToolUses, core.ToolUse{
		Source:     src,
		ToolCallID: boundedString(cb.Get("id")),
		Name:       boundedString(cb.Get("name")),
	})
	s.blocks[d.Get("index").Int()] = &toolBlock{
		at:      len(s.facts.ToolUses) - 1,
		initial: len(cb.Get("input").Raw),
	}
}

// blockStop settles the argument size of a tool block. The arguments arrive as
// partial_json text; a block with none keeps the size of the input its start
// event carried, which matches a non-streaming body.
func (s *streamScanner) blockStop(index int64) {
	b := s.blocks[index]
	if b == nil {
		return
	}
	delete(s.blocks, index)
	s.facts.ToolUses[b.at].ArgBytes = b.size()
}

func (b *toolBlock) size() int {
	if b.deltaLen == 0 {
		return b.initial
	}
	return b.deltaLen
}

// addUsage merges a usage object into the facts: a field a later event sends
// replaces the earlier value.
func (s *streamScanner) addUsage(u gjson.Result) {
	if !u.IsObject() {
		return
	}
	if s.usage == nil {
		s.usage = map[string]json.RawMessage{}
	}
	u.ForEach(func(k, v gjson.Result) bool {
		s.usage[k.String()] = json.RawMessage(v.Raw)
		return true
	})
	setUsage(&s.facts.Usage, u)
}

// bodyScanner buffers a non-streaming body up to MaxBodyBytes and parses it
// once, when the facts are asked for.
type bodyScanner struct {
	buf     []byte
	over    bool
	facts   Facts
	scanned bool
}

func (b *bodyScanner) Write(p []byte) {
	if b.over {
		return
	}
	if room := MaxBodyBytes - len(b.buf); len(p) > room {
		b.over = true
		b.buf = nil
		return
	}
	b.buf = append(b.buf, p...)
}

func (b *bodyScanner) ContentStarted() bool { return false }

func (b *bodyScanner) Facts() Facts {
	if !b.scanned {
		b.scanned = true
		b.facts = parseBody(b.buf, b.over)
		b.buf = nil
	}
	return b.facts
}

// parseBody reads the facts of one JSON body: a message, or an error body.
func parseBody(body []byte, over bool) Facts {
	if over {
		return Facts{Partial: true}
	}
	if len(body) == 0 {
		return Facts{}
	}
	if !gjson.ValidBytes(body) {
		return Facts{Partial: true}
	}
	d := gjson.ParseBytes(body)
	var f Facts
	if d.Get("type").String() == "error" {
		f.ErrorType = boundedString(d.Get("error.type"))
		f.Complete = true
		return f
	}
	if u := d.Get("usage"); u.IsObject() {
		setUsage(&f.Usage, u)
		f.UsageRaw = json.RawMessage(u.Raw)
	}
	f.StopReason = boundedString(d.Get("stop_reason"))
	d.Get("content").ForEach(func(_, blk gjson.Result) bool {
		blockType := blk.Get("type").String()
		if isServerToolResult(blockType) {
			f.ToolResults = append(f.ToolResults, serverToolResult(blk))
		} else if src, ok := toolSource(blockType); ok {
			f.ToolUses = append(f.ToolUses, core.ToolUse{
				Source:     src,
				ToolCallID: boundedString(blk.Get("id")),
				Name:       boundedString(blk.Get("name")),
				ArgBytes:   len(blk.Get("input").Raw),
			})
		}
		return true
	})
	f.Complete = true
	return f
}

// toolSource maps a content block type to who runs the tool.
func toolSource(blockType string) (core.ToolSource, bool) {
	switch blockType {
	case "tool_use":
		return core.ToolSourceClient, true
	case "server_tool_use":
		return core.ToolSourceServer, true
	}
	return "", false
}

// isServerToolResult reports whether a response block type holds the result of
// a server-side tool, such as web_search_tool_result. An MCP result is not one:
// its use is not recorded either, until the MCP gateway adds source = mcp.
func isServerToolResult(blockType string) bool {
	return strings.HasSuffix(blockType, "_tool_result") && blockType != "mcp_tool_result"
}

// serverToolResult measures a server tool result block without keeping it. Its
// size is the whole JSON of its content. A failed tool reports an error object
// as the content, with a type such as web_search_tool_result_error.
func serverToolResult(blk gjson.Result) core.ToolResult {
	c := blk.Get("content")
	return core.ToolResult{
		Source:      core.ToolSourceServer,
		ToolCallID:  boundedString(blk.Get("tool_use_id")),
		IsError:     strings.HasSuffix(c.Get("type").Str, "_error"),
		ResultBytes: len(c.Raw),
	}
}

// setUsage copies the usage fields u carries into dst, leaving the rest alone.
func setUsage(dst *Usage, u gjson.Result) {
	set := func(p **int, path string) {
		if r := u.Get(path); r.Type == gjson.Number {
			n := int(r.Int())
			*p = &n
		}
	}
	set(&dst.InputTokens, "input_tokens")
	set(&dst.OutputTokens, "output_tokens")
	set(&dst.CacheReadInputTokens, "cache_read_input_tokens")
	set(&dst.CacheCreationInputTokens, "cache_creation_input_tokens")
	set(&dst.Ephemeral5mInputTokens, "cache_creation.ephemeral_5m_input_tokens")
	set(&dst.Ephemeral1hInputTokens, "cache_creation.ephemeral_1h_input_tokens")
}

// mergeUsageRaw renders the merged usage fields as one JSON object, with keys
// in sorted order so the output is stable. It returns nil when there were none.
func mergeUsageRaw(m map[string]json.RawMessage) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	// Marshalling raw JSON values that were valid on arrival cannot fail.
	b, _ := json.Marshal(m)
	return b
}
