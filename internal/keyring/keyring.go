package keyring

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/mtanzi/llm-proxy/internal/core"
)

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Keyring is an immutable map from SHA-256 to key. It is safe for concurrent
// use because nothing modifies it after New.
type Keyring struct {
	byHash map[string]core.Key
}

// New builds a Keyring from keys. It rejects an empty name, a duplicate name,
// a hash that is not 64 lowercase hex characters and a duplicate hash. The
// error lists every problem and never contains a key hash.
func New(keys []core.Key) (*Keyring, error) {
	byHash := make(map[string]core.Key, len(keys))
	names := make(map[string]int, len(keys))
	var errs []error
	for i, k := range keys {
		if k.Name == "" {
			errs = append(errs, fmt.Errorf("keys[%d]: name is required", i))
		} else if j, dup := names[k.Name]; dup {
			errs = append(errs, fmt.Errorf("keys[%d]: duplicate name %q (also keys[%d])", i, k.Name, j))
		} else {
			names[k.Name] = i
		}
		if !hashRE.MatchString(k.SHA256) {
			errs = append(errs, fmt.Errorf("keys[%d]: sha256 must be 64 lowercase hex characters", i))
			continue
		}
		if _, dup := byHash[k.SHA256]; dup {
			errs = append(errs, fmt.Errorf("keys[%d]: duplicate sha256", i))
			continue
		}
		byHash[k.SHA256] = k
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid keys: %w", errors.Join(errs...))
	}
	return &Keyring{byHash: byHash}, nil
}

// Lookup returns the key with the given lowercase hex SHA-256.
func (r *Keyring) Lookup(sha256 string) (core.Key, bool) {
	k, ok := r.byHash[sha256]
	return k, ok
}

// Len returns the number of keys.
func (r *Keyring) Len() int { return len(r.byHash) }
