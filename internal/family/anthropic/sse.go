package anthropic

import "bytes"

// MaxEventBytes bounds the memory of one SSE event: every byte of its lines,
// terminators included. A larger event is skipped, not buffered.
const MaxEventBytes = 1 << 20

// Event is one SSE event as the splitter found it.
type Event struct {
	Name string // the event: field; empty when the event had none
	// Data is the data: lines joined by newlines. It is valid only during the
	// emit call. It is nil when Oversize is set.
	Data []byte
	// Oversize says the event passed the size bound and its content was
	// dropped. The splitter has resynchronised on the blank line that ended it.
	Oversize bool
}

// SSESplitter splits a byte stream into SSE events at blank lines. It is pure
// code over byte chunks: it never reads, and a chunk can end anywhere, inside a
// line or between a carriage return and its line feed. It keeps one event in
// memory at most, bounded by MaxEventBytes. An event that the stream ends
// before its blank line is never emitted. Not safe for concurrent use.
type SSESplitter struct {
	max  int
	emit func(Event)

	line    []byte // the current line, until its terminator arrives
	lineLen int    // bytes of the current line, kept even while discarding
	name    string
	data    []byte
	hasData bool
	hasName bool
	size    int  // bytes of the current event, for the bound
	discard bool // the current event passed the bound: skip to its blank line
	skipLF  bool // the last byte was a carriage return: swallow a line feed
}

// NewSSESplitter returns a splitter that calls emit for each event, with a
// per-event bound of MaxEventBytes.
func NewSSESplitter(emit func(Event)) *SSESplitter {
	return &SSESplitter{max: MaxEventBytes, emit: emit}
}

// Write feeds the next chunk of the stream.
func (s *SSESplitter) Write(p []byte) {
	for len(p) > 0 {
		if s.skipLF {
			s.skipLF = false
			if p[0] == '\n' {
				p = p[1:]
				continue
			}
		}
		i := indexEOL(p)
		if i < 0 {
			s.add(p)
			return
		}
		s.add(p[:i])
		s.endLine()
		s.skipLF = p[i] == '\r'
		p = p[i+1:]
	}
}

// add appends part of the current line.
func (s *SSESplitter) add(b []byte) {
	s.lineLen += len(b)
	if s.discard {
		return
	}
	s.size += len(b)
	if s.size > s.max {
		s.drop()
		return
	}
	s.line = append(s.line, b...)
}

// drop gives up the current event and releases its buffers.
func (s *SSESplitter) drop() {
	s.discard = true
	s.line, s.data = nil, nil
}

func (s *SSESplitter) endLine() {
	blank := s.lineLen == 0
	s.lineLen = 0
	if s.discard {
		if blank {
			s.reset()
			s.emit(Event{Oversize: true})
		}
		return
	}
	if blank {
		if s.hasName || s.hasData {
			s.emit(Event{Name: s.name, Data: s.data})
		}
		s.reset()
		return
	}
	s.size++ // the terminator
	if s.size > s.max {
		s.drop()
		return
	}
	s.field(s.line)
	s.line = s.line[:0]
}

// field applies one non-blank line to the current event.
func (s *SSESplitter) field(line []byte) {
	if line[0] == ':' {
		return // comment
	}
	name, value, _ := bytes.Cut(line, []byte{':'})
	value = bytes.TrimPrefix(value, []byte{' '})
	switch string(name) {
	case "event":
		s.name, s.hasName = string(value), true
	case "data":
		if s.hasData {
			s.data = append(s.data, '\n')
		}
		s.data = append(s.data, value...)
		s.hasData = true
	}
}

// reset clears the state of the event just ended, keeping the buffers.
func (s *SSESplitter) reset() {
	s.line = s.line[:0]
	s.data = s.data[:0]
	s.name = ""
	s.hasName, s.hasData = false, false
	s.size = 0
	s.discard = false
}

// indexEOL returns the index of the first carriage return or line feed in p,
// or -1. It works on bytes: bytes.IndexAny decodes UTF-8, which a chunk cut in
// the middle of a character breaks.
func indexEOL(p []byte) int {
	for i, c := range p {
		if c == '\r' || c == '\n' {
			return i
		}
	}
	return -1
}
