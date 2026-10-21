package main

import (
	_ "embed"
	"net/http"

	"github.com/unaquatic/catalog/gateway"
)

// page is the catalog browser that Site serves at /: one self-contained
// file, which reads /catalog.json and lists the providers and the models,
// each provider's offerings of a model with their endpoints, tiers, modes
// and prices, and filters by lab, provider, family and option that the URL
// keeps.
//
//go:embed index.html
var page []byte

// Site returns the handler of the catalog server: the catalog browser at /,
// and the endpoints of h everywhere else.
func Site(h *gateway.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.Handle("/", h)
	return mux
}
