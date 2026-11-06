package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	openapi "github.com/mtanzi/llm-proxy/api"
	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/core"
)

// handlers holds what the data handlers read.
type handlers struct {
	calls     core.CallReader
	config    func() config.View
	live      Live
	now       func() time.Time
	keepAlive time.Duration
}

// listCalls serves GET /admin/calls.
func (h *handlers) listCalls(w http.ResponseWriter, r *http.Request) {
	if h.calls == nil {
		notWired(w)
		return
	}
	p, perr := parseCallParams(r.URL.Query(), h.now())
	if perr != nil {
		writeError(w, perr)
		return
	}
	rows, next, err := h.calls.ListCalls(r.Context(), p.Filter, p.Page)
	if err != nil {
		internalError(w, r, err)
		return
	}
	n, capped, err := h.calls.CountCalls(r.Context(), p.Filter, CountCap)
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := callListJSON{Calls: make([]callSummaryJSON, 0, len(rows)), Count: n, CountCapped: capped}
	for _, c := range rows {
		out.Calls = append(out.Calls, toCallSummary(c))
	}
	if next != nil {
		s := encodeCursor(*next)
		out.NextCursor = &s
	}
	writeJSON(w, http.StatusOK, out)
}

// getCall serves GET /admin/calls/{id}.
func (h *handlers) getCall(w http.ResponseWriter, r *http.Request) {
	if h.calls == nil {
		notWired(w)
		return
	}
	id, err := core.ParseID(r.PathValue("id"))
	if err != nil {
		writeError(w, &apiError{status: http.StatusBadRequest, typ: ErrInvalidID, message: "id is not a call id"})
		return
	}
	d, err := h.calls.GetCall(r.Context(), id)
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, &apiError{status: http.StatusNotFound, typ: ErrNotFound, message: "no such call"})
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, toCallDetail(d))
	}
}

// getConfig serves GET /admin/config. config.View holds no secret by design.
func (h *handlers) getConfig(w http.ResponseWriter, _ *http.Request) {
	if h.config == nil {
		notWired(w)
		return
	}
	v := h.config()
	// The schema wants arrays, never null.
	if v.Providers == nil {
		v.Providers = []config.ProviderView{}
	}
	if v.Pricing == nil {
		v.Pricing = []config.PricingView{}
	}
	if v.Keys == nil {
		v.Keys = []config.KeyView{}
	}
	writeJSON(w, http.StatusOK, v)
}

// getOpenAPI serves GET /admin/openapi.json.
func (h *handlers) getOpenAPI(w http.ResponseWriter, _ *http.Request) {
	spec, err := specJSON()
	if err != nil {
		internalError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(spec)
}

func notWired(w http.ResponseWriter) {
	writeError(w, &apiError{status: http.StatusServiceUnavailable, typ: ErrInternal, message: "admin API is not wired to a store"})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, &apiError{status: http.StatusNotFound, typ: ErrNotFound, message: "no such endpoint"})
}

// specJSON is the embedded OpenAPI YAML as JSON, converted once.
var specJSON = sync.OnceValues(func() ([]byte, error) {
	v, err := decodeYAML(openapi.OpenAPIYAML)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
})
