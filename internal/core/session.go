package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Digest is a SHA-256 hash. The extractor computes digests over in-memory
// request bytes; the bytes are not kept.
type Digest [sha256.Size]byte

// DigestOf hashes b.
func DigestOf(b []byte) Digest { return sha256.Sum256(b) }

// SessionKey derives a session key for a request with no session header. It is
// a pure hash of (key name, client, tools and system digest, first user message
// digest): it keeps no state, has no timeout and gives the same result on every
// replica. Clients with identical openings merge into one session.
func SessionKey(keyName, client string, toolsAndSystem, firstUser Digest) string {
	h := sha256.New()
	writeField(h.Write, []byte(keyName))
	writeField(h.Write, []byte(client))
	writeField(h.Write, toolsAndSystem[:])
	writeField(h.Write, firstUser[:])
	sum := h.Sum(nil)
	return "derived-" + hex.EncodeToString(sum[:16])
}

// writeField writes a length-prefixed field, so ("ab","c") and ("a","bc") differ.
func writeField(write func([]byte) (int, error), b []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	_, _ = write(n[:])
	_, _ = write(b)
}
