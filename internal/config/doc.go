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
// Environment overrides. Every scalar key can be overridden by LLM_PROXY_ plus
// the upper-cased key path joined by underscores, for example
// LLM_PROXY_SERVER_DRAIN_TIMEOUT or LLM_PROXY_LISTEN_TLS_CERT_FILE. A secret
// reference is overridden by its parts: LLM_PROXY_DATABASE_URL_ENV or
// LLM_PROXY_DATABASE_URL_FILE; setting either replaces the whole reference from
// the file. Lists (providers, keys, pricing) are not overridable from the
// environment.
//
// Secrets are never inlined. A resolved secret lives in an unexported field,
// is returned only by Secret.Reveal, and is redacted from fmt, slog, JSON and
// the read-only View.
package config
