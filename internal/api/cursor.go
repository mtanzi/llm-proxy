package api

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/mtanzi/spyglass/internal/core"
)

// errBadCursor is returned by decodeCursor for any text this API did not issue.
var errBadCursor = errors.New("api: invalid cursor")

// encodeCursor returns the opaque wire form of c: base64url, without padding,
// of the timestamp in Unix microseconds and the call id, joined by a dot. A
// tool-call cursor with a non-zero Idx adds a third field, the index, because
// one call carries several tool halves. The store keeps microsecond
// timestamps, so nothing is lost.
func encodeCursor(c core.Cursor) string {
	raw := strconv.FormatInt(c.TS.UnixMicro(), 10) + "." + c.ID.String()
	if c.Idx != 0 {
		raw += "." + strconv.Itoa(c.Idx)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses text from encodeCursor. It accepts only the exact form
// encodeCursor writes, so altered, truncated or hand-made text is refused.
func decodeCursor(s string) (core.Cursor, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return core.Cursor{}, errBadCursor
	}
	ts, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return core.Cursor{}, errBadCursor
	}
	var idx int
	if i, rest, more := strings.Cut(id, "."); more {
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 || strconv.Itoa(n) != rest {
			return core.Cursor{}, errBadCursor
		}
		id, idx = i, n
	}
	us, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || strconv.FormatInt(us, 10) != ts {
		return core.Cursor{}, errBadCursor
	}
	cid, err := core.ParseID(id)
	if err != nil || cid.String() != id {
		return core.Cursor{}, errBadCursor
	}
	return core.Cursor{TS: time.UnixMicro(us).UTC(), ID: cid, Idx: idx}, nil
}
