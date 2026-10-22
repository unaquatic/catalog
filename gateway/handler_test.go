package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// get requests path from srv and returns the response and its body.
func get(t *testing.T, srv *httptest.Server, path string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// decodeAs decodes body into a T.
func decodeAs[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

// newServer serves the fixture catalog.
func newServer(t *testing.T) (*httptest.Server, *Handler, *Catalog) {
	t.Helper()
	c, _ := groupFixture(t)
	h, err := NewHandler(c)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, h, c
}

func TestHandlerModels(t *testing.T) {
	srv, _, c := newServer(t)
	var all []ModelID
	for m := range c.All() {
		all = append(all, m.ID)
	}
	tests := []struct {
		query string
		want  []ModelID
	}{
		{"", all},
		{"?lab=openai", []ModelID{"openai/gpt-5.4-nano", "openai/gpt-5.6-sol", "openai/gpt-6-astra", "openai/gpt-6-astra-pro",
			"openai/gpt-6.1-sol", "openai/gpt-6.1-sol-pro", "openai/gpt-astra-fast-latest", "openai/gpt-astra-latest", "openai/o3"}},
		{"?provider=vercel", []ModelID{"anthropic/claude-opus-5-5", "bytedance-seed/seedance-2.0", "bytedance-seed/seedance-2.0-fast", "openai/gpt-6-astra", "openai/o3"}},
		{"?family=claude-opus&provider=github-copilot", []ModelID{"anthropic/claude-opus-5-5"}},
		{"?provider=opencode", []ModelID{"anthropic/claude-opus-4-8", "openai/gpt-5.6-sol", "opencode/big-pickle"}},
		{"?provider=nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			resp, body := get(t, srv, "/v1/models"+tt.query, nil)
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			var got []ModelID
			for _, m := range decodeAs[ModelList](t, body).Models {
				got = append(got, m.ID)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("models = %v, want %v", got, tt.want)
			}
		})
	}
}

// calls describes how off is called: "= ID" for its own call, "key = ID"
// for each variant, in order, its ID empty when it sends the call's, and
// "alias ID" for each alias.
func calls(off Offering) []string {
	out := []string{"= " + off.ProviderOptions.ID}
	for _, k := range off.VariantKeys() {
		out = append(out, k+" = "+off.Variants[k].ProviderOptions.ID)
	}
	for _, a := range off.Aliases {
		out = append(out, "alias "+a)
	}
	return out
}

func TestHandlerModel(t *testing.T) {
	srv, _, _ := newServer(t)
	resp, body := get(t, srv, "/v1/models/anthropic/claude-opus-5-5", nil)
	d := decodeAs[ModelDetail](t, body)
	var got []string
	for _, o := range d.Offerings {
		got = append(got, string(o.Provider)+" ("+o.ProviderName+") "+string(o.Endpoint.Format))
	}
	want := []string{"anthropic (Anthropic) anthropic-messages", "github-copilot (GitHub Copilot) openai-chat",
		"llmgateway (LLM Gateway) openai-chat", "openrouter (OpenRouter) openai-chat", "vercel (Vercel AI Gateway) openai-chat"}
	if resp.StatusCode != http.StatusOK || d.ID != "anthropic/claude-opus-5-5" || !slices.Equal(got, want) {
		t.Errorf("status %d, model %s, offerings %q; want %q", resp.StatusCode, d.ID, got, want)
	}
	// Vercel's "-fast" ID is the fast variant of its call.
	if got, want := calls(d.Offerings[4].Offering), []string{
		"= anthropic/claude-opus-5.5",
		"fast = anthropic/claude-opus-5.5-fast",
	}; !slices.Equal(got, want) {
		t.Errorf("vercel calls = %q, want %q", got, want)
	}
	// Copilot's call takes Anthropic's format at Copilot's URL.
	if ep := d.Offerings[1].ProviderOptions.Endpoint; ep == nil || ep.Format != FormatAnthropicMessages ||
		ep.API != "https://api.githubcopilot.com" || ep.Auth.Scheme != AuthBearer {
		t.Errorf("copilot endpoint = %+v, want anthropic-messages at Copilot's API, with a bearer token", ep)
	}

	// A "-latest" ID is no model of its own: models.dev's tie makes it an ID
	// of the model it serves.
	if resp, _ := get(t, srv, "/v1/models/anthropic/claude-opus-latest", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/v1/models/anthropic/claude-opus-latest = %d, want 404: no alias models", resp.StatusCode)
	}

	// The mapping derives the pro model; the API serves it as any other,
	// with no word of how the mapping made it or tied its offerings.
	_, body = get(t, srv, "/v1/models/openai/gpt-6-astra-pro", nil)
	pro := decodeAs[ModelDetail](t, body)
	if len(pro.Offerings) != 2 || pro.Offerings[0].Provider != "openai" {
		t.Fatalf("derived pro model = %+v", pro)
	}
	for _, key := range []string{`"derived"`, `"inferred"`, `"basis"`, `"models_dev"`} {
		if strings.Contains(string(body), key) {
			t.Errorf("derived pro model's JSON has %s: %s", key, body)
		}
	}
	if o := pro.Offerings[0]; o.ProviderOptions.ID != "gpt-6-astra" || o.ProviderOptions.Body["reasoning"] == nil || len(o.Variants) != 0 {
		t.Errorf("OpenAI's pro offering = %+v, want gpt-6-astra with the pro mode's fields", o)
	}

	resp, body = get(t, srv, "/v1/models/acme/open-1", nil)
	if d := decodeAs[ModelDetail](t, body); resp.StatusCode != http.StatusOK || d.Offerings == nil || len(d.Offerings) != 0 {
		t.Errorf("model without offerings: status %d, body %s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/v1/models/anthropic/nope", nil)
	if e := decodeAs[ErrorBody](t, body); resp.StatusCode != http.StatusNotFound || e.Error.Code != "model_not_found" {
		t.Errorf("unknown model: status %d, body %s", resp.StatusCode, body)
	}
}

func TestHandlerProviders(t *testing.T) {
	srv, _, _ := newServer(t)
	_, body := get(t, srv, "/v1/providers", nil)
	list := decodeAs[ProviderList](t, body)
	var ids []ProviderID
	for _, p := range list.Providers {
		ids = append(ids, p.ID)
		if p.Provider.Models != nil || p.Provider.Unlisted != nil {
			t.Errorf("provider %s lists its offerings in the summary", p.ID)
		}
	}
	if !slices.Equal(ids, fixtureProviders) {
		t.Errorf("providers = %v, want %v", ids, fixtureProviders)
	}
	if v := list.Providers[6]; v.ID != "vercel" || v.Offerings != 5 || v.Unlisted != 0 {
		t.Errorf("vercel summary = %+v, want 5 offerings, none unlisted", v)
	}

	resp, body := get(t, srv, "/v1/providers/opencode", nil)
	if p := decodeAs[Provider](t, body); resp.StatusCode != http.StatusOK || len(p.Models) != 3 || len(p.Unlisted) != 0 ||
		p.Endpoint.Format != FormatOpenAIChat || p.Models["opencode/big-pickle"].ProviderOptions.ID != "big-pickle" {
		t.Errorf("GET /v1/providers/opencode: status %d, body %s", resp.StatusCode, body)
	}
	// One variant, resolved: what to send, and where, from one response.
	resp, body = get(t, srv, "/v1/providers/vercel/openai/o3:fast", nil)
	if r := decodeAs[Resolved](t, body); resp.StatusCode != http.StatusOK || r.Provider != "vercel" || r.Model != "openai/o3" ||
		r.Tier != TierFast || r.ProviderOptions.ID != "openai/o3-fast" || r.ProviderOptions.Endpoint == nil ||
		!r.ProviderOptions.Endpoint.Equal(Endpoint{Format: FormatOpenAIChat, API: "https://ai-gateway.vercel.sh/v1", APIDefault: true,
			Auth: Auth{Scheme: AuthBearer}, Env: []string{"AI_GATEWAY_API_KEY"}}) {
		t.Errorf("GET a variant: status %d, body %s", resp.StatusCode, body)
	}
	for path, code := range map[string]string{
		"/v1/providers/nope":                   "provider_not_found",
		"/v1/providers/nope/openai/o3":         "provider_not_found",
		"/v1/providers/vercel/nope":            "variant_not_found",
		"/v1/providers/vercel/openai/o3:turbo": "variant_not_found",
		"/v2/elsewhere":                        "not_found",
	} {
		resp, body := get(t, srv, path, nil)
		if e := decodeAs[ErrorBody](t, body); resp.StatusCode != http.StatusNotFound || e.Error.Code != code {
			t.Errorf("GET %s: status %d, body %s; want 404 %s", path, resp.StatusCode, body, code)
		}
	}
}

func TestHandlerDocument(t *testing.T) {
	srv, h, c := newServer(t)
	resp, body := get(t, srv, "/catalog.json", nil)
	etag := resp.Header.Get("ETag")
	want, _ := json.Marshal(c)
	if resp.StatusCode != http.StatusOK || etag == "" || string(body) != string(want) {
		t.Fatalf("status %d, etag %q, body matches %v", resp.StatusCode, etag, string(body) == string(want))
	}

	resp, body = get(t, srv, "/catalog.json", http.Header{"If-None-Match": {etag}})
	if resp.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Errorf("conditional GET: status %d, %d bytes; want 304 and none", resp.StatusCode, len(body))
	}

	next := *c
	next.Source = "elsewhere"
	if err := h.Set(&next); err != nil {
		t.Fatal(err)
	}
	resp, body = get(t, srv, "/catalog.json", http.Header{"If-None-Match": {etag}})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") == etag || decodeAs[Catalog](t, body).Source != "elsewhere" {
		t.Errorf("after Set: status %d, etag %q; want the new catalog under a new ETag", resp.StatusCode, resp.Header.Get("ETag"))
	}
	if h.Catalog().Source != "elsewhere" {
		t.Errorf("Catalog().Source = %q, want elsewhere", h.Catalog().Source)
	}
}
