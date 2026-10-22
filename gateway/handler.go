package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
)

// Handler serves a catalog over HTTP:
//
//	GET /catalog.json                   the whole catalog document
//	GET /schema.json                    its JSON Schema, SchemaDocument
//	GET /v1/models                      {"models": [...]}, in ID order
//	GET /v1/models/{id...}              one model and every provider's offering of it, such as /v1/models/openai/gpt-6.1-sol
//	GET /v1/providers                   {"providers": [...]}, without their offerings, in ID order
//	GET /v1/providers/{id}              one provider and all of its offerings
//	GET /v1/providers/{id}/{variant...} one variant, resolved, such as /v1/providers/openai/openai/gpt-6.1-sol:fast
//
// A variant that GET /v1/providers/{id}/{variant} returns is Resolved: it
// says, alone, what to send and where. /v1/models takes the query
// parameters lab, family, and provider, which keeps the models that
// provider offers. Errors are {"error": {"code": ..., "message": ...}}. Set
// replaces the catalog while the handler serves.
type Handler struct {
	cur atomic.Pointer[served]
	mux *http.ServeMux
}

// served is a catalog as the handler serves it.
type served struct {
	cat  *Catalog
	doc  []byte
	etag string
	// variants holds every variant, resolved, by provider and variant ID.
	variants map[ProviderID]map[string]Resolved
}

// NewHandler returns a Handler serving c.
func NewHandler(c *Catalog) (*Handler, error) {
	h := &Handler{mux: http.NewServeMux()}
	if err := h.Set(c); err != nil {
		return nil, err
	}
	h.mux.HandleFunc("GET "+DocumentPath, h.document)
	h.mux.HandleFunc("GET "+SchemaPath, h.schema)
	h.mux.HandleFunc("GET /v1/models", h.models)
	h.mux.HandleFunc("GET /v1/models/{id...}", h.model)
	h.mux.HandleFunc("GET /v1/providers", h.providers)
	h.mux.HandleFunc("GET /v1/providers/{id}", h.provider)
	h.mux.HandleFunc("GET /v1/providers/{id}/{variant...}", h.variant)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "no endpoint at "+r.URL.Path)
	})
	return h, nil
}

// Set makes the handler serve c from now on. It serves the document
// compact, as browsers fetch it whole.
func (h *Handler) Set(c *Catalog) error {
	doc, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("gateway: encode: %w", err)
	}
	sum := sha256.Sum256(doc)
	s := &served{cat: c, doc: doc, etag: `"` + hex.EncodeToString(sum[:16]) + `"`,
		variants: make(map[ProviderID]map[string]Resolved, len(c.Providers))}
	for p := range c.Providers {
		vs := make(map[string]Resolved)
		for r := range c.Variants(p) {
			vs[r.ID] = r
		}
		s.variants[p] = vs
	}
	h.cur.Store(s)
	return nil
}

// Catalog returns the catalog the handler serves.
func (h *Handler) Catalog() *Catalog {
	return h.cur.Load().cat
}

// ServeHTTP serves the endpoints that Handler lists.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// document serves the catalog document, answering a request that names its
// ETag with 304 Not Modified.
func (h *Handler) document(w http.ResponseWriter, r *http.Request) {
	s := h.cur.Load()
	w.Header().Set("ETag", s.etag)
	if r.Header.Get("If-None-Match") == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.doc)
}

// ModelList is the body of GET /v1/models.
type ModelList struct {
	Models []Model `json:"models"`
}

// ProviderSummary is a provider as GET /v1/providers lists it: without its
// offerings, but with how many it has of the catalog's models and how many
// unlisted.
type ProviderSummary struct {
	Provider
	Offerings int `json:"offerings"`
	Unlisted  int `json:"unlisted"`
}

// ProviderList is the body of GET /v1/providers.
type ProviderList struct {
	Providers []ProviderSummary `json:"providers"`
}

