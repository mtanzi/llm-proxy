//go:build noui

package ui

import (
	"io/fs"
	"net/http"
	"testing/fstest"
)

const stubPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>llm-proxy</title></head>
<body>
<h1>llm-proxy</h1>
<p>This binary was built without the web UI (build tag noui). Run <code>make build</code> after <code>make ui-build</code> to embed it.</p>
</body>
</html>
`

var stubFS fs.FS = fstest.MapFS{"index.html": {Data: []byte(stubPage)}}

// Handler serves the stub page for every path. It expects the request path
// with Prefix stripped.
func Handler() http.Handler { return newHandler(stubFS) }
