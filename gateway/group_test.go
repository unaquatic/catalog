package gateway

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/unaquatic/catalog/modelsdev"
)

// groupEndpoints maps testdata/endpoints.json to the catalog.
func groupEndpoints(t *testing.T) (*Catalog, Report) {
	t.Helper()
	data, err := os.ReadFile("testdata/endpoints.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := modelsdev.DecodeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c, rep
}

// mustOffering returns provider p's offering of model m in c.
func mustOffering(t *testing.T, c *Catalog, p ProviderID, m ModelID) Offering {
	t.Helper()
	o, ok := c.Offering(p, m)
	if !ok {
		t.Fatalf("%s has no offering of %s; it has %v", p, m, slices.Sorted(maps.Keys(c.Providers[p].Models)))
	}
	return o
}

func TestGroupCalls(t *testing.T) {
	c, rep := groupFixture(t)
	tests := []struct {
		provider ProviderID
		model    ModelID
		want     []string // as calls describes them
	}{
		// Tiers by request fields.
		{"openai", "openai/gpt-6.1-sol", []string{"= gpt-6.1-sol", "fast = ", "ultrafast = "}},
		// A pro model calls its base's ID with the pro mode's fields.
		{"openai", "openai/gpt-6.1-sol-pro", []string{"= gpt-6.1-sol"}},
		// The ID that names the model is the call; another is an alias.
		{"openai", "openai/gpt-5.6-sol", []string{"= gpt-5.6-sol", "alias gpt-5.6"}},
		// A tier by its own ID wins over request fields of the same tier;
		// another tier by request fields stays.
		{"vercel", "openai/o3", []string{"= openai/o3", "flex = ", "fast = openai/o3-fast"}},
		// A merged model's listing is a tier of the model's.
		{"vercel", "openai/gpt-6-astra", []string{"= openai/gpt-6-astra", "fast = openai/gpt-6-astra-fast"}},
		// ":free" is a tier of its own ID.
		{"openrouter", "anthropic/claude-opus-4-8", []string{"= anthropic/claude-opus-4.8", "free = anthropic/claude-opus-4.8:free"}},
		// A "-latest" ID no rule ties is a model of its own, and so is one
		// that names a tier of it: no alias models.
		{"llmgateway", "openai/gpt-astra-latest", []string{"= accounts/acme/routers/gpt-astra-latest"}},
		{"llmgateway", "openai/gpt-astra-fast-latest", []string{"= gpt-astra-fast-latest"}},
	}
	for _, tt := range tests {
		if got := calls(mustOffering(t, c, tt.provider, tt.model)); !slices.Equal(got, tt.want) {
			t.Errorf("%s %s calls = %q, want %q", tt.provider, tt.model, got, tt.want)
		}
	}
	pro := mustOffering(t, c, "openai", "openai/gpt-6.1-sol-pro")
	if !reflect.DeepEqual(pro.ProviderOptions.Body, map[string]any{"reasoning": map[string]any{"mode": "pro"}}) ||
		pro.Name != "GPT-6.1 Sol Pro" || pro.Limit.Context == 0 {
		t.Errorf("pro offering = %+v, want the pro mode's fields, the pro model's name, and the base's facts", pro)
	}
	if v := mustOffering(t, c, "vercel", "openai/o3").Variants["fast"]; v.Tier != TierFast || v.Price.Input != 3.5 {
		t.Errorf("o3 fast = %+v, want the -fast listing's price", v)
	}
	// A provider's own model, which names no maker, is a model of the
	// provider's, not unlisted.
	if u := c.Providers["opencode"].Unlisted; len(u) != 0 || mustOffering(t, c, "opencode", "opencode/big-pickle").ProviderOptions.ID != "big-pickle" {
		t.Errorf("opencode unlisted = %+v, want big-pickle as opencode/big-pickle", u)
	}
	// A listing no rule ties is left out, and listed in the report; without
	// the rules, it is unlisted, under its own ID.
	if u := c.Providers["openrouter"].Unlisted; len(u) != 0 ||
		!slices.Contains(rep.Unknown, Note{Provider: "openrouter", Model: "~google/gemini-pro-latest", Text: "no model or lab known"}) {
		t.Errorf("openrouter unlisted = %+v, unknown %v; want ~google/gemini-pro-latest left out", u, rep.Unknown)
	}
	plain, _, err := FromModelsDev(loadFixture(t), fixtureOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if u := plain.Providers["openrouter"].Unlisted; u["google/gemini-pro-latest"].ProviderOptions.ID != "~google/gemini-pro-latest" {
		t.Errorf("openrouter unlisted without the rules = %+v", u)
	}
	// Every model lists the providers that offer it.
	if got := mustModel(t, c.Models, "openai/gpt-6-astra-pro").Providers; !slices.Equal(got, []ProviderID{"openai", "openrouter"}) {
		t.Errorf("astra pro providers = %v", got)
	}
	if got := mustModel(t, c.Models, "acme/open-1").Providers; got == nil || len(got) != 0 {
		t.Errorf("acme providers = %#v, want empty, not nil", got)
	}
	// gpt-5.4-nano's "turbo" mode is no tier or mode the catalog lists.
	if _, ok := c.Resolve("openai", "openai/gpt-5.4-nano:turbo"); ok {
		t.Error("the turbo mode is a variant")
	}
	if !slices.ContainsFunc(rep.Notes, func(n Note) bool { return strings.Contains(n.Text, `mode "turbo"`) }) {
		t.Errorf("no note of the turbo mode left out in %v", rep.Notes)
	}
	if rep.Offerings != 30 || rep.Dropped != 0 {
		t.Errorf("offerings %d, dropped %d; want 30, 0", rep.Offerings, rep.Dropped)
	}
	// Variant IDs are unique at each provider.
	for p := range c.Providers {
		seen := make(map[string]bool)
		for r := range c.Variants(p) {
			if seen[r.ID] {
				t.Errorf("%s lists variant %s twice", p, r.ID)
			}
			seen[r.ID] = true
		}
	}
}

// TestVariantVocabulary checks that every variant the fixtures make is a
// tier and a mode the catalog lists, under the key they make: a tier or
// mode that slips past the list fails here.
func TestVariantVocabulary(t *testing.T) {
	f, _ := groupFixture(t)
	e, _ := groupEndpoints(t)
	for _, c := range []*Catalog{f, e} {
		for _, p := range c.Providers {
			for _, off := range slices.Concat(slices.Collect(maps.Values(p.Models)), slices.Collect(maps.Values(p.Unlisted))) {
				for k, v := range off.Variants {
					if !known(v.Tier, v.Mode) || v.Tier == "" && v.Mode == "" || k != v.Key() {
						t.Errorf("%s: variant %q is tier %q, mode %q", p.ID, k, v.Tier, v.Mode)
					}
				}
			}
		}
	}
}

func TestGroupBedrock(t *testing.T) {
	c, rep := groupEndpoints(t)
	// One region: global first. Its ultrafast tier keeps Converse's
	// wrapper; the regional Mantle listing is dropped.
	o := mustOffering(t, c, "amazon-bedrock", "openai/gpt-6.1-sol")
	if got, want := calls(o), []string{"= global.openai.gpt-6.1-sol", "ultrafast = "}; !slices.Equal(got, want) {
		t.Errorf("bedrock calls = %q, want %q", got, want)
	}
	if o.Price.Input != 2 || o.ProviderOptions.Endpoint != nil {
		t.Errorf("bedrock call = %+v, want global's $2 at the provider's endpoint", o)
	}
	if rep.Dropped != 2 {
		t.Errorf("dropped %d, want 2: us. and the regional Mantle ID", rep.Dropped)
	}
}

// aliasDoc is a models.dev catalog of one model at a gateway that lists it
// under other names: a coding plan's, which is never the call, a dated
// snapshot, a free tier by a "-free" suffix, a thinking version with a
// fast tier of its own, and an upstream at another endpoint.
const aliasDoc = `{
"models": {"lab/m-1": {"id": "lab/m-1", "name": "M 1"}},
"providers": {"gw": {"id": "gw", "name": "GW", "npm": "@ai-sdk/openai-compatible", "api": "https://gw.invalid/v1", "models": {
  "coding-m-1":     {"id": "coding-m-1", "canonical_model_id": "lab/m-1", "cost": {"input": 0.1, "output": 1}},
  "m-1":            {"id": "m-1", "canonical_model_id": "lab/m-1", "cost": {"input": 1, "output": 2}},
  "m-1-2026-01-01": {"id": "m-1-2026-01-01", "canonical_model_id": "lab/m-1", "cost": {"input": 1, "output": 2}},
  "m-1-free":       {"id": "m-1-free", "canonical_model_id": "lab/m-1", "cost": {"input": 0, "output": 0}},
  "m-1-thinking":   {"id": "m-1-thinking", "canonical_model_id": "lab/m-1", "cost": {"input": 1, "output": 3}},
  "m-1-thinking-fast": {"id": "m-1-thinking-fast", "canonical_model_id": "lab/m-1", "cost": {"input": 2, "output": 6}},
  "other/m-1":      {"id": "other/m-1", "canonical_model_id": "lab/m-1", "provider": {"npm": "@ai-sdk/anthropic", "api": "https://gw.invalid/anthropic"}}
}}}}`

// TestGroupAliases checks which listing is the call, which are variants,
// with a combination of a mode and a tier, and which are aliases.
func TestGroupAliases(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(aliasDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	o := mustOffering(t, c, "gw", "lab/m-1")
	want := []string{"= m-1", "free = m-1-free", "thinking = m-1-thinking", "thinking:fast = m-1-thinking-fast",
		"alias coding-m-1", "alias m-1-2026-01-01"}
	if got := calls(o); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if v := o.Variants["thinking:fast"]; v.Mode != ModeThinking || v.Tier != TierFast || v.Price.Input != 2 {
		t.Errorf("thinking:fast = %+v", v)
	}
	if !slices.ContainsFunc(rep.Notes, func(n Note) bool { return strings.Contains(n.Text, "another endpoint") }) {
		t.Errorf("no note of other/m-1, at another endpoint, in %v", rep.Notes)
	}
	r, ok := c.Resolve("gw", "lab/m-1:thinking:fast")
	if !ok || r.ProviderOptions.ID != "m-1-thinking-fast" || r.Mode != ModeThinking || r.Tier != TierFast || r.ProviderOptions.Endpoint == nil {
		t.Errorf("Resolve(thinking:fast) = %+v, %v", r, ok)
	}
}

func TestVariantKeys(t *testing.T) {
	off := Offering{Variants: map[string]Variant{
		"thinking:fast": {Mode: ModeThinking, Tier: TierFast},
		"free":          {Tier: TierFree},
		"thinking":      {Mode: ModeThinking},
		"fast":          {Tier: TierFast},
		"flex":          {Tier: TierFlex},
	}}
	if got, want := off.VariantKeys(), []string{"flex", "fast", "free", "thinking", "thinking:fast"}; !slices.Equal(got, want) {
		t.Errorf("VariantKeys = %q, want %q", got, want)
	}
	if VariantKey(ModeThinking, TierFast) != "thinking:fast" || VariantKey("", TierFast) != "fast" || VariantID("lab/m", "") != "lab/m" {
		t.Error("VariantKey or VariantID")
	}
}

// TestCatalogLookup checks that a variant ID finds its offering and variant, with
// keys of one and two segments, and an unlisted offering whose own ID holds
// colons.
func TestCatalogLookup(t *testing.T) {
	c := &Catalog{Providers: map[ProviderID]Provider{"p": {
		Models: map[ModelID]Offering{"lab/m": {Variants: map[string]Variant{
			"fast": {Tier: TierFast}, "thinking:fast": {Mode: ModeThinking, Tier: TierFast}}}},
		Unlisted: map[string]Offering{"ft:m:org": {Variants: map[string]Variant{"fast": {Tier: TierFast}}}},
	}}}
	tests := []struct {
		id, offering, variant string
		unlisted, ok          bool
	}{
		{"lab/m", "lab/m", "", false, true},
		{"lab/m:fast", "lab/m", "fast", false, true},
		{"lab/m:thinking:fast", "lab/m", "thinking:fast", false, true},
		{"lab/m:turbo", "", "", false, false},
		{"ft:m:org", "ft:m:org", "", true, true},
		{"ft:m:org:fast", "ft:m:org", "fast", true, true},
		{"nope", "", "", false, false},
	}
	for _, tt := range tests {
		off, unlisted, v, ok := c.Lookup("p", tt.id)
		if off != tt.offering || unlisted != tt.unlisted || v != tt.variant || ok != tt.ok {
			t.Errorf("Lookup(%s) = %q, %v, %q, %v", tt.id, off, unlisted, v, ok)
		}
	}
}

func TestResolve(t *testing.T) {
	c, _ := groupEndpoints(t)
	r, ok := c.Resolve("amazon-bedrock", "openai/gpt-6.1-sol:ultrafast")
	want := map[string]any{"additionalModelRequestFields": map[string]any{"service_tier": "ultrafast"}}
	if !ok || r.Model != "openai/gpt-6.1-sol" || r.Tier != TierUltrafast || r.ProviderOptions.ID != "global.openai.gpt-6.1-sol" ||
		!reflect.DeepEqual(r.ProviderOptions.Body, want) || r.ProviderOptions.Endpoint == nil ||
		r.ProviderOptions.Endpoint.Format != FormatBedrockConverse || r.Price.Input != 12 {
		t.Errorf("Resolve = %+v, %v; want global's ID, the tier's fields and price, Bedrock's endpoint", r, ok)
	}
	if _, ok := c.Resolve("amazon-bedrock", "openai/gpt-6.1-sol:fast"); ok {
		t.Error("Resolve found a tier Bedrock does not offer")
	}

	f, _ := groupFixture(t)
	r, ok = f.Resolve("vercel", "openai/o3:fast")
	if !ok || r.ProviderOptions.ID != "openai/o3-fast" || r.ProviderOptions.Body != nil || r.Name == "" {
		t.Errorf("vercel o3 fast = %+v, %v; want its own ID", r, ok)
	}
	// A variant by request fields sends the call's ID, at the call's price
	// when it lists none of its own.
	r, ok = f.Resolve("openai", "openai/gpt-6.1-sol:fast")
	if !ok || r.ProviderOptions.ID != "gpt-6.1-sol" || r.ProviderOptions.Body["service_tier"] != "priority" || r.Price == nil {
		t.Errorf("gpt-6.1-sol fast = %+v, %v", r, ok)
	}
	r, ok = f.Resolve("opencode", "opencode/big-pickle")
	if !ok || r.Model != "opencode/big-pickle" || r.ProviderOptions.ID != "big-pickle" || r.ProviderOptions.Endpoint == nil {
		t.Errorf("opencode's own big-pickle = %+v, %v", r, ok)
	}
	// An unlisted offering, as a catalog without the rules has them.
	plain, _, err := FromModelsDev(loadFixture(t), fixtureOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	r, ok = plain.Resolve("openrouter", "google/gemini-pro-latest")
	if !ok || r.Model != "" || r.ID != "google/gemini-pro-latest" || r.ProviderOptions.ID != "~google/gemini-pro-latest" || r.ProviderOptions.Endpoint == nil {
		t.Errorf("unlisted gemini-pro-latest = %+v, %v", r, ok)
	}
}

func TestMergeBody(t *testing.T) {
	base := map[string]any{"additionalModelRequestFields": map[string]any{"reasoning": map[string]any{"effort": "high"}}, "a": 1.0}
	over := map[string]any{"additionalModelRequestFields": map[string]any{"service_tier": "ultrafast"}, "a": 2.0}
	got := MergeBody(base, over)
	want := map[string]any{"additionalModelRequestFields": map[string]any{"reasoning": map[string]any{"effort": "high"},
		"service_tier": "ultrafast"}, "a": 2.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeBody = %v, want %v", got, want)
	}
	if base["a"] != 1.0 || len(base["additionalModelRequestFields"].(map[string]any)) != 1 {
		t.Errorf("MergeBody changed its base: %v", base)
	}
	if MergeBody(nil, nil) != nil {
		t.Error("MergeBody(nil, nil) is not nil")
	}
}

// caseDoc is a models.dev catalog whose lab writes its model IDs with
// capitals, as MiniMax does, and whose gateway serves one tied by
// canonical_model_id, and another provider-specific one, each with capitals.
const caseDoc = `{
"models": {"minimax/MiniMax-M2.7": {"id": "minimax/MiniMax-M2.7", "name": "MiniMax M2.7"}},
"providers": {
  "minimax": {"id": "minimax", "name": "MiniMax", "npm": "@ai-sdk/anthropic", "api": "https://mm.invalid/v1", "models": {
    "MiniMax-M2.7": {"id": "MiniMax-M2.7"}}},
  "gw": {"id": "gw", "name": "GW", "npm": "@ai-sdk/openai-compatible", "api": "https://gw.invalid/v1", "models": {
    "MiniMaxAI/MiniMax-M2.7": {"id": "MiniMaxAI/MiniMax-M2.7", "canonical_model_id": "minimax/MiniMax-M2.7"},
    "Org/Special-Model": {"id": "Org/Special-Model"}}}}}`

// TestLowerCaseIDs checks that the catalog's IDs are lower case, models.dev's
// capitals aside, and the provider's model IDs to send as they are, and that
// lookups ignore case.
func TestLowerCaseIDs(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(caseDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := FromModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := c.Models["minimax/minimax-m2.7"]; !ok || m.ID != "minimax/minimax-m2.7" ||
		!slices.Equal(m.Providers, []ProviderID{"gw", "minimax"}) {
		t.Errorf("models = %v, want minimax/minimax-m2.7 at gw and minimax", slices.Collect(maps.Keys(c.Models)))
	}
	if o := mustOffering(t, c, "minimax", "minimax/minimax-m2.7"); o.ProviderOptions.ID != "MiniMax-M2.7" || o.Basis != BasisLab {
		t.Errorf("lab offering = %+v, want its own ID as it is, tied as the lab's", o)
	}
	if o := mustOffering(t, c, "gw", "minimax/minimax-m2.7"); o.ProviderOptions.ID != "MiniMaxAI/MiniMax-M2.7" || o.ModelsDev != "minimax/minimax-m2.7" {
		t.Errorf("gateway offering = %+v", o)
	}
	if o, ok := c.Providers["gw"].Unlisted["org/special-model"]; !ok || o.ProviderOptions.ID != "Org/Special-Model" {
		t.Errorf("unlisted = %v, want org/special-model sending Org/Special-Model", c.Providers["gw"].Unlisted)
	}
	for _, id := range []string{"minimax/MiniMax-M2.7", "MINIMAX/minimax-m2.7"} {
		if r, ok := c.Resolve("gw", id); !ok || r.ID != "minimax/minimax-m2.7" || r.ProviderOptions.ID != "MiniMaxAI/MiniMax-M2.7" {
			t.Errorf("Resolve(%s) = %+v, %v", id, r, ok)
		}
	}
	if _, ok := c.Model("minimax/MiniMax-M2.7"); !ok {
		t.Error("Model ignores no case")
	}
}

func TestCatalogID(t *testing.T) {
	for in, want := range map[string]string{
		"minimax/MiniMax-M2.7":          "minimax/minimax-m2.7",
		"cohere/north-mini-code:free":   "cohere/north-mini-code-free",
		"Qwen3_5-9B-MLX-4bit":           "qwen3-5-9b-mlx-4bit",
		"@cf/cloudflare/clef":           "cf/cloudflare/clef",
		"~deepseek/deepseek-pro-latest": "deepseek/deepseek-pro-latest",
		"leanstral-1-5@eu":              "leanstral-1-5-eu",
		"a//b/":                         "a/b",
		"gemma-4-E4B-it-IQ4_XS":         "gemma-4-e4b-it-iq4-xs",
	} {
		if got := CatalogID(in); got != want {
			t.Errorf("CatalogID(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNameOf checks that a model's name is made of its ID's words alone,
// spelt as most names spell them.
func TestNameOf(t *testing.T) {
	sp := SpellingsOf(slices.Values([]string{"GPT-4o", "gpt-oss-120b", "GPT OSS 20B", "GPT-OSS", "Gemma 3 27B IT",
		"Gemma-2-9B-IT", "gemma 2 it", "Qwen2.5-VL 72B", "Qwen3.5 122B-A10B", "MiniMax-M2.5", "DeepSeek V3",
		"ALLaM-2-7b", "MiMo-V2.5-Pro-UltraSpeed", "Nemotron Nano 9B v2", "Solar Pro 3", "Solar Pro 3", "Qwen 3 14b"}))
	for _, tt := range []struct {
		id   ModelID
		want string
	}{
		{"openai/gpt-6.1-sol", "GPT-6.1 Sol"},
		{"openai/gpt-oss-120b", "GPT OSS 120B"},
		{"openai/gpt-4o-2024-05-13", "GPT-4o 20240513"},
		{"openai/o3-mini", "o3 Mini"},
		{"openai/gpt-4.1-mini", "GPT-4.1 Mini"},
		{"anthropic/claude-opus-4-5", "Claude Opus 4.5"},
		{"anthropic/claude-opus-4-0", "Claude Opus 4.0"},
		{"anthropic/claude-3-5-sonnet-20241022", "Claude 3.5 Sonnet 20241022"},
		{"alibaba/qwen2-5-vl-72b-instruct", "Qwen2.5 VL 72B Instruct"},
		{"alibaba/qwen3.5-122b-a10b", "Qwen3.5 122B A10B"},
		{"minimax/minimax-m2.5-highspeed", "MiniMax M2.5 Highspeed"},
		{"mistral/codestral-22b-v0.1", "Codestral 22B V0.1"},
		{"nvidia/nemotron-nano-9b-v2", "Nemotron Nano 9B V2"},
		{"deepseek/deepseek-chat-v3-0324", "DeepSeek Chat V3 0324"},
		{"xiaomi/mimo-v2.5-pro-ultraspeed", "MiMo V2.5 Pro UltraSpeed"},
		{"sdaia/allam-2-7b", "ALLaM 2 7B"},
		{"google/gemma-2-27b-it", "Gemma 2 27B IT"},
		{"google/gemini-2.5-flash-preview-05-20", "Gemini 2.5 Flash Preview 05 20"},
		{"moonshotai/kimi-k2-0905", "Kimi K2 0905"},
		{"typesafe/jev-latest", "Jev Latest"},
		// A word and the number after it, as names write them apart, spell
		// the word an ID runs them together in; a word names write joined
		// more often stays joined.
		{"upstage/solar-pro3", "Solar Pro 3"},
		{"alibaba/qwen3.5-122b-a10b", "Qwen3.5 122B A10B"},
	} {
		if got := NameOf(tt.id, sp); got != tt.want {
			t.Errorf("NameOf(%s) = %q, want %q", tt.id, got, tt.want)
		}
	}
	got := sp.For(slices.Values([]string{"minimax-m2.5-highspeed", "o3-mini", "nemotron-nano-9b-v2"}))
	if want := (Spellings{"minimax": "MiniMax"}); !maps.Equal(got, want) {
		t.Errorf("For = %v, want only the spellings Spell needs, %v", got, want)
	}
	c := &Catalog{Spellings: got}
	if n := c.NameOf("MiniMax/MiniMax-M2.5"); n != "MiniMax M2.5" {
		t.Errorf("Catalog.NameOf(MiniMax/MiniMax-M2.5) = %q, want MiniMax M2.5", n)
	}
	for key, want := range map[string]string{"meta-llama/llama-4-scout": "Llama 4 Scout", "minimax-m2.5": "MiniMax M2.5",
		"accounts/fireworks/models/qwen3-8b": "Qwen3 8B"} {
		if n := c.UnlistedName(key); n != want {
			t.Errorf("UnlistedName(%s) = %q, want %q", key, n, want)
		}
	}
	for key, want := range map[string]string{"ultrafast": "GPT-6.1 Sol Ultrafast", "fast": "GPT-6.1 Sol Fast", "thinking:fast": "GPT-6.1 Sol Thinking Fast"} {
		if n := VariantName("GPT-6.1 Sol", key); n != want {
			t.Errorf("VariantName(GPT-6.1 Sol, %s) = %q, want %q", key, n, want)
		}
	}
}

// TestNamesMatchIDs checks that every model the catalog lists, and every
// offering and variant, is named from its ID with the catalog's own
// spellings, so that anyone with the catalog can name an ID as it does; that
// the provider's names are in ProviderOptions; and that the spellings hold
// only what the names need.
func TestNamesMatchIDs(t *testing.T) {
	c, _ := groupFixture(t)
	for name, c := range map[string]*Catalog{"catalog": c, "kept": c.Keep("openai")} {
		for id, m := range c.Models {
			if want := c.NameOf(id); m.Name != want {
				t.Errorf("%s: %s is named %q, want %q", name, id, m.Name, want)
			}
		}
		check := func(where, want string, o Offering) {
			if o.Name != want || o.ProviderOptions.Name == "" {
				t.Errorf("%s: %s is named %q, the provider's %q; want %q, and the provider's name", name, where, o.Name, o.ProviderOptions.Name, want)
			}
			for k, v := range o.Variants {
				if v.Name != VariantName(o.Name, k) {
					t.Errorf("%s: %s:%s is named %q, want %q", name, where, k, v.Name, VariantName(o.Name, k))
				}
			}
		}
		for pid, p := range c.Providers {
			for id, o := range p.Models {
				check(string(pid)+"/"+string(id), c.Models[id].Name, o)
			}
			for key, o := range p.Unlisted {
				check(string(pid)+"/"+key+" (unlisted)", c.UnlistedName(key), o)
			}
		}
		if extra := maps.Collect(func(yield func(string, string) bool) {
			used := c.Spellings.For(c.NameParts())
			for w, s := range c.Spellings {
				if _, ok := used[w]; !ok && !yield(w, s) {
					return
				}
			}
		}); len(extra) > 0 {
			t.Errorf("%s: spellings no model needs: %v", name, extra)
		}
	}
}

// unlistedDoc is a models.dev catalog of a gateway's provider-specific
// models: one with a free tier, a thinking version and an EU region, one
// whose ID holds characters the catalog does not write, and two whose IDs
// the catalog writes alike.
const unlistedDoc = `{"models": {}, "providers": {"gw": {"id": "gw", "name": "GW", "npm": "@ai-sdk/openai-compatible", "api": "https://gw.invalid/v1", "models": {
  "Vendor/Model-X":          {"id": "Vendor/Model-X", "cost": {"input": 1, "output": 2}},
  "Vendor/Model-X:free":     {"id": "Vendor/Model-X:free", "cost": {"input": 0, "output": 0}},
  "Vendor/Model-X:thinking": {"id": "Vendor/Model-X:thinking", "cost": {"input": 1, "output": 2}},
  "Vendor/Model-X@eu":       {"id": "Vendor/Model-X@eu", "cost": {"input": 1.1, "output": 2.2}},
  "@cf/odd_model":           {"id": "@cf/odd_model"},
  "same_id":                 {"id": "same_id"},
  "same-id":                 {"id": "same-id"}
}}}}`

// TestGroupUnlisted checks that unlisted models group as listed ones do,
// under keys the catalog writes, sending the provider's own IDs.
func TestGroupUnlisted(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(unlistedDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	u := c.Providers["gw"].Unlisted
	if got, want := slices.Sorted(maps.Keys(u)), []string{"cf/odd-model", "same-id", "same-id-2", "vendor/model-x"}; !slices.Equal(got, want) {
		t.Fatalf("unlisted keys = %q, want %q", got, want)
	}
	if got, want := calls(u["vendor/model-x"]), []string{"= Vendor/Model-X", "free = Vendor/Model-X:free",
		"thinking = Vendor/Model-X:thinking", "alias Vendor/Model-X@eu"}; !slices.Equal(got, want) {
		t.Errorf("vendor/model-x calls = %q, want %q", got, want)
	}
	if o := u["cf/odd-model"]; o.ProviderOptions.ID != "@cf/odd_model" {
		t.Errorf("cf/odd-model sends %q, want the provider's own ID", o.ProviderOptions.ID)
	}
	if !slices.ContainsFunc(rep.Notes, func(n Note) bool { return strings.Contains(n.Text, "keyed same-id-2") }) {
		t.Errorf("no note of the shared key in %v", rep.Notes)
	}
	if r, ok := c.Resolve("gw", "vendor/model-x:free"); !ok || r.Model != "" || r.ProviderOptions.ID != "Vendor/Model-X:free" {
		t.Errorf("Resolve(vendor/model-x:free) = %+v, %v", r, ok)
	}
}

// sharedDoc is a models.dev catalog in which three providers list a Qwen
// model models.dev does not, under IDs that name it alike, one with a free
// tier; one provider lists a Llama model alone, under its maker, another a
// Grok model by its brand alone, fine-tunes of Qwen and Llama, a Poe bot
// named after Claude, and models of makers models.dev lists no lab of but
// the rules add; Alibaba lists a model of its own; one lists a router and
// a model of a maker without a lab; and Databricks lists a model models.dev
// does under an ID of its own, as another lists a Llama model under the
// lab's name twice.
const sharedDoc = `{
"models": {
  "alibaba/qwen3-max": {"id": "alibaba/qwen3-max", "name": "Qwen3 Max"},
  "meta/llama-4": {"id": "meta/llama-4", "name": "Llama 4"},
  "meta/llama-3.3-70b-instruct": {"id": "meta/llama-3.3-70b-instruct", "name": "Llama 3.3 70B Instruct"},
  "mistral/mistral-small": {"id": "mistral/mistral-small", "name": "Mistral Small"},
  "anthropic/claude-opus-5": {"id": "anthropic/claude-opus-5", "name": "Claude Opus 5"},
  "xai/grok-4": {"id": "xai/grok-4", "name": "Grok 4"},
  "openai/gpt-oss-120b": {"id": "openai/gpt-oss-120b", "name": "GPT OSS 120B"}},
"providers": {
  "alibaba": {"id": "alibaba", "name": "Alibaba", "npm": "@ai-sdk/openai-compatible", "api": "https://ali.invalid/v1", "models": {
    "qwen3-max": {"id": "qwen3-max"},
    "qwen-mt-turbo": {"id": "qwen-mt-turbo", "name": "Qwen MT Turbo"},
    "qwen3-14b": {"id": "qwen3-14b", "name": "Qwen3 14B", "release_date": "2025-04-29", "limit": {"context": 131072, "output": 8192}}}},
  "hf": {"id": "hf", "name": "HF", "npm": "@ai-sdk/openai-compatible", "api": "https://hf.invalid/v1", "models": {
    "Qwen/Qwen3-14B": {"id": "Qwen/Qwen3-14B", "name": "Qwen3-14B", "release_date": "2025-04-29", "limit": {"context": 40960, "output": 8192}},
    "Qwen/Qwen3-14B:free": {"id": "Qwen/Qwen3-14B:free", "name": "Qwen3-14B (free)", "cost": {"input": 0, "output": 0}},
    "openrouter/auto": {"id": "openrouter/auto", "name": "Auto Router"},
    "acme-tools/rerank-3": {"id": "acme-tools/rerank-3", "name": "Rerank 3"}}},
  "gw": {"id": "gw", "name": "GW", "npm": "@ai-sdk/openai-compatible", "api": "https://gw.invalid/v1", "models": {
    "qwen/qwen3-14b": {"id": "qwen/qwen3-14b", "name": "Qwen: Qwen3 14B", "release_date": "2025-04-29", "limit": {"context": 40960, "output": 8192}},
    "meta-llama/llama-4-scout": {"id": "meta-llama/llama-4-scout", "name": "Llama 4 Scout"},
    "meta-llama/Meta-Llama-4": {"id": "meta-llama/Meta-Llama-4", "name": "Meta Llama 4"},
    "grok-3": {"id": "grok-3", "name": "Grok 3"},
    "qwen/qwen3-14b-uncensored": {"id": "qwen/qwen3-14b-uncensored", "name": "Qwen3 14B Uncensored"},
    "huihui-ai/Llama-3.3-70B-Instruct-abliterated": {"id": "huihui-ai/Llama-3.3-70B-Instruct-abliterated"},
    "Envoid/Llama-3.05-NT-Storybreaker-Ministral-70B": {"id": "Envoid/Llama-3.05-NT-Storybreaker-Ministral-70B"},
    "poetools/claude-code": {"id": "poetools/claude-code", "name": "Claude Code"},
    "voyage/voyage-4-lite": {"id": "voyage/voyage-4-lite", "name": "Voyage 4 Lite"},
    "bfl/flux-2-pro": {"id": "bfl/flux-2-pro", "name": "FLUX.2 Pro"}}},
  "mistral": {"id": "mistral", "name": "Mistral", "npm": "@ai-sdk/openai-compatible", "api": "https://mi.invalid/v1", "models": {
    "mistral-small": {"id": "mistral-small"},
    "mistral-medium-2508": {"id": "mistral-medium-2508", "name": "Mistral Medium 2508"}}},
  "databricks": {"id": "databricks", "name": "Databricks", "npm": "@ai-sdk/openai-compatible", "api": "https://db.invalid/v1", "models": {
    "databricks-gpt-oss-120b": {"id": "databricks-gpt-oss-120b", "name": "GPT OSS 120B"}}}}}`

// TestShareUnlisted checks that a model ID that two or more providers list
// alike, and models.dev does not, is a model of the catalog's own, under the
// lab the IDs name, with the lab's facts; that so is one provider's ID that
// names the lab as its maker, or of the lab's own, or by its brand alone;
// that routers, makers
// without a lab and fine-tunes get none; and that an ID naming a model the
// catalog lists, as the ID or with the lab's name before it, ties to it.
func TestShareUnlisted(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(sharedDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := c.Models["alibaba/qwen3-14b"]
	if !ok || m.Name != "Qwen3 14B" || m.Inferred == nil || *m.Inferred != (Inference{LabBy: LabByMaker, Facts: "alibaba"}) ||
		m.Limit.Context != 131072 || !slices.Equal(m.Providers, []ProviderID{"alibaba", "gw", "hf"}) {
		t.Fatalf("alibaba/qwen3-14b = %+v, %v; want a shared model at alibaba, gw and hf, with Alibaba's facts", m, ok)
	}
	if !slices.Equal(slices.Sorted(slices.Values(rep.Inferred)), []ModelID{"alibaba/qwen-mt-turbo", "alibaba/qwen3-14b",
		"alibaba/qwen3-14b-uncensored", "black-forest-labs/flux-2-pro", "meta/llama-3.05-nt-storybreaker-ministral-70b",
		"meta/llama-3.3-70b-instruct-abliterated", "meta/llama-4-scout", "mistral/mistral-medium-2508", "voyage/voyage-4-lite",
		"xai/grok-3"}) {
		t.Errorf("report inferred %v, want the shared Qwen3 14B, Alibaba's own Qwen MT Turbo, the Llama of a named maker, and Grok 3 by its brand", rep.Inferred)
	}
	if m := c.Models["meta/llama-4-scout"]; m.Inferred == nil || *m.Inferred != (Inference{LabBy: LabByMaker, Facts: "gw"}) {
		t.Errorf("meta/llama-4-scout = %+v, want inferred from gw's listing", m)
	}
	if o := mustOffering(t, c, "gw", "meta/llama-4"); o.Basis != BasisID || o.ProviderOptions.ID != "meta-llama/Meta-Llama-4" {
		t.Errorf("gw's meta/llama-4 = %+v; want meta-llama/Meta-Llama-4 tied by ID", o)
	}
	if m := c.Models["xai/grok-3"]; m.Inferred == nil || *m.Inferred != (Inference{LabBy: LabByBrand, Facts: "gw"}) {
		t.Errorf("xai/grok-3 = %+v; want a model of one provider's, of lab xai by its brand", m)
	}
	if o := mustOffering(t, c, "gw", "xai/grok-3"); o.Basis != BasisInferred || o.ProviderOptions.ID != "grok-3" {
		t.Errorf("gw's xai/grok-3 = %+v; want grok-3, inferred", o)
	}
	for _, tt := range []struct {
		model ModelID
		want  Finetuned
	}{
		{"alibaba/qwen3-14b-uncensored", Finetuned{Of: "alibaba/qwen3-14b", Kind: TuneFinetune}},
		{"meta/llama-3.3-70b-instruct-abliterated", Finetuned{Base: "meta-llama/Llama-3.3-70B-Instruct", Of: "meta/llama-3.3-70b-instruct",
			By: "huihui-ai", Kind: TuneFinetune}},
		{"meta/llama-3.05-nt-storybreaker-ministral-70b", Finetuned{By: "envoid", Kind: TuneFinetune}},
	} {
		m, ok := c.Models[tt.model]
		if !ok || m.Finetuned == nil || *m.Finetuned != tt.want || m.Inferred == nil || m.Inferred.LabBy != LabByBase {
			t.Errorf("%s = %+v, %v; want a fine-tune %+v under the tuned model's lab", tt.model, m, ok, tt.want)
		}
	}
	if _, ok := c.Models["anthropic/claude-code"]; ok || !slices.ContainsFunc(rep.Unknown, func(n Note) bool { return n.Model == "poetools/claude-code" }) {
		t.Errorf("gw's poetools/claude-code a model, or not in the report's unknown %v; want it left out: no one tunes Claude", rep.Unknown)
	}
	for id, want := range map[ModelID]string{"voyage/voyage-4-lite": "voyage/voyage-4-lite", "black-forest-labs/flux-2-pro": "bfl/flux-2-pro"} {
		if o := mustOffering(t, c, "gw", id); o.Basis != BasisInferred || o.ProviderOptions.ID != want {
			t.Errorf("gw's %s = %+v; want %s, of a maker lab", id, o, want)
		}
	}
	o := mustOffering(t, c, "hf", "alibaba/qwen3-14b")
	if o.Basis != BasisInferred || o.ProviderOptions.ID != "Qwen/Qwen3-14B" || o.ProviderOptions.Name != "Qwen3-14B" ||
		o.Variants["free"].ProviderOptions.ID != "Qwen/Qwen3-14B:free" {
		t.Errorf("hf offering = %+v; want the inferred basis, HF's ID and name, and its free tier", o)
	}
	if !slices.ContainsFunc(rep.Unknown, func(n Note) bool { return n.Provider == "hf" && n.Model == "acme-tools/rerank-3" }) {
		t.Errorf("hf's acme-tools/rerank-3 not in the report's unknown %v; want it left out, of no lab", rep.Unknown)
	}
	if _, ok := c.Providers["hf"].Unlisted["openrouter/auto"]; ok {
		t.Error("hf's router openrouter/auto in the catalog; want it left out")
	}
	if o := mustOffering(t, c, "databricks", "openai/gpt-oss-120b"); o.Basis != BasisID || o.ProviderOptions.ID != "databricks-gpt-oss-120b" {
		t.Errorf("databricks offering = %+v; want databricks-gpt-oss-120b tied by ID", o)
	}
}

// matchDoc is a models.dev catalog of unlisted IDs that name models it
// lists, in another order, without a date, or as models.dev ties the ID at
// another provider; of a dated ID of an undated model; of two spellings of
// one model at two providers; and of one model a provider lists twice,
// under IDs that name its lab by the brand alone.
const matchDoc = `{
"models": {
  "anthropic/claude-haiku-4-5": {"id": "anthropic/claude-haiku-4-5", "name": "Claude Haiku 4.5"},
  "anthropic/claude-3-7-sonnet-20250219": {"id": "anthropic/claude-3-7-sonnet-20250219", "name": "Claude Sonnet 3.7"},
  "openai/gpt-3.5-turbo": {"id": "openai/gpt-3.5-turbo", "name": "GPT-3.5 Turbo"},
  "xai/grok-9": {"id": "xai/grok-9", "name": "Grok 9"},
  "alibaba/qwen3-max": {"id": "alibaba/qwen3-max", "name": "Qwen3 Max"},
  "zhipuai/glm-5": {"id": "zhipuai/glm-5", "name": "GLM 5"}},
"providers": {
  "vc": {"id": "vc", "name": "VC", "npm": "@ai-sdk/openai-compatible", "api": "https://vc.invalid/v1", "models": {
    "spacexai/grok-9-fast": {"id": "spacexai/grok-9-fast", "canonical_model_id": "xai/grok-9"}}},
  "hl": {"id": "hl", "name": "HL", "npm": "@ai-sdk/openai-compatible", "api": "https://hl.invalid/v1", "models": {
    "claude-4.5-haiku": {"id": "claude-4.5-haiku"},
    "claude-3.7-sonnet": {"id": "claude-3.7-sonnet"},
    "gpt-3.5-turbo-0125": {"id": "gpt-3.5-turbo-0125"},
    "xai/grok-9-fast": {"id": "xai/grok-9-fast"},
    "Qwen/Qwen2.5-7B": {"id": "Qwen/Qwen2.5-7B"}}},
  "az": {"id": "az", "name": "AZ", "npm": "@ai-sdk/openai-compatible", "api": "https://az.invalid/v1", "models": {
    "gpt-3.5-turbo-0125": {"id": "gpt-3.5-turbo-0125"},
    "qwen/qwen2-5-7b": {"id": "qwen/qwen2-5-7b"},
    "glm-9-air": {"id": "glm-9-air"},
    "glm-9.air": {"id": "glm-9.air"}}}}}`

// TestShareUnlistedMatches checks that an unlisted ID ties to a model the
// catalog lists when it names it in another order or punctuation, or when
// models.dev ties it so at another provider; that an undated ID of a dated
// model, and a dated ID of an undated one, are models of their own; and
// that two spellings of one ID make one model, at two providers or at one.
func TestShareUnlistedMatches(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(matchDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		provider ProviderID
		model    ModelID
		id       string
		basis    Basis
	}{
		{"hl", "anthropic/claude-haiku-4-5", "claude-4.5-haiku", BasisID},
		{"hl", "anthropic/claude-3-7-sonnet", "claude-3.7-sonnet", BasisInferred},
		{"hl", "xai/grok-9", "xai/grok-9-fast", BasisID},
		{"hl", "openai/gpt-3.5-turbo-0125", "gpt-3.5-turbo-0125", BasisInferred},
		{"az", "openai/gpt-3.5-turbo-0125", "gpt-3.5-turbo-0125", BasisInferred},
		{"hl", "alibaba/qwen2.5-7b", "Qwen/Qwen2.5-7B", BasisInferred},
		{"az", "alibaba/qwen2.5-7b", "qwen/qwen2-5-7b", BasisInferred},
	} {
		if o := mustOffering(t, c, tt.provider, tt.model); o.Basis != tt.basis || o.ProviderOptions.ID != tt.id {
			t.Errorf("%s/%s = %s, %s; want %s, %s", tt.provider, tt.model, o.ProviderOptions.ID, o.Basis, tt.id, tt.basis)
		}
	}
	if !slices.Equal(slices.Sorted(slices.Values(rep.Inferred)), []ModelID{"alibaba/qwen2.5-7b", "anthropic/claude-3-7-sonnet",
		"openai/gpt-3.5-turbo-0125", "zhipuai/glm-9-air"}) {
		t.Errorf("inferred models %v, want the dated GPT-3.5 Turbo, the undated Claude 3.7 Sonnet, one Qwen2.5 7B and one GLM 9 Air", rep.Inferred)
	}
	// Of two spellings, the one in Zhipu's own style, glm-<version>-…, wins.
	if m, ok := c.Models["zhipuai/glm-9-air"]; !ok || !slices.Equal(m.Providers, []ProviderID{"az"}) {
		t.Errorf("zhipuai/glm-9-air = %+v, %v; want one model of az's two spellings", m, ok)
	}
	if _, ok := c.Models["zhipuai/glm-9.air"]; ok {
		t.Error("zhipuai/glm-9.air made beside zhipuai/glm-9-air; want one model of the two spellings")
	}
	if m := c.Models["alibaba/qwen2.5-7b"]; m.Name != "Qwen2.5 7B" {
		t.Errorf("alibaba/qwen2.5-7b is named %q", m.Name)
	}
}

// copyDoc is a models.dev catalog of unlisted copies of models it lists: free,
// thinking and no-thinking copies at a provider that lists the model too, a
// free copy at one that does not, a preview, GitLab's names without
// "claude", and an ID without it at a provider whose IDs name no host, an
// undated ID of a dated one another provider lists, a thinking
// model of its own, quantized builds, and a fine-tune its description names.
const copyDoc = `{
"models": {
  "zhipuai/glm-5": {"id": "zhipuai/glm-5", "name": "GLM 5"},
  "xiaomi/mimo-v2-flash": {"id": "xiaomi/mimo-v2-flash", "name": "MiMo V2 Flash"},
  "anthropic/claude-opus-4-7": {"id": "anthropic/claude-opus-4-7", "name": "Claude Opus 4.7"},
  "anthropic/claude-opus-4-8": {"id": "anthropic/claude-opus-4-8", "name": "Claude Opus 4.8"},
  "google/gemini-2.5-flash": {"id": "google/gemini-2.5-flash", "name": "Gemini 2.5 Flash"},
  "google/gemma-4-e4b-it": {"id": "google/gemma-4-e4b-it", "name": "Gemma 4 E4B IT"},
  "google/gemma-4-31b-it": {"id": "google/gemma-4-31b-it", "name": "Gemma 4 31B IT"},
  "moonshotai/kimi-k2-0905": {"id": "moonshotai/kimi-k2-0905", "name": "Kimi K2 0905"},
  "alibaba/qwen3-32b": {"id": "alibaba/qwen3-32b", "name": "Qwen3 32B"},
  "baidu/ernie-4.5-21b-a3b": {"id": "baidu/ernie-4.5-21b-a3b", "name": "ERNIE 4.5 21B A3B"}},
"providers": {
  "oc": {"id": "oc", "name": "OC", "npm": "@ai-sdk/openai-compatible", "api": "https://oc.invalid/v1", "models": {
    "glm-5": {"id": "glm-5", "canonical_model_id": "zhipuai/glm-5"},
    "glm-5-free": {"id": "glm-5-free", "name": "GLM-5 Free", "cost": {"input": 0, "output": 0}},
    "claude-opus-4-7": {"id": "claude-opus-4-7", "canonical_model_id": "anthropic/claude-opus-4-7"},
    "claude-opus-4-7-think": {"id": "claude-opus-4-7-think"},
    "gemini-2.5-flash": {"id": "gemini-2.5-flash", "canonical_model_id": "google/gemini-2.5-flash"},
    "gemini-2.5-flash-nothink": {"id": "gemini-2.5-flash-nothink"},
    "mimo-v2-flash-free": {"id": "mimo-v2-flash-free", "cost": {"input": 0, "output": 0}},
    "kimi-k2-0905-preview": {"id": "kimi-k2-0905-preview"}}},
  "gitlab": {"id": "gitlab", "name": "GitLab", "npm": "@ai-sdk/openai-compatible", "api": "https://gl.invalid/v1", "models": {
    "duo-chat-opus-4-8": {"id": "duo-chat-opus-4-8", "name": "Agentic Chat (Claude Opus 4.8)"}}},
  "hf": {"id": "hf", "name": "HF", "npm": "@ai-sdk/openai-compatible", "api": "https://hf.invalid/v1", "models": {
    "Qwen/Qwen3-235B-A22B-Thinking-2507": {"id": "Qwen/Qwen3-235B-A22B-Thinking-2507"},
    "Qwen/Qwen3-32B-FP8": {"id": "Qwen/Qwen3-32B-FP8"}}},
  "hl": {"id": "hl", "name": "HL", "npm": "@ai-sdk/openai-compatible", "api": "https://hl.invalid/v1", "models": {
    "qwen3-235b-a22b-thinking": {"id": "qwen3-235b-a22b-thinking"},
    "ernie-4.5-21b-a3b-thinking": {"id": "ernie-4.5-21b-a3b-thinking"}}},
  "ng": {"id": "ng", "name": "NG", "npm": "@ai-sdk/openai-compatible", "api": "https://ng.invalid/v1", "models": {
    "gemma-4-E4B-it-IQ4_XS": {"id": "gemma-4-E4B-it-IQ4_XS"},
    "opus-4-7": {"id": "opus-4-7"},
    "gemma-4-31b-it-garnet": {"id": "gemma-4-31b-it-garnet", "name": "Garnet",
      "description": "Garnet is a multimodal Gemma 4 31B creative finetune for expressive dialogue"}}}}}`

// TestCopiesAndTunes checks that an unlisted copy of a model the catalog
// lists is that model, not one of its own: a free or thinking copy of a
// listing of its provider's is that offering's variant, and a no-thinking
// copy its alias; a free copy alone is the offering; a name without its
// lab's family word ties to the model. A preview, an undated ID of a dated
// model, and a thinking model whose provider lists no model it is a mode of
// are models of their own. Quantized builds, and a fine-tune its
// description names, are fine-tunes of the model they build on.
func TestCopiesAndTunes(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(copyDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		provider ProviderID
		model    ModelID
		variant  string
		id       string
	}{
		{"oc", "zhipuai/glm-5", "free", "glm-5-free"},
		{"oc", "anthropic/claude-opus-4-7", "thinking", "claude-opus-4-7-think"},
	} {
		if o := mustOffering(t, c, tt.provider, tt.model); o.Variants[tt.variant].ProviderOptions.ID != tt.id {
			t.Errorf("%s's %s variants = %+v; want %s to be %s", tt.provider, tt.model, o.Variants, tt.variant, tt.id)
		}
	}
	if o := mustOffering(t, c, "oc", "google/gemini-2.5-flash"); o.ProviderOptions.ID != "gemini-2.5-flash" ||
		!slices.Contains(o.Aliases, "gemini-2.5-flash-nothink") {
		t.Errorf("oc's gemini-2.5-flash = %+v; want gemini-2.5-flash the call, -nothink an alias", o)
	}
	for _, tt := range []struct {
		provider ProviderID
		model    ModelID
		id       string
	}{
		{"oc", "xiaomi/mimo-v2-flash", "mimo-v2-flash-free"},
		{"oc", "moonshotai/kimi-k2-0905-preview", "kimi-k2-0905-preview"},
		{"gitlab", "anthropic/claude-opus-4-8", "duo-chat-opus-4-8"},
		{"hl", "alibaba/qwen3-235b-a22b-thinking", "qwen3-235b-a22b-thinking"},
		{"hl", "baidu/ernie-4.5-21b-a3b-thinking", "ernie-4.5-21b-a3b-thinking"},
	} {
		if o := mustOffering(t, c, tt.provider, tt.model); o.ProviderOptions.ID != tt.id {
			t.Errorf("%s's %s calls %q, want %q", tt.provider, tt.model, o.ProviderOptions.ID, tt.id)
		}
	}
	for _, id := range []ModelID{"zhipuai/glm-5-free", "anthropic/claude-opus-4-7-think", "google/gemini-2.5-flash-nothink",
		"xiaomi/mimo-v2-flash-free", "anthropic/opus-4-8"} {
		if _, ok := c.Models[id]; ok {
			t.Errorf("%s made a model of its own; want it a copy of the model it names", id)
		}
	}
	// Without a host's word before it, an ID lacking its lab's family word
	// is no copy: NG's "opus-4-7" is a model of its own.
	if o := mustOffering(t, c, "ng", "anthropic/opus-4-7"); o.ProviderOptions.ID != "opus-4-7" {
		t.Errorf("ng's opus-4-7 = %+v; want a model of its own, not anthropic/claude-opus-4-7", o)
	}
	for id, want := range map[ModelID]Finetuned{
		"google/gemma-4-e4b-it-iq4-xs": {Of: "google/gemma-4-e4b-it", Kind: TuneQuantized},
		"alibaba/qwen3-32b-fp8":        {Of: "alibaba/qwen3-32b", Kind: TuneQuantized},
		"google/gemma-4-31b-it-garnet": {Of: "google/gemma-4-31b-it", Kind: TuneFinetune},
	} {
		if m, ok := c.Models[id]; !ok || m.Finetuned == nil || *m.Finetuned != want {
			t.Errorf("%s = %+v, %v; want a fine-tune %+v", id, m.Finetuned, ok, want)
		}
	}
}

// ownDoc is a models.dev catalog of providers' own models: Bee's, OpenCode's
// and its Go plan's, and NanoGPT's, which writes itself "nanogpt" in its IDs,
// with a product name only Thinking Machines uses; of a safety model that
// mentions routing; and of routers, a preset and a tool: GW's own, under a
// family of its name, its plan's, NanoGPT's, and OpenRouter's at GW.
const ownDoc = `{
"models": {
  "thinkingmachines/inkling": {"id": "thinkingmachines/inkling", "name": "Inkling"},
  "meta/llama-4": {"id": "meta/llama-4", "name": "Llama 4"}},
"providers": {
  "bee": {"id": "bee", "name": "Bee", "npm": "@ai-sdk/openai-compatible", "api": "https://bee.invalid/v1", "models": {
    "bee-buzz": {"id": "bee-buzz", "name": "Bee Buzz 1.0", "description": "Mid-tier production model for tool-using agents"}}},
  "opencode": {"id": "opencode", "name": "OpenCode", "npm": "@ai-sdk/openai-compatible", "api": "https://oc.invalid/v1", "models": {
    "big-pickle": {"id": "big-pickle", "name": "Big Pickle"}}},
  "opencode-go": {"id": "opencode-go", "name": "OpenCode Go", "npm": "@ai-sdk/openai-compatible", "api": "https://ocg.invalid/v1", "models": {
    "space-bunny": {"id": "space-bunny", "name": "Space Bunny"}}},
  "gw": {"id": "gw", "name": "GW", "npm": "@ai-sdk/openai-compatible", "api": "https://gw.invalid/v1", "models": {
    "inkling-256k": {"id": "inkling-256k", "name": "Inkling 256K"},
    "meta-llama/llama-guard-4-12b": {"id": "meta-llama/llama-guard-4-12b", "description": "Safety model for policy screening, moderation, and risk-aware routing workflows"},
    "custom": {"id": "custom", "name": "Custom Model", "description": "Automatic model router for matching prompts to suitable backends and budgets"},
    "gw/zdr": {"id": "gw/zdr", "name": "Zero Data Retention", "description": "GW privacy routing alias that prefers zero data retention model endpoints."},
    "universal-summarizer": {"id": "universal-summarizer", "name": "Universal Summarizer"},
    "gw-auto/balanced": {"id": "gw-auto/balanced", "name": "Auto Balanced"},
    "openrouter/auto": {"id": "openrouter/auto", "name": "Auto Router"}}},
  "gw-plan": {"id": "gw-plan", "name": "GW Plan", "npm": "@ai-sdk/openai-compatible", "api": "https://gwp.invalid/v1", "models": {
    "model-router": {"id": "model-router", "name": "Model Router"}}},
  "nano-gpt": {"id": "nano-gpt", "name": "NanoGPT", "npm": "@ai-sdk/openai-compatible", "api": "https://ng.invalid/v1", "models": {
    "nanogpt/coding-router": {"id": "nanogpt/coding-router", "name": "Coding Router"},
    "auto-model": {"id": "auto-model", "name": "Auto model"},
    "celeris-1": {"id": "celeris-1", "name": "Celeris 1"}}}}}`

// TestOwnModelsAndServices checks that a model ID that names no maker, and
// that no rule finds a lab of, is its provider's own model, or its plan's
// owner's; that a product name its maker alone uses is enough to tie one
// listing; and that routers, presets and tools are services, not models,
// which the catalog leaves out, though a model that only mentions routing is
// a model.
func TestOwnModelsAndServices(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(ownDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[ModelID]Inference{
		"bee/bee-buzz":                  {LabBy: LabByProvider, Facts: "bee"},
		"opencode/big-pickle":           {LabBy: LabByProvider, Facts: "opencode"},
		"opencode/space-bunny":          {LabBy: LabByProvider, Facts: "opencode-go"},
		"thinkingmachines/inkling-256k": {LabBy: LabByProduct, Facts: "gw"},
		"meta/llama-guard-4-12b":        {LabBy: LabByMaker, Facts: "gw"},
		"nanogpt/celeris-1":             {LabBy: LabByProvider, Facts: "nano-gpt"},
	} {
		if m, ok := c.Models[id]; !ok || m.Inferred == nil || *m.Inferred != want {
			t.Errorf("%s = %+v, %v; want a model inferred %+v", id, m, ok, want)
		}
	}
	// A service is left out of the catalog, and listed in the report.
	want := map[ProviderID][]string{
		"gw":       {"custom", "gw/zdr", "universal-summarizer", "gw-auto/balanced", "openrouter/auto"},
		"gw-plan":  {"model-router"},
		"nano-gpt": {"nanogpt/coding-router", "auto-model"},
	}
	kinds := map[string]Service{"gw/custom": ServiceRouter, "gw/gw/zdr": ServicePreset, "gw/universal-summarizer": ServiceTool}
	got := make(map[string]Service)
	for _, n := range rep.Services {
		got[string(n.Provider)+"/"+n.Model] = Service(n.Text)
	}
	for pid, ids := range want {
		for _, id := range ids {
			if _, ok := got[string(pid)+"/"+id]; !ok {
				t.Errorf("%s's %s not in the report's services %v", pid, id, rep.Services)
			}
			for key, u := range c.Providers[pid].Unlisted {
				if u.ProviderOptions.ID == id {
					t.Errorf("%s's service %s in the catalog, unlisted as %s", pid, id, key)
				}
			}
		}
	}
	for key, kind := range kinds {
		if got[key] != kind {
			t.Errorf("service %s is a %q, want a %s", key, got[key], kind)
		}
	}
}

// TestDoubleDashIDs checks that SAP AI Core's double dash, which the rules
// give it as a separator, separates the maker from the model in its IDs, as
// a slash does: its "amazon--nova-lite" is amazon/nova-lite, and its
// "anthropic--claude-3-opus" Anthropic's claude-3-opus, not
// anthropic/-claude-3-opus; and that it separates nothing at other
// providers.
func TestDoubleDashIDs(t *testing.T) {
	r := DefaultRules()
	for _, tt := range []struct {
		provider ProviderID
		id       string
		want     []string
	}{
		{"sap-ai-core", "anthropic--claude-3-haiku", []string{"anthropic", "claude-3-haiku"}},
		{"openrouter", "anthropic--claude-3-haiku", []string{"anthropic--claude-3-haiku"}},
		{"openrouter", "~openai/gpt-6-astra", []string{"openai", "gpt-6-astra"}},
		{"cloudflare-workers-ai", "@cf/meta/llama-3.1-8b", []string{"cf", "meta", "llama-3.1-8b"}},
	} {
		if got := r.IDParts(tt.provider, tt.id); !slices.Equal(got, tt.want) {
			t.Errorf("IDParts(%s, %q) = %q, want %q", tt.provider, tt.id, got, tt.want)
		}
	}
	b, err := modelsdev.DecodeBundle([]byte(`{
"models": {
  "amazon/nova-lite": {"id": "amazon/nova-lite", "name": "Nova Lite"},
  "anthropic/claude-opus-4-8": {"id": "anthropic/claude-opus-4-8", "name": "Claude Opus 4.8"}},
"providers": {
  "sap-ai-core": {"id": "sap-ai-core", "name": "SAP AI Core", "npm": "@ai-sdk/openai-compatible", "api": "https://sap.invalid/v1", "models": {
    "amazon--nova-lite": {"id": "amazon--nova-lite"},
    "anthropic--claude-3-opus": {"id": "anthropic--claude-3-opus"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	for model, want := range map[ModelID]Basis{"amazon/nova-lite": BasisID, "anthropic/claude-3-opus": BasisInferred} {
		if o := mustOffering(t, c, "sap-ai-core", model); o.Basis != want {
			t.Errorf("sap-ai-core's %s basis %q, want %q", model, o.Basis, want)
		}
	}
	for id := range c.Models {
		if strings.Contains(string(id), "/-") {
			t.Errorf("model %s has a dash after its lab", id)
		}
	}
}

// TestLabSpelling checks that an ID the mapping makes writes its brand and
// version as its lab's models.dev IDs do, and that IDs a dash or a dot apart
// are one model: Qwen's joined and dotted, Llama's and Claude's dashed.
func TestLabSpelling(t *testing.T) {
	b, err := modelsdev.DecodeBundle([]byte(`{
"models": {
  "alibaba/qwen3-max": {"id": "alibaba/qwen3-max", "name": "Qwen3 Max"},
  "alibaba/qwen2.5-coder-32b-instruct": {"id": "alibaba/qwen2.5-coder-32b-instruct", "name": "Qwen2.5 Coder 32B Instruct"},
  "meta/llama-3.3-70b-instruct": {"id": "meta/llama-3.3-70b-instruct", "name": "Llama 3.3 70B Instruct"},
  "meta/llama-4-scout": {"id": "meta/llama-4-scout", "name": "Llama 4 Scout"},
  "anthropic/claude-3-5-haiku-20241022": {"id": "anthropic/claude-3-5-haiku-20241022", "name": "Claude Haiku 3.5"},
  "cohere/command-a": {"id": "cohere/command-a", "name": "Command A"}},
"providers": {
  "do": {"id": "do", "name": "DO", "npm": "@ai-sdk/openai-compatible", "api": "https://do.invalid/v1", "models": {
    "qwen-2.5-14b-instruct": {"id": "qwen-2.5-14b-instruct"},
    "llama3.3-70b-instruct": {"id": "llama3.3-70b-instruct"},
    "claude-3.5-haiku": {"id": "claude-3.5-haiku"},
    "cohere-embed-v-4-0": {"id": "cohere-embed-v-4-0"}}},
  "ali": {"id": "ali", "name": "Ali", "npm": "@ai-sdk/openai-compatible", "api": "https://ali.invalid/v1", "models": {
    "qwen2-5-14b-instruct": {"id": "qwen2-5-14b-instruct"}}},
  "vc": {"id": "vc", "name": "VC", "npm": "@ai-sdk/openai-compatible", "api": "https://vc.invalid/v1", "models": {
    "cohere/embed-v4.0": {"id": "cohere/embed-v4.0"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := FromModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		model     ModelID
		providers []ProviderID
	}{
		{"alibaba/qwen2.5-14b-instruct", []ProviderID{"ali", "do"}},
		{"meta/llama-3.3-70b-instruct", []ProviderID{"do"}},
		{"anthropic/claude-3-5-haiku", []ProviderID{"do"}},
		{"cohere/embed-v4.0", []ProviderID{"do", "vc"}},
	} {
		if m, ok := c.Models[tt.model]; !ok || !slices.Equal(m.Providers, tt.providers) {
			t.Errorf("%s = %v, %v; want it served by %v", tt.model, m.Providers, ok, tt.providers)
		}
	}
	for _, id := range []ModelID{"alibaba/qwen-2.5-14b-instruct", "alibaba/qwen2-5-14b-instruct", "meta/llama3.3-70b-instruct",
		"anthropic/claude-3.5-haiku", "cohere/embed-v-4-0"} {
		if _, ok := c.Models[id]; ok {
			t.Errorf("%s is a model; want it spelt as its lab spells it, one model with its other spelling", id)
		}
	}
}
