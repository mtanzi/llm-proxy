// Package ui embeds and serves the built web UI (ADR 0029).
//
// The build copies web/dist to internal/ui/dist, and embed.go embeds it. The
// build tag noui replaces the embed with a stub page, so Go-only development
// and tests need no Node and no dist directory.
package ui