// ProviderOffering is a provider's offering of a model, with the provider's
// name and endpoint, which its variants take unless they name their own.
type ProviderOffering struct {
	Provider     ProviderID `json:"provider"`
	ProviderName string     `json:"provider_name"`
	Endpoint     Endpoint   `json:"endpoint"`
	Offering
}

// ModelDetail is the body of GET /v1/models/{id}: the model and every
// provider's offering of it, in provider order.
type ModelDetail struct {
	Model
	Offerings []ProviderOffering `json:"offerings"`
}

// Filter selects models as the query parameters of GET /v1/models do. Its
// empty fields select every model.
type Filter struct {
	Provider ProviderID
	Lab      ProviderID
	Family   string
}

// FilterOf returns the filter that the query of r sets.
func FilterOf(r *http.Request) Filter {
	q := r.URL.Query()
	return Filter{Provider: ProviderID(q.Get("provider")), Lab: ProviderID(q.Get("lab")), Family: q.Get("family")}
}

// Selects reports whether f selects m.
func (f Filter) Selects(m Model) bool {
	if f.Lab != "" && m.Lab != f.Lab || f.Family != "" && m.Family != f.Family {
		return false
	}
	return f.Provider == "" || slices.Contains(m.Providers, f.Provider)
}

// models serves the models the query selects.
func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	f := FilterOf(r)
	list := ModelList{Models: []Model{}}
	for m := range h.Catalog().All() {
		if f.Selects(m) {
			list.Models = append(list.Models, m)
		}
	}
	WriteJSON(w, http.StatusOK, list)
}

// model serves the model named in the path, with every provider's offering
// of it.
func (h *Handler) model(w http.ResponseWriter, r *http.Request) {
	c := h.Catalog()
	id := ModelID(r.PathValue("id"))
	m, ok := c.Model(id)
	if !ok {
		WriteError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("no model %q", id))
		return
	}
	d := ModelDetail{Model: m, Offerings: []ProviderOffering{}}
	for _, pid := range m.Providers {
		p := c.Providers[pid]
		d.Offerings = append(d.Offerings, ProviderOffering{Provider: pid, ProviderName: p.Name, Endpoint: p.Endpoint, Offering: p.Models[m.ID]})
	}
	WriteJSON(w, http.StatusOK, d)
}

// providers serves every provider, without its offerings.
func (h *Handler) providers(w http.ResponseWriter, _ *http.Request) {
	list := ProviderList{Providers: []ProviderSummary{}}
	for p := range h.Catalog().AllProviders() {
		s := ProviderSummary{Offerings: len(p.Models), Unlisted: len(p.Unlisted)}
		p.Models, p.Unlisted = nil, nil
		s.Provider = p
		list.Providers = append(list.Providers, s)
	}
	WriteJSON(w, http.StatusOK, list)
}

// provider serves the provider named in the path, with its offerings.
func (h *Handler) provider(w http.ResponseWriter, r *http.Request) {
	id := ProviderID(r.PathValue("id"))
	p, ok := h.Catalog().Providers[id]
	if !ok {
		WriteError(w, http.StatusNotFound, "provider_not_found", fmt.Sprintf("no provider %q", id))
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

// variant serves the variant named in the path, resolved.
func (h *Handler) variant(w http.ResponseWriter, r *http.Request) {
	s := h.cur.Load()
	p, id := ProviderID(r.PathValue("id")), r.PathValue("variant")
	if _, ok := s.cat.Providers[p]; !ok {
		WriteError(w, http.StatusNotFound, "provider_not_found", fmt.Sprintf("no provider %q", p))
		return
	}
	v, ok := s.variants[p][strings.ToLower(id)]
	if !ok {
		WriteError(w, http.StatusNotFound, "variant_not_found", fmt.Sprintf("no variant %q at %s", id, p))
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

// WriteJSON writes v as a JSON response with status code status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// ErrorBody is the body of an error response.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail says what went wrong: a code that programs can test, and a
// message for people.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes an error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}
