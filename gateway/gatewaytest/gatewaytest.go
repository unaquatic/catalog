// Package gatewaytest builds catalogs for tests: from a models.dev api.json
// document, the form in which flow's tests describe providers' models, and
// into a Store that serves them.
package gatewaytest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/modelsdev"
)

// npms are the npm packages of the providers whose documents name none.
var npms = map[modelsdev.ProviderID]string{
	"anthropic": "@ai-sdk/anthropic",
	"openai":    "@ai-sdk/openai",
}

// FromAPI returns the catalog of doc, a models.dev api.json document, as
// production builds it, with the mapping's own rules: each provider with
// the models it lists, each of which is the model of a lab of the
// provider's ID too, so that the provider's models are the catalog's,
// under IDs such as "openai/gpt-5.6", and not unlisted. A pro mode makes a
// model of its own, such as "openai/gpt-5.6-pro". A provider that
// names no npm package speaks its own: Anthropic Messages for anthropic,
// OpenAI Responses for openai, and OpenAI Chat Completions for any other,
// at a base URL that reaches nothing unless the document names one.
func FromAPI(t testing.TB, doc string) *gateway.Catalog {
	t.Helper()
	var api modelsdev.Catalog
	if err := json.Unmarshal([]byte(doc), &api); err != nil {
		t.Fatalf("gatewaytest: decode api.json: %v", err)
	}
	b := modelsdev.Bundle{Providers: api, Models: make(map[string]modelsdev.LabModel)}
	for pid, p := range api {
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
		for mid, m := range p.Models {
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var lm modelsdev.LabModel
			if err := json.Unmarshal(raw, &lm); err != nil {
				t.Fatal(err)
			}
			lm.ID = string(pid) + "/" + string(mid)
			b.Models[lm.ID] = lm
		}
	}
	c, _, err := gateway.FromModelsDev(b, gateway.Options{Source: "gatewaytest", Infer: true})
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
	if err := modelsdev.NewCacheFile(dir, gateway.StoreFile).Store(&modelsdev.Entry{Fetched: time.Now(), Data: data}); err != nil {
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
