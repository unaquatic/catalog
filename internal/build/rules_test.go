package build

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

// withRules returns rules.json with extra rules first, and drop's rules
// left out, as one rules document.
func withRules(t *testing.T, extra []string, drop ...string) *Rules {
	t.Helper()
	doc := string(rulesJSON)
	for _, d := range drop {
		if !strings.Contains(doc, d) {
			t.Fatalf("rules.json lacks %s", d)
		}
		doc = strings.Replace(doc, d, "", 1)
	}
	doc = strings.Replace(doc, `"rules": [`, `"rules": [`+strings.Join(append(extra, ""), ",\n"), 1)
	r, err := DecodeRules([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestDefaultRules checks that rules.json decodes, and that its rules answer
// each step's question as the step expects.
func TestDefaultRules(t *testing.T) {
	r := DefaultRules()
	if lab, ok := r.MakerLab("x-ai"); !ok || lab != "xai" {
		t.Errorf("MakerLab(x-ai) = %s, %v", lab, ok)
	}
	if !r.Added("voyage") || r.Added("openai") {
		t.Error("Added: want voyage, a lab models.dev lacks, and not openai")
	}
	for s, want := range map[string]gateway.ProviderID{"seedance-2.0": "bytedance-seed", "seed-1-6": "bytedance-seed", "grok-3": "xai", "gpt-oss-120b": "openai"} {
		if lab, ok := r.BrandLab(s); !ok || lab != want {
			t.Errorf("BrandLab(%s) = %s, %v; want %s", s, lab, ok, want)
		}
	}
	if _, ok := r.BrandLab("grokker"); ok {
		t.Error("BrandLab(grokker): a brand is followed by its end, a dash, a dot or a digit")
	}
	if labs := slices.Collect(r.ProductLabs("inkling-256k")); !slices.Equal(labs, []gateway.ProviderID{"thinkingmachines"}) {
		t.Errorf("ProductLabs(inkling-256k) = %v", labs)
	}
	for w, want := range map[string]gateway.ProviderID{"l3.3": "meta", "mn": "mistral", "qwen2.5": "alibaba", "gemma3": "google"} {
		if lab, ok := r.OpenLab(w); !ok || lab != want {
			t.Errorf("OpenLab(%s) = %s, %v; want %s", w, lab, ok, want)
		}
	}
	if _, ok := r.OpenLab("gpt5"); ok {
		t.Error("OpenLab(gpt5): GPT's weights are not open")
	}
	if !r.HostSegment("accounts") || r.HostSegment("meta-llama") {
		t.Error("HostSegment: want accounts, and not meta-llama")
	}
	if got := slices.Collect(r.HostPrefixes("gitlab")); !slices.Equal(got, []string{"duo-chat-"}) {
		t.Errorf("HostPrefixes(gitlab) = %v", got)
	}
	if fb, ok := r.FinetuneBase("huihui-ai/Llama-3.3-70B-Instruct-abliterated"); !ok || fb.Base != "meta-llama/Llama-3.3-70B-Instruct" {
		t.Errorf("FinetuneBase = %+v, %v", fb, ok)
	}
	if !r.FinetuneWord("qwen/qwen3.8-27b-uncensored", "") || !r.FinetuneWord("x", "Dark Idol RP") || r.FinetuneWord("qwen/qwen3.8-27b", "Qwen3.8 27B") {
		t.Error("FinetuneWord: want the uncensored ID and the RP name, not Qwen's own")
	}
	if !r.Quantized("Qwen3-32B-FP8") || r.Quantized("qwen3-32b") || !r.FinetuneDescription("a creative finetune") {
		t.Error("Quantized and FinetuneDescription")
	}
	for _, tt := range []struct {
		id, name, desc string
		want           Service
	}{
		{"openrouter/auto", "", "", ServiceRouter},
		{"x", "Pareto Code Router", "", ServiceRouter},
		{"gw/zdr", "", "GW privacy routing alias that prefers zero data retention.", ServicePreset},
		{"universal-summarizer", "Universal Summarizer", "", ServiceTool},
		{"bee-buzz", "Bee Buzz", "Mid-tier model for risk-aware routing workflows", ""},
	} {
		if got := r.ServiceOf(tt.id, tt.name, tt.desc); got != tt.want {
			t.Errorf("ServiceOf(%s, %s, %s) = %q, want %q", tt.id, tt.name, tt.desc, got, tt.want)
		}
	}
	if base, tier, ok := r.CutTier("gpt-6-astra-fast"); !ok || base != "gpt-6-astra" || tier != gateway.TierFast {
		t.Errorf("CutTier = %s, %s, %v", base, tier, ok)
	}
	if _, _, ok := r.CutTier("glm-5-free"); ok {
		t.Error("CutTier cut the free tier, which is no suffix of a model's own")
	}
	if !slices.Equal(r.FreeSuffixes(), []string{":free", "-free"}) || !slices.Contains(r.ThinkingSuffixes(), "-think") ||
		!slices.Contains(r.NoThinkingSuffixes(), "-nothink") {
		t.Error("suffixes of free, thinking and no-thinking IDs")
	}
	if got := r.StripRegion("us-gov-anthropic-claude"); got != "anthropic-claude" {
		t.Errorf("StripRegion = %s, want the longest region off", got)
	}
	listed := slices.Collect(r.Listed())
	if !slices.Contains(listed, "anthropic") || slices.Contains(listed, "nano-gpt") || len(slices.Compact(slices.Sorted(slices.Values(listed)))) != len(listed) {
		t.Errorf("Listed = %v; want anthropic, not nano-gpt, each once", listed)
	}
	keep, missing := r.ListedIn(loadFixture(t))
	if want := []gateway.ProviderID{"anthropic", "github-copilot", "openai", "opencode", "openrouter", "vercel"}; !slices.Equal(keep, want) {
		t.Errorf("ListedIn kept %v, want %v", keep, want)
	}
	if !slices.Contains(missing, "zai") || len(keep)+len(missing) != len(listed) {
		t.Errorf("ListedIn missing %v; want every listed provider the fixture lacks", missing)
	}
}

// TestDecodeRulesErrors checks that DecodeRules refuses a document it cannot
// map by, naming the rule.
func TestDecodeRulesErrors(t *testing.T) {
	for _, tt := range []struct {
		name, doc, want string
	}{
		{"version", `{"version": 2, "rules": []}`, "version 2"},
		{"unknown field", `{"version": 1, "rules": [{"match": {"maker": "x"}, "lab": "y", "colour": "red"}]}`, "colour"},
		{"says nothing", `{"version": 1, "rules": [{"match": {"maker": "x"}}]}`, "rule 1: says 0 things"},
		{"says two things", `{"version": 1, "rules": [{"add_lab": "x"}, {"match": {"segment": "x"}, "skip": true, "strip": true}]}`, "rule 2: says 2 things"},
		{"lab without a match", `{"version": 1, "rules": [{"lab": "x"}]}`, "needs a match of maker, product, brand or word"},
		{"open maker", `{"version": 1, "rules": [{"match": {"maker": "x"}, "lab": "y", "open": true}]}`, "open marks a brand or a word"},
		{"bad pattern", `{"version": 1, "rules": [{"match": {"id": "("}, "service": "router"}]}`, "missing closing )"},
		{"unknown tier", `{"version": 1, "rules": [{"match": {"suffix": "-turbo"}, "tier": "turbo"}]}`, `tier "turbo"`},
		{"strip without a prefix", `{"version": 1, "rules": [{"match": {"provider": "gitlab"}, "strip": true}]}`, "strip needs a match of prefix"},
		{"model without a listing", `{"version": 1, "rules": [{"match": {"provider": "x"}, "model": "a/b"}]}`, "a model needs a match of listing"},
		{"separator without a provider", `{"version": 1, "rules": [{"separator": "--"}]}`, "a separator needs a match of provider"},
		{"exclude without a provider", `{"version": 1, "rules": [{"match": {"maker": "x"}, "exclude": true}]}`, "exclude needs a match of provider"},
		{"official without a provider", `{"version": 1, "rules": [{"match": {"maker": "x"}, "official": true}]}`, "official needs a match of provider"},
		{"name without a match", `{"version": 1, "rules": [{"name": "X"}]}`, "a name needs a match of one provider, lab, family, effort, tier or mode"},
		{"name of a lab and a family", `{"version": 1, "rules": [{"match": {"lab": "x", "family": "y"}, "name": "X"}]}`, "a name needs a match of one provider, lab, family, effort, tier or mode"},
		{"name of two providers", `{"version": 1, "rules": [{"match": {"provider": ["x", "y"]}, "name": "X"}]}`, "a name needs a match of one provider, lab, family, effort, tier or mode"},
		{"name of a provider and a lab", `{"version": 1, "rules": [{"match": {"provider": "x", "lab": "x"}, "name": "X"}]}`, "a name needs a match of one provider, lab, family, effort, tier or mode"},
		{"effort match for no name", `{"version": 1, "rules": [{"match": {"effort": "xhigh"}, "exclude": true}]}`, "a match of lab, family, effort, tier or mode is for a name"},
		{"lab match for no name", `{"version": 1, "rules": [{"match": {"lab": "x"}, "add_lab": "y"}]}`, "a match of lab, family, effort, tier or mode is for a name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeRules([]byte(tt.doc))
			if !errors.Is(err, ErrRules) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want ErrRules saying %q", err, tt.want)
			}
		})
	}
	if r, err := DecodeRules([]byte(`{"version": 1, "rules": [{"match": {"provider": ["a", "b"]}, "listed": true}, {"match": {"provider": "c"}, "listed": true}]}`)); err != nil ||
		!slices.Equal(slices.Collect(r.Listed()), []gateway.ProviderID{"a", "b", "c"}) {
		t.Errorf("a provider list and a provider alone: %v, %v", r, err)
	}
}

// TestExclude checks that a provider the rules exclude is in no catalog,
// whichever providers a build keeps, and that the rules built in exclude
// Venice.
func TestExclude(t *testing.T) {
	if !DefaultRules().Excluded("venice") || DefaultRules().Excluded("openai") {
		t.Error("rules.json excludes venice, and not openai")
	}
	opts := fixtureOptions(true)
	opts.Rules = withRules(t, []string{`{"match": {"provider": "opencode"}, "exclude": true}`})
	c, _, err := FromModelsDev(loadFixture(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Providers["opencode"]; ok || len(c.Providers) != len(fixtureProviders)-1 {
		t.Errorf("providers %v; want all of the fixture's but opencode", slices.Sorted(maps.Keys(c.Providers)))
	}
	if _, ok := c.Models["opencode/big-pickle"]; ok {
		t.Error("opencode/big-pickle, OpenCode's own model, is in a catalog that excludes OpenCode")
	}
	opts.Providers = []gateway.ProviderID{"opencode"}
	if _, _, err := FromModelsDev(loadFixture(t), opts); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("keeping an excluded provider: err = %v, want ErrUnknownProvider", err)
	}
	// A model of an excluded provider's own, at another provider, stays: GW's
	// "inkling-256k" is still Thinking Machines' product.
	b, err := modelsdev.DecodeBundle([]byte(ownDoc))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = FromModelsDev(b, Options{Infer: true, Rules: withRules(t, []string{`{"match": {"provider": "thinkingmachines"}, "exclude": true}`})})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := c.Models["thinkingmachines/inkling-256k"]; !ok || !slices.Equal(m.Providers, []gateway.ProviderID{"gw"}) {
		t.Errorf("thinkingmachines/inkling-256k = %+v, %v; want it kept, served by gw", m, ok)
	}
}

// TestOfficial checks that a provider an official rule names is official,
// labs and gateways alike, that every other provider is community, and
// that a rule naming a provider models.dev lacks gets a note.
func TestOfficial(t *testing.T) {
	if !DefaultRules().Official("anthropic") || DefaultRules().Official("vercel") {
		t.Error("rules.json makes anthropic official, and not vercel")
	}
	opts := fixtureOptions(true)
	opts.Rules = withRules(t, []string{`{"match": {"provider": ["vercel", "nowhere"]}, "official": true}`})
	c, rep, err := FromModelsDev(loadFixture(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[gateway.ProviderID]gateway.ProviderType{
		"anthropic": gateway.ProviderOfficial, "openrouter": gateway.ProviderOfficial, "vercel": gateway.ProviderOfficial, "llmgateway": gateway.ProviderCommunity,
	} {
		if got := c.Providers[id].Type; got != want {
			t.Errorf("%s type = %q, want %q", id, got, want)
		}
	}
	if !slices.ContainsFunc(rep.Notes, func(n Note) bool { return n.Provider == "nowhere" && strings.Contains(n.Text, "official rule") }) {
		t.Errorf("notes %v; want one of the official rule naming nowhere", rep.Notes)
	}
}

// TestRulesDriveTheMapping checks that the rules a build is given decide
// what the steps find: a rule ties one listing to a model, and without the
// rule that names bytedance as bytedance-seed, Vercel's
// "bytedance/seedance-2.0-fast" names no lab, and is left out.
func TestRulesDriveTheMapping(t *testing.T) {
	r := withRules(t, []string{
		`{"match": {"provider": "openrouter", "listing": "~google/gemini-pro-latest"}, "model": "openai/gpt-6-astra"}`,
		`{"match": {"provider": "openrouter", "listing": "anthropic/claude-opus-4.8"}, "model": "acme/nothing"}`,
	}, `{"match": {"maker": "bytedance"}, "lab": "bytedance-seed"},`)
	opts := fixtureOptions(true)
	opts.Rules = r
	c, rep, err := ListModelsDev(loadFixture(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if o := mustListing(t, c, "openrouter", "~google/gemini-pro-latest"); o.Model != "openai/gpt-6-astra" || o.Basis != BasisRule || !o.Inferred() {
		t.Errorf("pinned listing = %s, %s; want openai/gpt-6-astra by a rule", o.Model, o.Basis)
	}
	if !slices.ContainsFunc(rep.Notes, func(n Note) bool { return strings.Contains(n.Text, "a rule ties it to acme/nothing") }) {
		t.Errorf("no note of a rule naming a model the catalog lacks in %v", rep.Notes)
	}
	seedance := Ref{Provider: "vercel", ID: "bytedance/seedance-2.0-fast"}
	if o, ok := c.Listing(seedance); ok {
		t.Errorf("seedance 2.0 fast serves %q without the rule that names bytedance as bytedance-seed", o.Model)
	}
	plain, _ := build(t, true)
	if o, ok := plain.Listing(seedance); !ok || o.Model != "bytedance-seed/seedance-2.0-fast" {
		t.Errorf("with rules.json, seedance 2.0 fast serves %q, want bytedance-seed/seedance-2.0-fast", o.Model)
	}
}
