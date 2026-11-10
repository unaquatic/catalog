// Package gatewaytest builds catalogs for tests: from a models.dev api.json
// document, the form in which flow's tests describe providers' models, and
// into a Store that serves them.
package gatewaytest

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/build"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

// npms are the npm packages of the providers whose documents name none.
var npms = map[modelsdev.ProviderID]string{
	"anthropic": "@ai-sdk/anthropic",
	"openai":    "@ai-sdk/openai",
}

// FromAPI returns the catalog of doc, a models.dev api.json document, as
// production builds it, with the mapping's own rules: each provider with
// the models it lists, all of them the catalog's, and not unlisted.
//
// A model that names its lab's model in canonical_model_id, such as
// "lab-a/model-a", is that model of that lab, as a provider serving
// another lab's model is in models.dev; the lab offers it with the same
// modes, but is no provider of the catalog. A model that names none is the
// model of a lab of the provider's ID, under an ID such as
// "openai/gpt-5.6". A pro mode makes a model of its own, such as
// "openai/gpt-5.6-pro".
//
// A provider that names no npm package speaks its own: Anthropic Messages
// for anthropic, OpenAI Responses for openai, and OpenAI Chat Completions
// for any other, at a base URL that reaches nothing unless the document
// names one.
func FromAPI(t testing.TB, doc string) *gateway.Catalog {
	t.Helper()
	var api modelsdev.Catalog
	if err := json.Unmarshal([]byte(doc), &api); err != nil {
		t.Fatalf("gatewaytest: decode api.json: %v", err)
	}
	b := modelsdev.Bundle{Providers: api, Models: make(map[string]modelsdev.LabModel)}
	// The labs that are no provider of doc, with the models doc's
	// providers serve of theirs, which the build reads their modes from.
	labs := make(map[modelsdev.ProviderID]modelsdev.Provider)
	providers := slices.Sorted(maps.Keys(api))
	for _, pid := range providers {
		p := api[pid]
		p.ID = pid
		if p.NPM == "" {
			p.NPM = npms[pid]
		}
		if p.NPM == "" {
			p.NPM = "@ai-sdk/openai-compatible"
			if p.API == "" {
				p.API = "https://" + string(pid) + ".invalid/v1"
			}
		}
		api[pid] = p
		for _, mid := range slices.Sorted(maps.Keys(p.Models)) {
			m := p.Models[mid]
			id := cmp.Or(m.CanonicalModelID, string(pid)+"/"+string(mid))
			if _, ok := b.Models[id]; ok {
				continue
			}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var lm modelsdev.LabModel
			if err := json.Unmarshal(raw, &lm); err != nil {
				t.Fatal(err)
			}
			lm.ID = id
			b.Models[id] = lm
			lab, tail, ok := strings.Cut(id, "/")
			if !ok {
				t.Fatalf("gatewaytest: canonical_model_id %q of %s/%s names no lab", id, pid, mid)
			}
			if _, ok := api[modelsdev.ProviderID(lab)]; ok {
				continue
			}
			l := labs[modelsdev.ProviderID(lab)]
			if l.Models == nil {
				l = modelsdev.Provider{ID: modelsdev.ProviderID(lab), Models: make(map[modelsdev.ModelID]modelsdev.Model)}
			}
			m.ID, m.CanonicalModelID = modelsdev.ModelID(tail), ""
			l.Models[m.ID] = m
			labs[l.ID] = l
		}
	}
	maps.Copy(api, labs)
	opts := build.Options{Source: "gatewaytest", Infer: true}
	for _, pid := range providers {
		opts.Providers = append(opts.Providers, gateway.ProviderID(pid))
	}
	c, _, err := build.FromModelsDev(b, opts)
	if err != nil {
		t.Fatalf("gatewaytest: build the catalog: %v", err)
	}
	return c
}

// WriteCache writes c into dir as a Store's cached download, fetched now.
func WriteCache(t testing.TB, dir string, c *gateway.Catalog) {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	WriteEntry(t, dir, Entry{Data: data, Fetched: time.Now()})
}

// Entry is a Store's cached download: the catalog document as the server
// sent it, and when it was fetched.
type Entry struct {
	Data    []byte
	Fetched time.Time
}

// WriteEntry writes e into dir as a Store's cached download, as a test of a
// fresh or a stale cache needs it.
func WriteEntry(t testing.TB, dir string, e Entry) {
	t.Helper()
	if err := modelsdev.NewCacheFile(dir, gateway.StoreFile).Store(&modelsdev.Entry{Data: e.Data, Fetched: e.Fetched}); err != nil {
		t.Fatal(err)
	}
}

// Store returns a Store over a new temporary directory, which holds c as its
// cached download, and the directory.
func Store(t testing.TB, c *gateway.Catalog) (*gateway.Store, string) {
	t.Helper()
	dir := t.TempDir()
	WriteCache(t, dir, c)
	return gateway.NewStore(gateway.StoreOptions{Dir: dir}), dir
}

// StoreAPI returns Store(t, FromAPI(t, doc)).
func StoreAPI(t testing.TB, doc string) (*gateway.Store, string) {
	t.Helper()
	return Store(t, FromAPI(t, doc))
}
