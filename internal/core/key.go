package core

// Key is a virtual key as configured: a name and the lowercase hex SHA-256 of
// the plaintext. The plaintext is never held. Validation of the hash belongs to
// the keyring and config loader.
type Key struct {
	Name     string
	SHA256   string
	Disabled bool
}
