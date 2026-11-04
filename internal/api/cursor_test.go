package api

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, ts := range []time.Time{
		time.Date(2026, 10, 21, 12, 30, 45, 123456000, time.UTC),
		time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC),
		time.UnixMicro(0).UTC(),
	} {
		c := core.Cursor{TS: ts, ID: core.NewIDAt(ts)}
		s := encodeCursor(c)
		got, err := decodeCursor(s)
		if err != nil {
			t.Fatalf("decode %q: %v", s, err)
		}
		if !got.TS.Equal(c.TS) || got.ID != c.ID || got.Idx != 0 {
			t.Errorf("round trip = %+v, want %+v", got, c)
		}
	}
}

func TestCursorRejectsTampering(t *testing.T) {
	good := core.Cursor{TS: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), ID: core.NewID()}
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	enc := encodeCursor(good)
	for name, s := range map[string]string{
		"empty":              "",
		"not base64":         "!!!",
		"padded":             enc + "=",
		"standard alphabet":  base64.StdEncoding.EncodeToString([]byte("1.x")),
		"truncated":          enc[:len(enc)-4],
		"no separator":       raw("123"),
		"non numeric ts":     raw("abc." + good.ID.String()),
		"leading zero ts":    raw("0123." + good.ID.String()),
		"plus sign ts":       raw("+123." + good.ID.String()),
		"bad id":             raw("123.not-an-id"),
		"upper case id":      raw("123." + upper(good.ID.String())),
		"extra field":        raw("123." + good.ID.String() + ".1"),
		"ts overflow":        raw("99999999999999999999." + good.ID.String()),
		"json shaped cursor": raw(`{"ts":1,"id":"x"}`),
	} {
		if c, err := decodeCursor(s); err == nil {
			t.Errorf("%s: decoded %+v, want error", name, c)
		}
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 32
		}
	}
	return string(b)
}
