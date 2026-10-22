// Package keyring holds virtual-key lookup.
//
// New builds an immutable map from the lowercase hex SHA-256 of a key to its
// core.Key. The map is built from the keys of one config snapshot and travels
// with that snapshot, so a reload swaps it atomically and a request in flight
// keeps the map it started with. The plaintext key is never held.
package keyring
