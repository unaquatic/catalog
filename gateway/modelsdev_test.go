package gateway

import (
	"bytes"
	"errors"
	"flag"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unaquatic/catalog/modelsdev"
)

var update = flag.Bool("update", false, "rewrite testdata/catalog.golden.json")

// fixtureProviders are the providers of testdata/catalog.json.
var fixtureProviders = []ProviderID{"anthropic", "github-copilot", "llmgateway", "openai", "opencode", "openrouter", "vercel"}

// loadFixture returns the models.dev bundle of testdata/catalog.json.
func loadFixture(t *testing.T) modelsdev.Bundle {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := modelsdev.DecodeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixtureOptions are the options the tests map testdata/catalog.json with.
func fixtureOptions(infer bool) Options {
	return Options{Infer: infer, Source: "testdata/catalog.json", Generated: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
}

// build lists testdata/catalog.json with every provider, applying the
// mapping's rules when infer is set.
func build(t *testing.T, infer bool) (*Listings, Report) {
	t.Helper()
	l, rep, err := ListModelsDev(loadFixture(t), fixtureOptions(infer))
	if err != nil {
		t.Fatalf("ListModelsDev: %v", err)
	}
	return l, rep
}

// buildFixture lists testdata/catalog.json with the mapping's rules.
func buildFixture(t *testing.T) (*Listings, Report) {
	t.Helper()
	return build(t, true)
}

// groupFixture maps testdata/catalog.json to the catalog, with the
// mapping's rules.
func groupFixture(t *testing.T) (*Catalog, Report) {
	t.Helper()
	c, rep, err := FromModelsDev(loadFixture(t), fixtureOptions(true))
	if err != nil {
		t.Fatalf("FromModelsDev: %v", err)
	}
	return c, rep
}

// mustListing returns the listing of provider p with ID id in l.
func mustListing(t *testing.T, l *Listings, p ProviderID, id string) Listing {
	t.Helper()
	o, ok := l.Listing(Ref{Provider: p, ID: id})
	if !ok {
		t.Fatalf("listings lack %s/%s", p, id)
	}
	return o
}

// mustModel returns model id of models.
func mustModel(t *testing.T, models map[ModelID]Model, id ModelID) Model {
	t.Helper()
	m, ok := models[id]
	if !ok {
		t.Fatalf("catalog lacks %s; it has %v", id, slices.Sorted(maps.Keys(models)))
	}
	return m
}

// refs returns the refs of the listings that serve model m, as strings.
func refs(l *Listings, m ModelID) []string {
	var out []string
	for r := range l.ListingsOf(m) {
		out = append(out, r.String())
	}
	return out
}

// notes returns ns as strings.
func notes(ns []Note) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.String())
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func TestFromModelsDevGolden(t *testing.T) {
	c, _ := groupFixture(t)
	got, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/catalog.golden.json"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("catalog differs from %s; run with -update and review the diff", golden)
	}
	back, err := Decode(got)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if again, _ := Encode(back); !bytes.Equal(again, got) {
		t.Error("catalog does not survive Decode and Encode unchanged")
	}
}

