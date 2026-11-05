//go:build !noui

package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

// The build copies web/dist to dist (make ui-build). dist is not committed, so
// a build without it needs the noui tag.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the embedded UI. It expects the request path with Prefix
// stripped.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is a literal directory name: cannot fail.
	}
	return newHandler(sub)
}
