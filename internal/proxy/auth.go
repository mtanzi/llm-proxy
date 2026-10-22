package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/family/anthropic"
)

// KeyLookup finds a virtual key by the lowercase hex SHA-256 of its plaintext.
// keyring.Keyring implements it.
type KeyLookup interface {
	Lookup(sha256 string) (core.Key, bool)
}

// Authenticate checks the virtual key a request presents in proxy_key mode:
// Authorization: Bearer <key>, otherwise x-api-key. It returns the key name,
// which the correlation step stores as key_name. On failure it returns a
// protocol-correct 401 and ok false: a missing or unknown key is
// MissingOrInvalidKeyError, a disabled key is DisabledKeyError. The presented
// key is hashed at once and never logged, stored or returned.
func Authenticate(h http.Header, keys KeyLookup, requestID string) (name string, errResp anthropic.ErrorResponse, ok bool) {
	presented := presentedKey(h)
	if presented == "" {
		return "", anthropic.MissingOrInvalidKeyError(requestID), false
	}
	sum := sha256.Sum256([]byte(presented))
	key, found := keys.Lookup(hex.EncodeToString(sum[:]))
	if !found {
		return "", anthropic.MissingOrInvalidKeyError(requestID), false
	}
	if key.Disabled {
		return "", anthropic.DisabledKeyError(requestID), false
	}
	return key.Name, anthropic.ErrorResponse{}, true
}

// presentedKey returns the credential from the Authorization bearer form, or
// else from x-api-key. It returns "" when neither carries one.
func presentedKey(h http.Header) string {
	if scheme, token, found := strings.Cut(strings.TrimSpace(h.Get("Authorization")), " "); found &&
		strings.EqualFold(scheme, "Bearer") {
		if token = strings.TrimSpace(token); token != "" {
			return token
		}
	}
	return strings.TrimSpace(h.Get("X-Api-Key"))
}
