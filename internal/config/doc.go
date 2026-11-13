// Package config loads, overlays and validates the YAML configuration file.
//
// Load reads one YAML file, rejects unknown fields, applies the environment
// overrides, resolves the secret references and validates the result. A bad
// file is rejected whole: Load returns either a valid *Config or an error that
// lists every problem found.
//
// Reload. A Holder keeps the current Snapshot behind an atomic pointer. A
// request reads Current once and keeps that snapshot for its whole life. Reload
// (on SIGHUP, see Holder.ReloadOnSIGHUP) builds and validates a new snapshot
// and swaps it in; a bad file is rejected whole, the previous snapshot stays,
// and the failure is logged and counted. Restart-only settings (listener
// addresses, TLS files, database URL, queue capacity) keep their running
// values, and a reload that changes them logs a warning.
//
// Keys. Options.BuildKeys builds the key index (keyring.New, passed in by the
// composition root because config imports only core) from the validated keys.
// The index belongs to the Snapshot, so the swap replaces config and index
// together; a build error rejects the reload like a validation error.
//
// Environment overrides. Every scalar key can be overridden by SPYGLASS_ plus
// the upper-cased key path joined by underscores, for example
// SPYGLASS_SERVER_DRAIN_TIMEOUT or SPYGLASS_LISTEN_TLS_CERT_FILE. A secret
// reference is overridden by its parts: SPYGLASS_DATABASE_URL_ENV or
// SPYGLASS_DATABASE_URL_FILE; setting either replaces the whole reference from
// the file. Lists (providers, keys, pricing) are not overridable from the
// environment.
//
// Secrets are never inlined. A resolved secret lives in an unexported field,
// is returned only by Secret.Reveal, and is redacted from fmt, slog, JSON and
// the read-only View.
package config
