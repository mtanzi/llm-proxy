package httpmw

import (
	"net/http"
	"strings"
)

// Redacted replaces the value of a credential header in logs.
const Redacted = "[redacted]"

// redactedHeaders are the header names, in canonical form, whose values are
// never logged.
var redactedHeaders = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	"X-Api-Key":           true,
	"X-Llm-Proxy-Key":     true,
	"Cookie":              true,
	"Set-Cookie":          true,
}

// RedactHeaders returns a copy of h, with the values of credential headers
// replaced by Redacted and each header joined into one string. It is the only
// place headers are prepared for logging.
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		name = http.CanonicalHeaderKey(name)
		if redactedHeaders[name] {
			out[name] = Redacted
			continue
		}
		out[name] = strings.Join(values, ", ")
	}
	return out
}
