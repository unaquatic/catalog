package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/unaquatic/catalog/gateway"
)

// page is the catalog browser that Server serves at /: one self-contained
// file, which reads catalog.json and lists the providers and the models,
// each provider's offerings of a model with their endpoints, tiers, modes
// and prices, and filters by lab, provider, family and option that the URL
// keeps.
//
//go:embed index.html
var page []byte

// SchemaPath is where Server serves gateway.SchemaDocument, beside the
// catalog at gateway.DocumentPath, as the site does.
const SchemaPath = "/schema.json"

// Server serves a catalog as the site does, and nothing else:
//
//	GET /              the catalog browser, page
//	GET /catalog.json  the catalog document, gateway.DocumentPath
//	GET /schema.json   its JSON Schema, gateway.SchemaDocument
//
// Set replaces the catalog while it serves.
type Server struct {
	cur atomic.Pointer[served]
	mux *http.ServeMux
}

// served is a catalog as a Server serves it: compact, as browsers fetch it
// whole, with its ETag.
type served struct {
	cat  *gateway.Catalog
	doc  []byte
	etag string
}

// NewServer returns a Server serving c.
func NewServer(c *gateway.Catalog) (*Server, error) {
	s := &Server{mux: http.NewServeMux()}
	if err := s.Set(c); err != nil {
		return nil, err
	}
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	s.mux.HandleFunc("GET "+gateway.DocumentPath, s.document)
	s.mux.HandleFunc("GET "+SchemaPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/schema+json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(gateway.SchemaDocument)
	})
	return s, nil
}

// Set makes s serve c from now on.
func (s *Server) Set(c *gateway.Catalog) error {
	doc, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode the catalog: %w", err)
	}
	sum := sha256.Sum256(doc)
	s.cur.Store(&served{cat: c, doc: doc, etag: `"` + hex.EncodeToString(sum[:16]) + `"`})
	return nil
}

// Catalog returns the catalog s serves.
func (s *Server) Catalog() *gateway.Catalog {
	return s.cur.Load().cat
}

// ServeHTTP serves the paths that Server lists, and 404 Not Found at any
// other.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// document serves the catalog document, answering a request that names its
// ETag with 304 Not Modified.
func (s *Server) document(w http.ResponseWriter, r *http.Request) {
	cur := s.cur.Load()
	w.Header().Set("ETag", cur.etag)
	if r.Header.Get("If-None-Match") == cur.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(cur.doc)
}
