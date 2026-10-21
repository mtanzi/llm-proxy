package core

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// ID is a 16-byte identifier in the UUID layout. Call ids are UUIDv7: the
// first 48 bits are the Unix millisecond timestamp, so ids sort by creation time.
// core defines its own type because it imports the standard library only.
type ID [16]byte

// ErrInvalidID is returned by ParseID for text that is not a canonical UUID.
var ErrInvalidID = errors.New("core: invalid id")

// NewID returns a new UUIDv7 for the current time.
func NewID() ID { return NewIDAt(time.Now()) }

// NewIDAt returns a new UUIDv7 whose timestamp is t, truncated to the millisecond.
func NewIDAt(t time.Time) ID {
	var id ID
	ms := uint64(t.UnixMilli())
	id[0], id[1], id[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	id[3], id[4], id[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	// Random bytes never fail since Go 1.24.
	_, _ = rand.Read(id[6:])
	id[6] = id[6]&0x0f | 0x70 // version 7
	id[8] = id[8]&0x3f | 0x80 // variant 10
	return id
}

// ParseID parses the canonical 8-4-4-4-12 lowercase or uppercase hex form.
func ParseID(s string) (ID, error) {
	var id ID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return id, ErrInvalidID
	}
	var raw [32]byte
	n := copy(raw[:], s[0:8])
	n += copy(raw[n:], s[9:13])
	n += copy(raw[n:], s[14:18])
	n += copy(raw[n:], s[19:23])
	copy(raw[n:], s[24:])
	if _, err := hex.Decode(id[:], raw[:]); err != nil {
		return ID{}, ErrInvalidID
	}
	return id, nil
}

// String returns the canonical lowercase form.
func (id ID) String() string {
	var buf [36]byte
	hex.Encode(buf[0:8], id[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], id[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], id[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], id[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], id[10:])
	return string(buf[:])
}

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool { return id == ID{} }

// Time returns the millisecond timestamp of a UUIDv7.
func (id ID) Time() time.Time {
	ms := int64(id[0])<<40 | int64(id[1])<<32 | int64(id[2])<<24 | int64(id[3])<<16 | int64(id[4])<<8 | int64(id[5])
	return time.UnixMilli(ms).UTC()
}

// Compare orders ids bytewise, which for UUIDv7 is creation order. It returns -1, 0 or 1.
func (id ID) Compare(other ID) int { return bytes.Compare(id[:], other[:]) }

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID) UnmarshalText(b []byte) error {
	parsed, err := ParseID(string(b))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