// TestNames checks that the catalog names the labs and the families of its
// models, and its providers: by the rules, the first of them winning, a
// lab by its own rule or else by that of the provider of its ID; else a lab
// by the name of the provider of its ID; else by the words of the ID.
func TestNames(t *testing.T) {
	c, _ := groupFixture(t)
	// Each catalog names exactly the labs and families of its own models,
	// the copy Keep makes too.
	for name, c := range map[string]*Catalog{"catalog": c, "kept": c.Keep("openai")} {
		labs, families := make(map[ProviderID]bool), make(map[string]bool)
		for _, m := range c.Models {
			labs[m.Lab] = true
			if m.Family != "" {
				families[m.Family] = true
			}
		}
		if !slices.Equal(slices.Sorted(maps.Keys(c.Labs)), slices.Sorted(maps.Keys(labs))) {
			t.Errorf("%s: labs %v, want its models' %v", name, slices.Sorted(maps.Keys(c.Labs)), slices.Sorted(maps.Keys(labs)))
		}
		if !slices.Equal(slices.Sorted(maps.Keys(c.Families)), slices.Sorted(maps.Keys(families))) {
			t.Errorf("%s: families %v, want its models' %v", name, slices.Sorted(maps.Keys(c.Families)), slices.Sorted(maps.Keys(families)))
		}
		for id, l := range c.Labs {
			if l.ID != id || l.Name == "" {
				t.Errorf("%s: lab %s is %+v", name, id, l)
			}
		}
	}
	for _, tt := range []struct {
		got  string
		want string
		why  string
	}{
		{c.Labs["openai"].Name, "OpenAI", "the provider of the lab's ID"},
		{c.Labs["anthropic"].Name, "Anthropic", "the provider of the lab's ID"},
		{c.Labs["bytedance-seed"].Name, "Bytedance Seed", "the words of the ID; the fixture's names spell no ByteDance"},
		{c.Families["claude-opus"].Name, "Claude Opus", "the words of the ID"},
		{c.Families["gpt-sol"].Name, "GPT Sol", "the words of the ID, spelt as names spell them"},
		{c.Families["o"].Name, "o", "a rule in rules.json"},
	} {
		if tt.got != tt.want {
			t.Errorf("name %q, want %q: %s", tt.got, tt.want, tt.why)
		}
	}

	opts := fixtureOptions(true)
	opts.Rules = withRules(t, []string{
		`{"match": {"lab": "openai"}, "name": "Open AI"}`,
		`{"match": {"provider": "openai"}, "name": "OpenAI Platform"}`,
		`{"match": {"provider": "anthropic"}, "name": "Anthropic PBC"}`,
		`{"match": {"family": "claude-opus"}, "name": "Opus"}`,
		`{"match": {"family": "claude-opus"}, "name": "Not Opus"}`,
	})
	c, _, err := FromModelsDev(loadFixture(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ got, want, why string }{
		{c.Labs["openai"].Name, "Open AI", "a lab's name rule, over its provider's"},
		{c.Providers["openai"].Name, "OpenAI Platform", "a provider's name rule"},
		{c.Providers["anthropic"].Name, "Anthropic PBC", "a provider's name rule"},
		{c.Labs["anthropic"].Name, "Anthropic PBC", "the name rule of the provider of the lab's ID"},
		{c.Families["claude-opus"].Name, "Opus", "the first of two rules' family names"},
	} {
		if tt.got != tt.want {
			t.Errorf("name %q, want %q: %s", tt.got, tt.want, tt.why)
		}
	}
}

// TestModelsDevOnly checks that without the mapping's rules the catalog
// holds models.dev's ties alone.
func TestModelsDevOnly(t *testing.T) {
	c, rep := build(t, false)
	if rep.Models != 13 || rep.Listings != 34 || len(rep.Merged) != 0 || len(rep.Derived) != 0 {
		t.Errorf("models %d, listings %d, merged %v, derived %v; want 13, 34, none, none",
			rep.Models, rep.Listings, rep.Merged, rep.Derived)
	}
	if want := map[Basis]int{BasisCanonical: 15, BasisLab: 4, BasisNone: 15}; !maps.Equal(rep.Bases, want) {
		t.Errorf("bases = %v, want %v", rep.Bases, want)
	}
	for _, tt := range []struct {
		provider ProviderID
		id       string
		want     ModelID
	}{
		{"anthropic", "claude-opus-4-8", "anthropic/claude-opus-4-8"},
		{"vercel", "openai/gpt-6-astra-fast", "openai/gpt-6-astra-fast"},
		{"llmgateway", "gpt-6-astra", ""},
		{"opencode", "claude-opus-4-8", ""},
		{"openrouter", "openai/gpt-6-astra-pro", ""},
		{"openrouter", "openai/gpt-6.1-sol-pro", "openai/gpt-6.1-sol"},
		{"openrouter", "~openai/gpt-astra-latest", ""},
		{"llmgateway", "anthropic/claude-opus-latest", "anthropic/claude-opus-5-5"},
	} {
		o := mustListing(t, c, tt.provider, tt.id)
		if o.Model != tt.want || o.Inferred() || (o.Basis == "") != (tt.want == "") {
			t.Errorf("%s/%s: model %q, basis %q; want %q from models.dev", tt.provider, tt.id, o.Model, o.Basis, tt.want)
		}
	}
	if fast := mustModel(t, c.Models, "openai/gpt-6-astra-fast"); fast.Derived != nil {
		t.Errorf("astra fast = %+v, want models.dev's model unchanged", fast)
	}
	if opt := mustListing(t, c, "openai", "gpt-6.1-sol").Modes["pro"]; opt.Model != "" || opt.Body == nil {
		t.Errorf("sol pro mode = %+v, want its body and no model of its own", opt)
	}
}

func TestFromModelsDevReport(t *testing.T) {
	c, rep := buildFixture(t)
	if rep.Providers != 7 || rep.Models != 18 || rep.Listings != 34 || len(c.Models) != 18 {
		t.Errorf("providers %d, models %d (catalog %d), listings %d; want 7, 18, 34",
			rep.Providers, rep.Models, len(c.Models), rep.Listings)
	}
	want := map[Basis]int{BasisCanonical: 13, BasisLab: 4, BasisID: 8, BasisTier: 1, BasisPro: 2, BasisInferred: 5, BasisNone: 0}
	if !maps.Equal(rep.Bases, want) {
		t.Errorf("bases = %v, want %v", rep.Bases, want)
	}
	if want := []ModelID{"openai/gpt-6-astra-fast"}; !slices.Equal(rep.Merged, want) {
		t.Errorf("merged = %v, want %v", rep.Merged, want)
	}
	if want := []ModelID{"openai/gpt-6-astra-pro", "openai/gpt-6.1-sol-pro"}; !slices.Equal(rep.Derived, want) {
		t.Errorf("derived = %v, want %v", rep.Derived, want)
	}
	wantTiers := []string{
		"vercel/anthropic/claude-opus-5.5-fast: tier fast of anthropic/claude-opus-5.5",
		"vercel/openai/gpt-6-astra-fast: tier fast of openai/gpt-6-astra",
		"vercel/openai/o3-fast: tier fast of openai/o3",
	}
	if got := notes(rep.Tiers); !slices.Equal(got, wantTiers) {
		t.Errorf("tiers = %q, want %q", got, wantTiers)
	}
	wantNotes := []string{
		`openai/gpt-5.4-nano: mode "turbo" is neither a tier nor a reasoning mode; left out`,
	}
	if got := notes(rep.Notes); !slices.Equal(got, wantNotes) {
		t.Errorf("notes = %q, want %q", got, wantNotes)
	}
	if got, want := notes(rep.Unknown), []string{"openrouter/~google/gemini-pro-latest: no model or lab known"}; !slices.Equal(got, want) {
		t.Errorf("unknown = %q, want %q", got, want)
	}
}

func TestTies(t *testing.T) {
	c, _ := buildFixture(t)
	tests := []struct {
		provider ProviderID
		id       string
		want     ModelID // "" for provider-specific
		basis    Basis
		tier     Tier
	}{
		{"anthropic", "claude-opus-5-5", "anthropic/claude-opus-5-5", BasisCanonical, ""},
		{"anthropic", "claude-opus-4-8", "anthropic/claude-opus-4-8", BasisLab, ""},
		{"openai", "gpt-5.6", "openai/gpt-5.6-sol", BasisCanonical, ""},
		{"github-copilot", "claude-opus-5.5", "anthropic/claude-opus-5-5", BasisCanonical, ""},
		// Rule 1: models.dev's openai/gpt-6-astra-fast is merged into GPT-6 Astra.
		{"vercel", "openai/gpt-6-astra-fast", "openai/gpt-6-astra", BasisTier, TierFast},
		// Rule 2, by ID: bare, lab/ID, a lab alias, dots, :variant.
		{"llmgateway", "gpt-6-astra", "openai/gpt-6-astra", BasisID, ""},
		{"llmgateway", "x-ai/grok-9", "xai/grok-9", BasisID, ""},
		{"opencode", "claude-opus-4-8", "anthropic/claude-opus-4-8", BasisID, ""},
		{"openrouter", "anthropic/claude-opus-4.8", "anthropic/claude-opus-4-8", BasisID, ""},
		{"openrouter", "anthropic/claude-opus-4.8:free", "anthropic/claude-opus-4-8", BasisID, ""},
		{"vercel", "bytedance/seedance-2.0", "bytedance-seed/seedance-2.0", BasisID, ""},
		// Rule 2, pro: by lab/ID.
		{"openrouter", "openai/gpt-6-astra-pro", "openai/gpt-6-astra-pro", BasisPro, ""},
		// Pro, moved from models.dev's tie to the model whose pro mode the ID
		// names; kept where the lab offers the model in no pro mode.
		{"openrouter", "openai/gpt-6.1-sol-pro", "openai/gpt-6.1-sol-pro", BasisPro, ""},
		{"openrouter", "openai/gpt-5.6-sol-pro", "openai/gpt-5.6-sol", BasisCanonical, ""},
		// A "-latest" ID is no model of its own: it is an ID of the model
		// models.dev ties it to, here or for the same ID elsewhere, or else a
		// model of its own, as any other ID, an account's name in its path
		// naming no lab.
		{"llmgateway", "anthropic/claude-opus-latest", "anthropic/claude-opus-5-5", BasisCanonical, ""},
		{"openrouter", "~anthropic/claude-opus-latest", "anthropic/claude-opus-5-5", BasisID, ""},
		{"openrouter", "~openai/gpt-astra-latest", "openai/gpt-astra-latest", BasisInferred, ""},
		{"llmgateway", "accounts/acme/routers/gpt-astra-latest", "openai/gpt-astra-latest", BasisInferred, ""},
		{"llmgateway", "gpt-astra-fast-latest", "openai/gpt-astra-fast-latest", BasisInferred, ""},
		// A video model's -fast is no tier of it, but its ID names its
		// maker, so the mapping makes a model of it.
		{"vercel", "bytedance/seedance-2.0-fast", "bytedance-seed/seedance-2.0-fast", BasisInferred, ""},
		// A provider's own model, which names no maker, is the provider's.
		{"opencode", "big-pickle", "opencode/big-pickle", BasisInferred, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider)+"/"+tt.id, func(t *testing.T) {
			o := mustListing(t, c, tt.provider, tt.id)
			if o.Model != tt.want || o.Basis != tt.basis || o.Tier != tt.tier || o.ProviderSpecific() != (tt.want == "") {
				t.Errorf("model %q, basis %q, tier %q; want %q, %q, %q", o.Model, o.Basis, o.Tier, tt.want, tt.basis, tt.tier)
			}
			if o.Inferred() != (tt.basis != BasisCanonical && tt.basis != BasisLab && tt.basis != "") {
				t.Errorf("Inferred() = %v for basis %q", o.Inferred(), o.Basis)
			}
		})
	}
	// Left out: an alias with no match.
	if _, ok := c.Providers["openrouter"].Models["~google/gemini-pro-latest"]; ok {
		t.Error("openrouter's ~google/gemini-pro-latest is listed; want it left out, as no rule ties it")
	}
	if o := mustListing(t, c, "vercel", "openai/gpt-6-astra-fast"); o.ModelsDev != "openai/gpt-6-astra-fast" || o.TierOf != "openai/gpt-6-astra" {
		t.Errorf("astra fast = models.dev %q, tier of %q; want models.dev's own model kept, tier of openai/gpt-6-astra", o.ModelsDev, o.TierOf)
	}
	if o := mustListing(t, c, "openrouter", "openai/gpt-6.1-sol-pro"); o.ModelsDev != "openai/gpt-6.1-sol" {
		t.Errorf("sol pro = models.dev %q, want models.dev's tie kept, openai/gpt-6.1-sol", o.ModelsDev)
	}
}

