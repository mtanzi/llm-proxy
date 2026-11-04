package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorType is the stable machine-readable class of an API error. The UI maps
// it to an inline message, so the values never change meaning.
type ErrorType string

const (
	ErrInvalidParameter ErrorType = "invalid_parameter"
	ErrInvalidCursor    ErrorType = "invalid_cursor"
	ErrInvalidID        ErrorType = "invalid_id"
	ErrNotFound         ErrorType = "not_found"
	ErrInternal         ErrorType = "internal"
)

// apiError is a failure with its HTTP status and wire form.
type apiError struct {
	status    int
	typ       ErrorType
	message   string
	parameter string
}

func (e *apiError) Error() string { return string(e.typ) + ": " + e.message }

func badParam(name, msg string) *apiError {
	return &apiError{status: http.StatusBadRequest, typ: ErrInvalidParameter, message: msg, parameter: name}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Type      ErrorType `json:"type"`
	Message   string    `json:"message"`
	Parameter string    `json:"parameter,omitempty"`
}

// writeJSON writes v as the JSON body with status. The admin plane sets no CORS
// header, so a page on another origin cannot read the response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("api: encode response", "err", err)
		status, b = http.StatusInternalServerError, []byte(`{"error":{"type":"internal","message":"encode response"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, errorBody{Error: errorDetail{Type: e.typ, Message: e.message, Parameter: e.parameter}})
}

// internalError logs err and answers with a generic internal error: the text of
// a store failure can name hosts or SQL and never goes to the client.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	if r != nil {
		slog.Error("api: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	} else {
		slog.Error("api: request failed", "err", err)
	}
	writeError(w, &apiError{status: http.StatusInternalServerError, typ: ErrInternal, message: "internal error"})
}
