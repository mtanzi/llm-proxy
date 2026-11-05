// Package fakeupstream is a fake Anthropic upstream for tests (ADR 0038). It
// is an httptest.Server that replays the recorded exchanges under
// testdata/fixtures with controllable timing: chunk splits, delays, ping
// events, mid-stream aborts, and any status. It is test support: only _test.go
// files import it.
//
// A fixture is a directory:
//
//	meta.json     endpoint, status, streamed, timings (required)
//	request.json  the request the exchange answered (required)
//	response.sse  the response body of a streamed exchange, or
//	response.json the response body of any other
//	replay.json   optional: response headers and the chunk plan
//
// replay.json has the shape {"headers": {"Request-Id": "req_01"}, "chunks":
// [{"bytes": 120, "delay_ms": 0}]}. Without it the server sends the
// fixture's content type and one chunk per SSE event, or the whole body for a
// non-streamed exchange. A Reply can override each part.
package fakeupstream