func TestModels(t *testing.T) {
	c, _ := buildFixture(t)
	tests := []struct {
		id   ModelID
		want []string
	}{
		{"openai/gpt-6-astra", []string{"llmgateway/gpt-6-astra", "openai/gpt-6-astra", "vercel/openai/gpt-6-astra", "vercel/openai/gpt-6-astra-fast"}},
		{"openai/gpt-astra-latest", []string{"llmgateway/accounts/acme/routers/gpt-astra-latest", "openrouter/~openai/gpt-astra-latest"}},
		{"openai/gpt-6-astra-pro", []string{"openai/gpt-6-astra (pro)", "openrouter/openai/gpt-6-astra-pro"}},
		{"openai/gpt-6.1-sol-pro", []string{"openai/gpt-6.1-sol (pro)", "openrouter/openai/gpt-6.1-sol-pro"}},
		{"anthropic/claude-opus-5-5", []string{"anthropic/claude-opus-5-5", "github-copilot/claude-opus-5.5",
			"llmgateway/anthropic/claude-opus-latest", "openrouter/~anthropic/claude-opus-latest",
			"vercel/anthropic/claude-opus-5.5", "vercel/anthropic/claude-opus-5.5-fast"}},
		{"openai/o3", []string{"openai/o3", "vercel/openai/o3", "vercel/openai/o3-fast"}},
		{"acme/open-1", nil},
	}
	for _, tt := range tests {
		if got := refs(c, tt.id); !slices.Equal(got, tt.want) {
			t.Errorf("%s listings = %q, want %q", tt.id, got, tt.want)
		}
	}
	if _, ok := c.Models["openai/gpt-6-astra-fast"]; ok {
		t.Error("openai/gpt-6-astra-fast is still a model; want it merged")
	}
	astra := mustModel(t, c.Models, "openai/gpt-6-astra")
	if !slices.Equal(astra.Merged, []ModelID{"openai/gpt-6-astra-fast"}) {
		t.Errorf("astra merged = %v", astra.Merged)
	}
	// A mode with a derived model names it; tiers and other modes do not.
	for _, tt := range []struct {
		id, mode string
		want     ModelID
	}{
		{"gpt-6-astra", "pro", "openai/gpt-6-astra-pro"},
		{"gpt-6.1-sol", "pro", "openai/gpt-6.1-sol-pro"},
	} {
		o := mustListing(t, c, "openai", tt.id)
		if opt, ok := o.Modes[tt.mode]; !ok || opt.Model != tt.want || opt.Body == nil {
			t.Errorf("openai/%s mode %s = %+v, %v; want model %q, with its body", tt.id, tt.mode, opt, ok, tt.want)
		}
		for name, opt := range o.Tiers {
			if opt.Model != "" {
				t.Errorf("openai/%s tier %s names model %q, want none", tt.id, name, opt.Model)
			}
		}
	}
	// No alias models: a "-latest" ID models.dev ties is an ID of its model.
	if _, ok := c.Models["anthropic/claude-opus-latest"]; ok {
		t.Error("anthropic/claude-opus-latest is a model; want its IDs under anthropic/claude-opus-5-5")
	}
	pro := mustModel(t, c.Models, "openai/gpt-6-astra-pro")
	if pro.Name != "GPT-6 Astra Pro" || pro.Derived == nil || *pro.Derived != (Derivation{From: "openai/gpt-6-astra", Mode: "pro"}) ||
		pro.Lab != "openai" || pro.Family != "gpt-astra" || pro.Merged != nil {
		t.Errorf("astra pro = %+v", pro)
	}
	acme := mustModel(t, c.Models, "acme/open-1")
	if !acme.OpenWeights || acme.License != "Apache-2.0" || len(acme.Weights) != 1 ||
		acme.Links[0].Type != "model_card" {
		t.Errorf("acme = %+v", acme)
	}
	if jev := mustModel(t, c.Models, "typesafe/jev-1.14.0"); jev.Type != "decision" || jev.Lab != "typesafe" {
		t.Errorf("jev = %+v, want a decision model of lab typesafe", jev)
	}
	n := 0
	for ref, o := range c.ListingsOf(pro.ID) {
		if ref.Mode == "pro" && o.ID != "gpt-6-astra" {
			t.Errorf("ListingsOf(pro) yields %s for %s", o.ID, ref)
		}
		n++
	}
	if n != 2 {
		t.Errorf("ListingsOf(pro) yields %d listings, want 2", n)
	}
}

