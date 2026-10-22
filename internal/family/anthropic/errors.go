package anthropic

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// Error types of the Anthropic error body. The proxy sends a fixed type per
// builder so clients keep matching on them.
const (
	ErrTypeAuthentication  = "authentication_error"
	ErrTypeInvalidRequest  = "invalid_request_error"
	ErrTypeRequestTooLarge = "request_too_large"
	ErrTypeRateLimit       = "rate_limit_error"
	ErrTypeAPI             = "api_error"
	ErrTypeTimeout         = "timeout_error"
)

// ErrorResponse is a complete proxy-generated error: status, headers and body.
// The caller writes it as is.
type ErrorResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type errorBody struct {
	Type      string      `json:"type"`
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id,omitempty"`
}

type errorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// newError builds the documented shape:
// {"type":"error","error":{"type":...,"message":...},"request_id":...}.
// requestID is the call id; it also goes in the x-request-id header.
func newError(status int, errType, message, requestID string) ErrorResponse {
	// Marshalling a struct of strings cannot fail.
	body, _ := json.Marshal(errorBody{
		Type:      "error",
		Error:     errorDetail{Type: errType, Message: message},
		RequestID: requestID,
	})
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if requestID != "" {
		h.Set("X-Request-Id", requestID)
	}
	return ErrorResponse{Status: status, Header: h, Body: body}
}

// MissingOrInvalidKeyError is the 401 for an absent or unknown virtual key.
func MissingOrInvalidKeyError(requestID string) ErrorResponse {
	return newError(http.StatusUnauthorized, ErrTypeAuthentication, "invalid x-api-key", requestID)
}

// DisabledKeyError is the 401 for a virtual key that is disabled.
func DisabledKeyError(requestID string) ErrorResponse {
	return newError(http.StatusUnauthorized, ErrTypeAuthentication, "this API key is disabled", requestID)
}

// OversizeBodyError is the 413 for a request body above the size limit.
func OversizeBodyError(requestID string) ErrorResponse {
	return newError(http.StatusRequestEntityTooLarge, ErrTypeRequestTooLarge, "request body is too large", requestID)
}

// MalformedRequestError is the 400 for a request the proxy cannot read.
func MalformedRequestError(requestID string) ErrorResponse {
	return newError(http.StatusBadRequest, ErrTypeInvalidRequest, "malformed request", requestID)
}

// UpstreamUnreachableError is the 502 for an upstream the proxy cannot reach.
func UpstreamUnreachableError(requestID string) ErrorResponse {
	return newError(http.StatusBadGateway, ErrTypeAPI, "upstream unreachable", requestID)
}

// UpstreamTimeoutError is the 504 for an upstream that did not answer in time.
func UpstreamTimeoutError(requestID string) ErrorResponse {
	return newError(http.StatusGatewayTimeout, ErrTypeTimeout, "upstream timed out", requestID)
}

// InternalError is the 500 for a failure inside the proxy.
func InternalError(requestID string) ErrorResponse {
	return newError(http.StatusInternalServerError, ErrTypeAPI, "internal error", requestID)
}

// RateLimitedError is the 429 for a limit the proxy enforces itself. The
// retry-after header carries whole seconds, rounded up, at least 1.
func RateLimitedError(requestID string, retryAfter time.Duration) ErrorResponse {
	r := newError(http.StatusTooManyRequests, ErrTypeRateLimit, "rate limit exceeded", requestID)
	secs := int64((retryAfter + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	r.Header.Set("Retry-After", strconv.FormatInt(secs, 10))
	return r
}