func TestCandidates(t *testing.T) {
	b := &builder{rules: DefaultRules(), labs: map[ProviderID]bool{"openai": true, "anthropic": true, "xai": true, "bytedance-seed": true}}
	tests := []struct {
		id   string
		want []candidate
	}{
		{"gpt-6-astra", []candidate{{tail: "gpt-6-astra"}}},
		{"openai/gpt-6.1-sol", []candidate{{lab: "openai", tail: "gpt-6-1-sol"}, {tail: "gpt-6-1-sol"}}},
		{"auto/openai/gpt-6-astra", []candidate{{lab: "openai", tail: "gpt-6-astra"}, {tail: "gpt-6-astra"}}},
		{"aws-mantle/gpt-6-astra", []candidate{{tail: "gpt-6-astra"}}},
		{"~x-ai/grok-latest", []candidate{{lab: "xai", tail: "grok-latest"}, {tail: "grok-latest"}}},
		{"openai-gpt-6-astra-pro", []candidate{{lab: "openai", tail: "gpt-6-astra-pro"}, {tail: "openai-gpt-6-astra-pro"}}},
		{"us.anthropic.claude-opus-4-8", []candidate{{lab: "anthropic", tail: "claude-opus-4-8"}, {tail: "us-anthropic-claude-opus-4-8"}}},
		{"bytedance-seed-seedance-2.0", []candidate{{lab: "bytedance-seed", tail: "seedance-2-0"}, {tail: "bytedance-seed-seedance-2-0"}}},
		{"anthropic/claude-opus-4.8:free", []candidate{{lab: "anthropic", tail: "claude-opus-4-8"}, {tail: "claude-opus-4-8"}}},
	}
	for _, tt := range tests {
		if got := b.candidates("gw", tt.id); !slices.Equal(got, tt.want) {
			t.Errorf("candidates(%q) = %+v, want %+v", tt.id, got, tt.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"~Anthropic/Claude-Opus-4.8:free": "anthropic/claude-opus-4-8",
		"claude-opus-5-5@default":         "claude-opus-5-5",
		"qwen2.5_vl_72b":                  "qwen2-5-vl-72b",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"gpt-6-astra-fast": "gpt-6-astra", "glm-5.2-ultrafast": "glm-5.2", "o3-flex": "o3", "fast": "fast"} {
		if got, _, _ := DefaultRules().CutTier(in); got != want {
			t.Errorf("CutTier(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTiersAndModes(t *testing.T) {
	c, _ := buildFixture(t)

	opus := mustListing(t, c, "anthropic", "claude-opus-5-5")
	fast := opus.Tiers[TierFast]
	if fast.Body["speed"] != "fast" || fast.Headers["anthropic-beta"] != "fast-mode-2026-02-01" || fast.Price.Input != 8 {
		t.Errorf("anthropic fast tier = %+v, want speed fast, the beta header and 8/40", fast)
	}
	sol := mustListing(t, c, "openai", "gpt-6.1-sol")
	if sol.Tiers[TierFast].Body["service_tier"] != "priority" || sol.Tiers[TierUltrafast].Body["service_tier"] != "ultrafast" {
		t.Errorf("gpt-6.1-sol tiers = %+v", sol.Tiers)
	}
	pro, ok := sol.Modes["pro"]
	if !ok || pro.Price != nil || pro.Body["reasoning"].(map[string]any)["mode"] != "pro" {
		t.Errorf("pro mode = %+v, %v; want reasoning.mode pro without a price", pro, ok)
	}
	o3 := mustListing(t, c, "vercel", "openai/o3")
	if o3.Tiers[TierFast].Body["service_tier"] != "priority" || o3.Tiers[TierFlex].Price.Input != 1 {
		t.Errorf("vercel o3 tiers = %+v, want fast and flex from its modes", o3.Tiers)
	}
	// A mode that is neither a tier nor a reasoning mode is left out.
	nano := mustListing(t, c, "openai", "gpt-5.4-nano")
	if turbo, ok := nano.Modes["turbo"]; ok {
		t.Errorf("gpt-5.4-nano turbo mode = %+v; want it left out", turbo)
	}
}

func TestEstimatedTierPrices(t *testing.T) {
	c, _ := buildFixture(t)
	sol := mustListing(t, c, "openai", "gpt-6.1-sol")
	tests := []struct {
		name string
		got  *Price
		want PriceTier
	}{
		{"standard", sol.Price, PriceTier{Above: 272_000, Rates: Rates{Input: 4, Output: 15, CacheRead: ptr(0.2), CacheWrite: ptr(5.0)}}},
		{"fast", sol.Tiers[TierFast].Price, PriceTier{Above: 272_000, Estimated: true,
			Rates: Rates{Input: 8, Output: 30, CacheRead: ptr(0.4), CacheWrite: ptr(10.0)}}},
		{"ultrafast", sol.Tiers[TierUltrafast].Price, PriceTier{Above: 272_000, Estimated: true,
			Rates: Rates{Input: 24, Output: 90, CacheRead: ptr(1.2), CacheWrite: ptr(30.0)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got == nil || len(tt.got.Tiers) != 1 {
				t.Fatalf("price = %+v, want one tier", tt.got)
			}
			got := tt.got.Tiers[0]
			if got.Above != tt.want.Above || got.Estimated != tt.want.Estimated || got.Input != tt.want.Input ||
				got.Output != tt.want.Output || *got.CacheRead != *tt.want.CacheRead || *got.CacheWrite != *tt.want.CacheWrite {
				t.Errorf("tier = %+v, want %+v", got, tt.want)
			}
		})
	}
	over200k := mustListing(t, c, "openai", "gpt-5.6-sol").Price
	if len(over200k.Tiers) != 1 || over200k.Tiers[0].Above != 200_000 || over200k.Tiers[0].Input != 8 {
		t.Errorf("gpt-5.6-sol price = %+v, want context_over_200k as a tier above 200000", over200k)
	}
}

func TestOfferingFields(t *testing.T) {
	c, _ := buildFixture(t)
	sol := mustListing(t, c, "openai", "gpt-5.6-sol")
	if want := []Effort{"none", "low", "medium", "high", "xhigh", "max"}; sol.Reasoning == nil || !slices.Equal(sol.Reasoning.Efforts, want) {
		t.Errorf("gpt-5.6-sol efforts = %+v, want %v without null and default", sol.Reasoning, want)
	}
	if sol.Reasoning == nil || sol.Reasoning.Mandatory == nil || *sol.Reasoning.Mandatory {
		t.Errorf("gpt-5.6-sol reasoning = %+v, want it not mandatory: it takes effort none", sol.Reasoning)
	}
	if s := mustListing(t, c, "anthropic", "claude-sonnet-4-5"); s.Reasoning == nil || *s.Reasoning.Budget != (Budget{Min: 1024}) || s.Reasoning.Mandatory != nil {
		t.Errorf("sonnet reasoning = %+v, want a budget from 1024, and mandatory unknown", s.Reasoning)
	}
	pickle := mustListing(t, c, "opencode", "big-pickle")
	if r := pickle.Reasoning; r == nil || r.Mandatory == nil || *r.Mandatory || pickle.Status != StatusBeta || !pickle.OpenWeights {
		t.Errorf("big-pickle = %+v, want reasoning not mandatory for its toggle, beta and open weights", pickle)
	}
	if nano := mustListing(t, c, "openai", "gpt-5.4-nano"); nano.Reasoning != nil {
		t.Errorf("gpt-5.4-nano reasoning = %+v, want nil for a model that does not reason", nano.Reasoning)
	}
	// An override of the package alone keeps the provider's base URL.
	copilot := mustListing(t, c, "github-copilot", "claude-opus-5.5")
	if ep := copilot.Endpoint; ep == nil || ep.Format != FormatAnthropicMessages || ep.API != "https://api.githubcopilot.com" ||
		ep.Auth != (Auth{Scheme: AuthBearer}) ||
		copilot.Interleaved == nil || copilot.Interleaved.Field != "reasoning_text" || copilot.Limit != (Limit{Context: 200_000, Output: 64_000}) {
		t.Errorf("copilot offering = %+v, endpoint %+v", copilot, copilot.Endpoint)
	}
	zen := mustListing(t, c, "opencode", "claude-opus-4-8")
	if ep := zen.Endpoint; ep == nil || !ep.Equal(Endpoint{Format: FormatAnthropicMessages, API: "https://opencode.ai/inference/anthropic/v1",
		Auth: Auth{Scheme: AuthHeader, Header: "x-api-key"}, Env: []string{"OPENCODE_API_KEY"}}) {
		t.Errorf("opencode endpoint = %+v, want anthropic-messages at its own API, whole", zen.Endpoint)
	}
	if ep := mustListing(t, c, "opencode", "gpt-5.6-sol").Endpoint; ep == nil || ep.Format != FormatOpenAIResponses ||
		ep.API != "https://opencode.ai/inference/openai/v1" || ep.APIDefault {
		t.Errorf("opencode gpt endpoint = %+v, want openai-responses at the provider's API", ep)
	}
	if v := mustListing(t, c, "vercel", "anthropic/claude-opus-5.5"); v.Endpoint != nil {
		t.Errorf("vercel offering endpoint = %+v, want the provider's", v.Endpoint)
	}
	if s := mustListing(t, c, "vercel", "bytedance/seedance-2.0"); s.Type != "video" || s.Price != nil {
		t.Errorf("seedance = %+v, want a video model without a price", s)
	}
}

func TestProviders(t *testing.T) {
	c, _ := buildFixture(t)
	bearer, apiKey := Auth{Scheme: AuthBearer}, Auth{Scheme: AuthHeader, Header: "x-api-key"}
	tests := []struct {
		id     ProviderID
		kind   ProviderKind
		format Format
		api    string // "" for the SDK's default
		auth   Auth
		models int
	}{
		{"anthropic", KindLab, FormatAnthropicMessages, "", apiKey, 3},
		{"openai", KindLab, FormatOpenAIResponses, "", bearer, 6},
		{"vercel", KindGateway, FormatOpenAIChat, "", bearer, 8},
		{"openrouter", KindGateway, FormatOpenAIChat, "https://openrouter.ai/api/v1", bearer, 7},
		{"opencode", KindGateway, FormatOpenAIChat, "https://opencode.ai/inference/openai/v1", bearer, 3},
		{"github-copilot", KindGateway, FormatOpenAIChat, "https://api.githubcopilot.com", bearer, 1},
		{"llmgateway", KindGateway, FormatOpenAIChat, "https://api.llmgateway.io/v1", bearer, 5},
	}
	for _, tt := range tests {
		p, ok := c.Providers[tt.id]
		ep := p.Endpoint
		if !ok || p.Kind != tt.kind || ep.Format != tt.format || ep.Auth != tt.auth || len(p.Models) != tt.models ||
			ep.APIDefault != (tt.api == "") || tt.api != "" && ep.API != tt.api || ep.API == "" {
			t.Errorf("provider %s: kind %s, endpoint %+v, %d models, %v; want %s, %s at %q, %+v, %d",
				tt.id, p.Kind, ep, len(p.Models), ok, tt.kind, tt.format, tt.api, tt.auth, tt.models)
		}
	}
	if api := c.Providers["anthropic"].Endpoint.API; api != "https://api.anthropic.com/v1" {
		t.Errorf("anthropic API = %q, want the SDK's default", api)
	}
	if ids := slices.Sorted(maps.Keys(c.Providers)); !slices.Equal(ids, fixtureProviders) {
		t.Errorf("providers = %v, want %v", ids, fixtureProviders)
	}
}

func TestKeepProviders(t *testing.T) {
	c, rep, err := FromModelsDev(loadFixture(t), Options{Providers: []ProviderID{"anthropic", "anthropic"}, Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Providers) != 1 || len(c.Models) != 12 || rep.Listings != 3 || rep.Offerings != 3 {
		t.Errorf("%d providers, %d models, %d listings, %d offerings; want 1, 12 (models.dev's, one merged, none derived), 3, 3",
			len(c.Providers), len(c.Models), rep.Listings, rep.Offerings)
	}
	if opus := mustModel(t, c.Models, "anthropic/claude-opus-5-5"); !slices.Equal(opus.Providers, []ProviderID{"anthropic"}) {
		t.Errorf("opus providers = %v, want only Anthropic", opus.Providers)
	}
	_, _, err = FromModelsDev(loadFixture(t), Options{Providers: []ProviderID{"anthropic", "nope"}})
	if !errors.Is(err, ErrUnknownProvider) || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("err = %v, want ErrUnknownProvider naming nope", err)
	}
}

func TestUnlistedCanonical(t *testing.T) {
	b := modelsdev.Bundle{
		Models: map[string]modelsdev.LabModel{},
		Providers: modelsdev.Catalog{"reseller": {ID: "reseller", Name: "Reseller", NPM: "@ai-sdk/openai-compatible", Models: map[modelsdev.ModelID]modelsdev.Model{
			"x": {ID: "x", Name: "X", CanonicalModelID: "lab/missing"},
		}}},
	}
	c, rep, err := ListModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if o := mustListing(t, c, "reseller", "x"); !o.ProviderSpecific() || o.Basis != "" {
		t.Errorf("offering = %+v, want provider-specific", o)
	}
	want := []string{
		`reseller/x: canonical_model_id "lab/missing" names no models.dev model`,
	}
	if got := notes(rep.Notes); !slices.Equal(got, want) {
		t.Errorf("notes = %q, want %q", got, want)
	}
}

func TestClassifyMode(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		wantKind OptionKind
		wantKey  string
	}{
		{"fast", map[string]any{"service_tier": "priority"}, OptionTier, "fast"},
		{"priority", map[string]any{"service_tier": "priority"}, OptionTier, "fast"},
		{"flex", map[string]any{"service_tier": "flex"}, OptionTier, "flex"},
		{"ultrafast", map[string]any{"service_tier": "ultrafast"}, OptionTier, "ultrafast"},
		{"fast", map[string]any{"speed": "fast"}, OptionTier, "fast"},
		{"pro", map[string]any{"reasoning": map[string]any{"mode": "pro"}}, OptionMode, "pro"},
		{"fast", nil, OptionTier, "fast"},
		{"turbo", map[string]any{"turbo": true}, OptionUnknown, "turbo"},
	}
	for _, tt := range tests {
		kind, key := ClassifyMode(tt.name, tt.body)
		if kind != tt.wantKind || key != tt.wantKey {
			t.Errorf("ClassifyMode(%q, %v) = %d, %q; want %d, %q", tt.name, tt.body, kind, key, tt.wantKind, tt.wantKey)
		}
	}
}

func TestPriceAt(t *testing.T) {
	p := Price{Rates: Rates{Input: 2}, Tiers: []PriceTier{
		{Above: 272_000, Rates: Rates{Input: 4}},
		{Above: 1_000_000, Rates: Rates{Input: 8}},
	}}
	for prompt, want := range map[int]float64{0: 2, 272_000: 2, 272_001: 4, 1_000_001: 8} {
		if got := p.At(prompt).Input; got != want {
			t.Errorf("At(%d).Input = %v, want %v", prompt, got, want)
		}
	}
}

func TestDecodeVersion(t *testing.T) {
	if _, err := Decode([]byte(`{"version": 1, "providers": {}, "models": {}}`)); !errors.Is(err, ErrVersion) {
		t.Errorf("Decode(version 1) = %v, want ErrVersion", err)
	}
}

// TestModelsDevBundle maps a full models.dev catalog.json, without and with
// the mapping's rules. It is skipped unless CATALOG_BUNDLE points at a
// downloaded one.
func TestModelsDevBundle(t *testing.T) {
	path := os.Getenv("CATALOG_BUNDLE")
	if path == "" {
		t.Skip("CATALOG_BUNDLE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := modelsdev.DecodeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	plain, rep, err := ListModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("models.dev alone: models %d, listings %d, bases %v", rep.Models, rep.Listings, rep.Bases)
	if len(plain.Models) != len(b.Models) || len(plain.Providers) != len(b.Providers) {
		t.Errorf("%d models and %d providers, want models.dev's %d and %d", len(plain.Models), len(plain.Providers), len(b.Models), len(b.Providers))
	}
	c, rep, err := ListModelsDev(b, Options{Infer: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("with the rules: models %d, listings %d, bases %v, merged %v, derived %v, notes %d",
		rep.Models, rep.Listings, rep.Bases, rep.Merged, rep.Derived, len(rep.Notes))
	for _, tt := range []struct {
		provider ProviderID
		id       string
		want     ModelID
	}{
		{"anthropic", "claude-opus-4-8", "anthropic/claude-opus-4-8"},
		{"vercel", "openai/gpt-6-astra-fast", "openai/gpt-6-astra"},
		{"llmgateway", "gpt-6-astra", "openai/gpt-6-astra"},
		{"openrouter", "openai/gpt-6-astra-pro", "openai/gpt-6-astra-pro"},
		{"openrouter", "~openai/gpt-astra-latest", "openai/gpt-astra-latest"},
		{"openrouter", "~openai/gpt-sol-latest", "openai/gpt-sol-latest"},
		{"opencode", "space-bunny-free", "opencode/space-bunny"},
		{"vercel", "bytedance/seedance-2.0-fast", "bytedance-seed/seedance-2.0-fast"},
	} {
		if o, ok := c.Listing(Ref{Provider: tt.provider, ID: tt.id}); !ok || o.Model != tt.want {
			t.Errorf("%s/%s serves %q (%v), want %q", tt.provider, tt.id, o.Model, ok, tt.want)
		}
	}
	if _, ok := c.Models["openai/gpt-6-astra-fast"]; ok {
		t.Error("openai/gpt-6-astra-fast is still a model")
	}
	if m, ok := c.Models["openai/gpt-6-astra-pro"]; !ok || m.Derived == nil ||
		!slices.Contains(slices.Collect(func(yield func(Ref) bool) {
			for r := range c.ListingsOf(m.ID) {
				if !yield(r) {
					return
				}
			}
		}), Ref{Provider: "openai", ID: "gpt-6-astra", Mode: "pro"}) {
		t.Errorf("openai/gpt-6-astra-pro = %+v, %v; want derived, served by OpenAI's gpt-6-astra in mode pro", m, ok)
	}
	// A "-latest" ID models.dev ties nowhere is a model of its own, with no
	// claim of what it serves; one it ties is an ID of that model.
	if m, ok := c.Models["openai/gpt-sol-latest"]; !ok || m.Name != "GPT Sol Latest" || m.Inferred == nil {
		t.Errorf("openai/gpt-sol-latest = %+v, %v; want an inferred model GPT Sol Latest", m, ok)
	}
	if o, ok := c.Listing(Ref{Provider: "fireworks-ai", ID: "accounts/fireworks/routers/glm-latest"}); ok && o.Basis != BasisCanonical {
		t.Errorf("fireworks-ai/accounts/fireworks/routers/glm-latest serves %q on %q, want models.dev's tie", o.Model, o.Basis)
	}
}
