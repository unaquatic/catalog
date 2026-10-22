// Package gateway defines flow's model catalog and builds it from the
// models.dev catalog.
//
// models.dev keeps two lists. Its models (the site's /models page) are the
// labs' models, one per canonical ID such as "anthropic/claude-opus-5-5".
// Its providers each list the models they serve under their own IDs, such
// as Vercel's "anthropic/claude-opus-5.5", which names the model it serves
// in canonical_model_id; one without it, such as OpenCode's "space-bunny",
// is provider-specific.
//
// FromModelsDev maps the providers' lists to Listings first, one Listing
// per models.dev entry, tied to a model by models.dev or by rules of the
// mapping's own: by ID, as LLM Gateway's "gpt-6-astra" is
// openai/gpt-6-astra; as a tier, as Vercel's "openai/gpt-6-astra-fast";
// to a model derived for a mode, as GPT-6 Astra Pro; or to one derived for
// a "-latest" alias, as GPT Sol Latest. Each Listing's Basis says which.
//
// It then groups each provider's listings into one Offering per model,
// keyed by the model's ID. An Offering is one way to call the model, with
// the ProviderOptions to send: the provider's model ID, request fields, and
// an Endpoint when it is not the provider's. Its Variants are the tiers and
// modes it offers on top, by name, such as "fast"; its Aliases are the
// provider's other model IDs of the model. A provider or a listing whose
// request format the catalog does not know is left out, as no client could
// call it.
package gateway

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Version is the version of the catalog document format that this package
// writes and reads. Version 8 replaced a reasoning's toggle with mandatory,
// and added its default_enabled and default_effort.
const Version = 8

// ErrVersion reports a catalog document of a format version this package
// does not read.
var ErrVersion = errors.New("gateway: unsupported catalog version")

// ModelID is a models.dev model ID, as CatalogID writes it: the lab that
// made the model, a slash, and the model's ID at the lab, such as
// "anthropic/claude-opus-5-5" and "minimax/minimax-m2.7".
type ModelID string

// CatalogID returns id as the catalog writes the IDs it keys models and
// unlisted offerings by: in lower case, of segments joined by "/", each of
// a-z, 0-9, "." and "-" alone, a run of any other characters a "-", with no
// "-" at either end and no empty segment, so that Kilo's
// "cohere/north-mini-code:free" is "cohere/north-mini-code-free" and
// Workers AI's "@cf/cloudflare/clef" is "cf/cloudflare/clef". The catalog's
// IDs hold ":" only between a model's ID and its variant's key.
func CatalogID(id string) string {
	var segs []string
	for seg := range strings.SplitSeq(strings.ToLower(id), "/") {
		var b strings.Builder
		dash := false
		for _, r := range seg {
			if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '.' || r == '-' {
				b.WriteRune(r)
				dash = false
			} else if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
		if s := strings.Trim(b.String(), "-"); s != "" {
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// Spellings maps a word of model IDs, in lower case, to how model names
// spell it, as "glm" to "GLM" and "minimax" to "MiniMax". NameOf spells the
// words of a model's ID with them.
type Spellings map[string]string

// SpellingsOf returns the spellings of the words of names, as NameWords
// splits them: of each word, the spelling most names give it, and of a tie,
// the first in byte order. A word of letters and the number after it also
// spell the word an ID runs them together in: "Solar Pro 3" spells the "pro3"
// of upstage/solar-pro3 "Pro 3", unless more names write "Pro3".
func SpellingsOf(names iter.Seq[string]) Spellings {
	votes := make(map[string]map[string]int)
	vote := func(k, w string) {
		if votes[k] == nil {
			votes[k] = make(map[string]int)
		}
		votes[k][w]++
	}
	for n := range names {
		words := NameWords(n)
		for i, w := range words {
			vote(strings.ToLower(w), w)
			if i > 0 && letterWord.MatchString(words[i-1]) && number.MatchString(w) {
				vote(strings.ToLower(words[i-1]+w), words[i-1]+" "+w)
			}
		}
	}
	sp := make(Spellings, len(votes))
	for k, vs := range votes {
		var best string
		for _, w := range slices.Sorted(maps.Keys(vs)) {
			if best == "" || vs[w] > vs[best] {
				best = w
			}
		}
		sp[k] = best
	}
	return sp
}

// NameWords returns the words of model name n: its runs of characters other
// than spaces, hyphens, slashes, parentheses, colons and commas, so that
// "Qwen3-Coder 480B-A35B (Instruct)" has "Qwen3", "Coder", "480B", "A35B"
// and "Instruct".
func NameWords(n string) []string {
	return strings.FieldsFunc(n, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("-/(),:", r)
	})
}

var (
	// letterWord and number are a word of letters alone, as "Pro", and a
	// version number, as "3" or "3.5".
	letterWord = regexp.MustCompile(`^[A-Za-z]+$`)
	number     = regexp.MustCompile(`^\d+(\.\d+)?$`)
	// paramCount is a word that counts parameters, as "7b".
	paramCount = regexp.MustCompile(`^\d+(\.\d+)?[bm]$`)
	// versionTag is a word that tags a version, as "v2" or "v0.1".
	versionTag = regexp.MustCompile(`^v\d+(\.\d+)*$`)
	// oSeries is a word of OpenAI's o-series, as "o3".
	oSeries = regexp.MustCompile(`^o\d+$`)
)

// IDWords returns the words of part, the part of a catalog ID that names a
// model, in lower case, as Name names them: the parts between its hyphens,
// with a date, year first, run together, and a digit after a word that ends
// in one digit joined to it with a dot. "claude-opus-4-5-20251101" has
// "claude", "opus", "4.5" and "20251101"; "qwen2-5-vl-72b-instruct" has
// "qwen2.5", "vl", "72b" and "instruct".
func IDWords(part string) []string {
	var words []string
	parts := strings.FieldsFunc(part, func(r rune) bool { return r == '-' || r == '/' })
	for len(parts) > 0 {
		p := parts[0]
		if len(parts) >= 3 && digits(p, 4) && digits(parts[1], 2) && digits(parts[2], 2) {
			words, parts = append(words, p+parts[1]+parts[2]), parts[3:]
			continue
		}
		if n := len(words); n > 0 && digits(p, 1) && endsInDigit(words[n-1]) {
			words[n-1], parts = words[n-1]+"."+p, parts[1:]
			continue
		}
		words, parts = append(words, p), parts[1:]
	}
	return words
}

// Name returns the name of part, the part of a catalog ID that names a
// model: IDWords(part), in their order, each spelt by Spell, with GPT's
// hyphen kept before its number. "claude-opus-4-5-20251101" is "Claude Opus
// 4.5 20251101", "minimax-m2.7" "MiniMax M2.7", and "gpt-6.1-sol" "GPT-6.1
// Sol".
func (sp Spellings) Name(part string) string {
	var words []string
	for _, w := range IDWords(part) {
		w = sp.Spell(w)
		if n := len(words); n > 0 && strings.EqualFold(words[n-1], "GPT") && w[0] >= '0' && w[0] <= '9' {
			words[n-1] += "-" + w
			continue
		}
		words = append(words, w)
	}
	return strings.Join(words, " ")
}

// NameOf returns the name of model id: the Name of its part after its lab.
// The catalog names every model so, with its Spellings, and so a model's
// name always matches its ID: minimax/minimax-m2.7 is "MiniMax M2.7".
func NameOf(id ModelID, sp Spellings) string {
	_, own := SplitModelID(id)
	return sp.Name(own)
}

// UnlistedName returns the name of the unlisted offering keyed key: the
// Name of the key's last segment, which names the model where the others
// name its maker or host. "meta-llama/llama-4-scout" is "Llama 4 Scout".
func UnlistedName(key string, sp Spellings) string {
	return sp.Name(UnlistedPart(key))
}

// UnlistedPart returns the part of unlisted key that UnlistedName names: its
// last segment.
func UnlistedPart(key string) string {
	return key[strings.LastIndexByte(key, '/')+1:]
}

// VariantName returns the name of variant key of an offering named name:
// the offering's name and the key's words, each with a capital, as "GPT-6.1
// Sol Ultrafast" and "Claude Opus 5 Thinking Fast".
func VariantName(name, key string) string {
	words := []string{name}
	for w := range strings.SplitSeq(key, ":") {
		words = append(words, strings.ToUpper(w[:1])+w[1:])
	}
	return strings.Join(words, " ")
}

// Spell returns word w of a model ID as NameOf writes it:
//
//   - A version tag starts with a capital V: "v0.1" is "V0.1".
//   - Any other word is spelt as sp spells it, or as it is.
//   - A parameter count is in capitals: "7b" is "7B".
//   - A word in lower case then starts with a capital, but for OpenAI's
//     o-series: "mini" is "Mini", and "o3" stays "o3".
func (sp Spellings) Spell(w string) string {
	if versionTag.MatchString(w) {
		return "V" + w[1:]
	}
	w = cmp.Or(sp[w], w)
	if paramCount.MatchString(w) {
		return strings.ToUpper(w)
	}
	if r, size := utf8.DecodeRuneInString(w); 'a' <= r && r <= 'z' && !oSeries.MatchString(w) {
		w = strings.ToUpper(w[:size]) + w[size:]
	}
	return w
}

// For returns the spellings Name needs to name parts as with sp: those of
// their words that sp spells otherwise than Spell would with none.
func (sp Spellings) For(parts iter.Seq[string]) Spellings {
	out := make(Spellings)
	for part := range parts {
		for _, w := range IDWords(part) {
			if sp.Spell(w) != Spellings(nil).Spell(w) {
				out[w] = sp[w]
			}
		}
	}
	return out
}

// digits reports whether s is ASCII digits alone, n of them.
func digits(s string, n int) bool {
	return len(s) == n && strings.Trim(s, "0123456789") == ""
}

// endsInDigit reports whether s ends in one digit, and not two, as "qwen2"
// and "4" do.
func endsInDigit(s string) bool {
	n := len(s)
	return n > 0 && digits(s[n-1:], 1) && (n == 1 || !digits(s[n-2:n-1], 1))
}

// Lab returns the lab part of id, the text before its first slash.
func (id ModelID) Lab() ProviderID {
	before, _, _ := strings.Cut(string(id), "/")
	return ProviderID(before)
}

// SplitModelID returns the lab part of id and the model's ID within the lab,
// as Cut splits it at its first slash.
func SplitModelID(id ModelID) (lab ProviderID, model string) {
	before, after, _ := strings.Cut(string(id), "/")
	return ProviderID(before), after
}

// ProviderID identifies a provider, as models.dev names it.
type ProviderID string

// ProviderKind tells a lab, which serves the models it makes, from a
// gateway, which serves models that labs make.
type ProviderKind string

// Provider kinds.
const (
	KindLab     ProviderKind = "lab"
	KindGateway ProviderKind = "gateway"
)

// Tier is a service tier other than the standard one, which trades price for
// speed.
type Tier string

// Service tiers, slowest first.
const (
	// TierFlex is slower and cheaper than standard.
	TierFlex Tier = "flex"
	// TierFast is faster and dearer than standard: OpenAI's priority
	// processing and Anthropic's fast mode.
	TierFast Tier = "fast"
	// TierUltrafast is faster and dearer than fast.
	TierUltrafast Tier = "ultrafast"
	// TierFree is a model ID that a provider serves for free, usually at
	// lower rate limits, as Kenari's "deepseek-v4-flash:free".
	TierFree Tier = "free"
)

// Tiers are the service tiers a Variant can run in, in the order variants
// list them.
var Tiers = []Tier{TierFlex, TierFast, TierUltrafast, TierFree}

// ModeThinking is the mode of a model ID that names a thinking version of
// another, as 302.AI's "claude-opus-5-thinking".
const ModeThinking = "thinking"

// Modes are the modes a Variant can run the model in. A mode that runs a
// model of its own, as OpenAI's pro mode runs GPT-6.1 Sol Pro, is that
// model's offering, not a variant.
var Modes = []string{ModeThinking}

// Effort is a reasoning effort level, such as "high". "none" turns
// reasoning off.
type Effort string

// EffortNone is the effort that turns reasoning off.
const EffortNone Effort = "none"

// EffortName is a reasoning effort of the catalog's offerings, and its
// name, as "Extra High" of xhigh.
type EffortName struct {
	ID   Effort `json:"id"`
	Name string `json:"name"`
}

// VariantNames name the tiers and the modes of the catalog's variants.
type VariantNames struct {
	Tiers map[Tier]TierName   `json:"tiers,omitempty"`
	Modes map[string]ModeName `json:"modes,omitempty"`
}

// TierName is a service tier of the catalog's variants, and its name, as
// "Ultrafast" of ultrafast.
type TierName struct {
	ID   Tier   `json:"id"`
	Name string `json:"name"`
}

// ModeName is a mode of the catalog's variants, and its name, as "Thinking"
// of thinking.
type ModeName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Modality is a kind of content a model takes or makes, such as "text".
type Modality string

// ModelType is a kind of model other than a language model, such as
// "embedding".
type ModelType string

// Status is the lifecycle state of an offering. The empty Status is active.
type Status string

// Offering lifecycle states other than active.
const (
	StatusAlpha      Status = "alpha"
	StatusBeta       Status = "beta"
	StatusDeprecated Status = "deprecated"
)

// Catalog is the gateway catalog: the models by ID, and the providers, each
// with its offerings of the models by model ID.
type Catalog struct {
	// Schema is the URL of the JSON Schema of the catalog document,
	// SchemaURL, so that editors and validators find it.
	Schema string `json:"$schema,omitempty"`
	// Version is the format version, Version.
	Version int `json:"version"`
	// Source names where the catalog was built from, such as
	// "https://models.dev".
	Source string `json:"source,omitempty"`
	// Generated is when the catalog was built.
	Generated time.Time `json:"generated"`
	// Spellings spell the words of the catalog's IDs that Spell would spell
	// otherwise with none: every model's Name, and every offering's, is
	// NameOf its model's ID with them, and every unlisted offering's
	// UnlistedName of its key.
	Spellings Spellings `json:"spellings,omitempty"`
	// Labs and Families name the labs and the families of the catalog's
	// models, by the IDs the models' Lab and Family hold.
	Labs     map[ProviderID]Lab `json:"labs,omitempty"`
	Families map[string]Family  `json:"families,omitempty"`
	// Efforts name the reasoning efforts of the catalog's offerings, by the
	// IDs their Efforts hold, as EffortNames names them.
	Efforts map[Effort]EffortName `json:"efforts,omitempty"`
	// Variant names the tiers and the modes of the catalog's variants, by
	// the IDs their Tier and Mode hold, as VariantNames names them.
	Variant   *VariantNames           `json:"variant,omitempty"`
	Models    map[ModelID]Model       `json:"models"`
	Providers map[ProviderID]Provider `json:"providers"`
}

// Lab is a lab of the catalog's models, and its name, as "ByteDance Seed"
// of bytedance-seed.
type Lab struct {
	ID   ProviderID `json:"id"`
	Name string     `json:"name"`
}

// Family is a family of the catalog's models, and its name, as "Claude
// Opus" of claude-opus.
type Family struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NameOf returns the name of model id, as the catalog names its models,
// whether or not it lists id.
func (c *Catalog) NameOf(id ModelID) string {
	return NameOf(ModelID(CatalogID(string(id))), c.Spellings)
}

// UnlistedName returns the name of the unlisted offering keyed key, as the
// catalog names them, whether or not it lists one.
func (c *Catalog) UnlistedName(key string) string {
	return UnlistedName(CatalogID(key), c.Spellings)
}

// NameParts returns the parts of the catalog's IDs that its names are made
// of: each model's ID after its lab, and each unlisted offering's
// UnlistedPart.
func (c *Catalog) NameParts() iter.Seq[string] {
	return func(yield func(string) bool) {
		for id := range c.Models {
			if _, own := SplitModelID(id); !yield(own) {
				return
			}
		}
		for _, p := range c.Providers {
			for key := range p.Unlisted {
				if !yield(UnlistedPart(key)) {
					return
				}
			}
		}
	}
}

// Model returns the model id, and reports whether the catalog lists it. It
// ignores case, as the catalog's model IDs are all lower case.
func (c *Catalog) Model(id ModelID) (Model, bool) {
	m, ok := c.Models[ModelID(strings.ToLower(string(id)))]
	return m, ok
}

// All returns the models of the catalog in ID order.
func (c *Catalog) All() iter.Seq[Model] {
	return sortedValues(c.Models)
}

// AllProviders returns the providers of the catalog in ID order.
func (c *Catalog) AllProviders() iter.Seq[Provider] {
	return sortedValues(c.Providers)
}

// Offering returns provider p's offering of model m, and reports whether
// the catalog lists it. It ignores case, as Model does.
func (c *Catalog) Offering(p ProviderID, m ModelID) (Offering, bool) {
	o, ok := c.Providers[p].Models[ModelID(strings.ToLower(string(m)))]
	return o, ok
}

// sortedValues returns the values of m in key order.
func sortedValues[K cmp.Ordered, V any](m map[K]V) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			if !yield(m[k]) {
				return
			}
		}
	}
}

// Provider is a provider and what it serves: its offerings of the models,
// by model ID, and the models it lists that are none of the catalog's.
type Provider struct {
	ID   ProviderID   `json:"id"`
	Name string       `json:"name"`
	Kind ProviderKind `json:"kind"`
	// Endpoint is how to call the provider's API, unless a variant names
	// an endpoint of its own.
	Endpoint Endpoint `json:"endpoint"`
	// Doc links to the provider's documentation of its models.
	Doc string `json:"doc,omitempty"`
	// Models are the provider's offerings of the catalog's models, by model
	// ID.
	Models map[ModelID]Offering `json:"models"`
	// Unlisted are the provider's models that are none of the catalog's,
	// such as OpenCode's "space-bunny-free", by the provider's model ID.
	Unlisted map[string]Offering `json:"unlisted,omitempty"`
}

// Offering is a model as one provider serves it: the model's facts there,
// and how to call it: its ProviderOptions, its price, and the variants it
// offers on top.
type Offering struct {
	// Name is the catalog's name of the offering: its model's name, or, for
	// an unlisted offering, UnlistedName of its key. The provider's own
	// name is ProviderOptions.Name.
	Name   string `json:"name"`
	Family string `json:"family,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type         ModelType    `json:"type,omitempty"`
	Released     string       `json:"released"`
	Updated      string       `json:"updated"`
	Knowledge    string       `json:"knowledge,omitempty"`
	OpenWeights  bool         `json:"open_weights"`
	Capabilities Capabilities `json:"capabilities"`
	Limit        Limit        `json:"limit"`
	// Reasoning lists the reasoning controls, nil when the model does not
	// reason.
	Reasoning *Reasoning `json:"reasoning,omitempty"`
	// Interleaved is set when the provider streams reasoning between
	// content parts.
	Interleaved *Interleaved `json:"interleaved,omitempty"`
	// ProviderOptions say how to call the model: the provider's model ID,
	// and the endpoint, request fields and headers, where it takes them.
	ProviderOptions ProviderOptions `json:"provider_options"`
	// Price is what a call costs, nil when models.dev lists no price.
	Price  *Price `json:"price,omitempty"`
	Status Status `json:"status,omitempty"`
	// Basis says how the mapping tied the provider's model ID to the model,
	// and ModelsDev the model models.dev ties it to, as Listing has them.
	// The build reads them; the catalog document leaves them out.
	Basis     Basis   `json:"-"`
	ModelsDev ModelID `json:"-"`
	// Variants are the tiers and modes the provider offers on top of the
	// call, by VariantKey, such as "fast" and "thinking:fast".
	Variants map[string]Variant `json:"variants,omitempty"`
	// Aliases are the provider's other model IDs of the model, such as a
	// dated snapshot, another upstream of a gateway, or another region,
	// for callers to send as they see fit. The catalog lists no price or
	// variants of them.
	Aliases []string `json:"aliases,omitempty"`
	// Lab is, for an unlisted offering, the lab its ID names, as LabOf
	// reads it, when it names one.
	Lab ProviderID `json:"lab,omitempty"`
}

// Variant is a tier or mode that an offering offers on top of its call, or
// both at once, as a fast tier of a thinking mode. Its ProviderOptions
// change the offering's: an ID or Endpoint of its own replaces the
// offering's, and its Body and Headers merge into the offering's, as
// Resolve combines them. Price is the variant's own; one it lacks is the
// offering's.
type Variant struct {
	// Name is the catalog's name of the variant: its offering's name and
	// its key, as VariantName writes them, as "GPT-6.1 Sol Ultrafast". The
	// provider's own name, of a variant that sends a model ID of its own,
	// is ProviderOptions.Name.
	Name string `json:"name"`
	// Tier is the service tier the variant runs in, one of Tiers.
	Tier Tier `json:"tier,omitempty"`
	// Mode is the mode the variant runs the model in, one of Modes.
	Mode            string          `json:"mode,omitempty"`
	ProviderOptions ProviderOptions `json:"provider_options"`
	Price           *Price          `json:"price,omitempty"`
	Status          Status          `json:"status,omitempty"`
}

// Key returns the key of v in an offering's Variants: its mode and then its
// tier, joined by ":", as "thinking:fast".
func (v Variant) Key() string {
	return VariantKey(v.Mode, v.Tier)
}

// VariantKey returns the key of a variant in mode and tier, either empty:
// the mode and then the tier, joined by ":".
func VariantKey(mode string, tier Tier) string {
	switch {
	case mode == "":
		return string(tier)
	case tier == "":
		return mode
	}
	return mode + ":" + string(tier)
}

// VariantID returns the ID of variant key of the offering keyed model, the
// model alone for the offering's own call: model, ":" and the key, as
// "openai/gpt-6.1-sol:fast".
func VariantID(model, key string) string {
	if key == "" {
		return model
	}
	return model + ":" + key
}

// VariantKeys returns the keys of off's variants in the order variants
// list them: by mode, none first, and then by tier, in the order of Tiers.
func (off Offering) VariantKeys() []string {
	keys := slices.Collect(maps.Keys(off.Variants))
	slices.SortFunc(keys, func(x, y string) int {
		a, b := off.Variants[x], off.Variants[y]
		return cmp.Or(cmp.Compare(a.Mode, b.Mode), cmp.Compare(tierRank(a.Tier), tierRank(b.Tier)), cmp.Compare(x, y))
	})
	return keys
}

// tierRank orders tiers: none first, and then as Tiers lists them.
func tierRank(t Tier) int {
	if t == "" {
		return -1
	}
	if i := slices.Index(Tiers, t); i >= 0 {
		return i
	}
	return len(Tiers)
}

// ProviderOptions say how to send a call's requests to its provider.
type ProviderOptions struct {
	// ID is the provider's model ID to send.
	ID string `json:"id,omitempty"`
	// Name is the provider's own name for ID, as models.dev lists it, such
	// as DigitalOcean's "OpenAI GPT-6.1 Sol".
	Name string `json:"name,omitempty"`
	// Endpoint is how to call the variant, when it is not the provider's.
	Endpoint *Endpoint `json:"endpoint,omitempty"`
	// Body holds request fields to send, merged into the request deeply,
	// object by object; Headers holds headers to send.
	Body    map[string]any    `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Listings is what FromModelsDev maps models.dev's catalog to before it
// groups it into offerings: the models, and every provider's models as
// models.dev lists them, by the provider's model IDs.
type Listings struct {
	Models    map[ModelID]Model
	Providers map[ProviderID]ListedProvider
}

// Listing returns the listing ref names, and reports whether l lists it.
func (l *Listings) Listing(ref Ref) (Listing, bool) {
	o, ok := l.Providers[ref.Provider].Models[ref.ID]
	return o, ok
}

// EndpointOf returns how to call the listing ref names: its own endpoint,
// or else its provider's. It reports whether l lists it.
func (l *Listings) EndpointOf(ref Ref) (Endpoint, bool) {
	p, ok := l.Providers[ref.Provider]
	if !ok {
		return Endpoint{}, false
	}
	o, ok := p.Models[ref.ID]
	if !ok {
		return Endpoint{}, false
	}
	if o.Endpoint != nil {
		return *o.Endpoint, true
	}
	return p.Endpoint, true
}

// ListingsOf returns the listings that serve model m, in provider and then
// ID order, with the refs that name them: those tied to m, and those that
// run it in a mode, as OpenAI's gpt-6-astra runs GPT-6 Astra Pro.
func (l *Listings) ListingsOf(m ModelID) iter.Seq2[Ref, Listing] {
	return func(yield func(Ref, Listing) bool) {
		for p := range sortedValues(l.Providers) {
			for o := range sortedValues(p.Models) {
				if o.Model == m && !yield(Ref{Provider: p.ID, ID: o.ID}, o) {
					return
				}
				for _, mode := range slices.Sorted(maps.Keys(o.Modes)) {
					if o.Modes[mode].Model == m && !yield(Ref{Provider: p.ID, ID: o.ID, Mode: mode}, o) {
						return
					}
				}
			}
		}
	}
}

// ListedProvider is a models.dev provider and every model it lists, by the
// provider's model ID.
type ListedProvider struct {
	ID   ProviderID   `json:"id"`
	Name string       `json:"name"`
	Kind ProviderKind `json:"kind"`
	// Endpoint is how to call the provider's API, unless a listing has an
	// endpoint of its own.
	Endpoint Endpoint           `json:"endpoint"`
	Doc      string             `json:"doc,omitempty"`
	Models   map[string]Listing `json:"models"`
}

// Sorted returns p's listings in ID order.
func (p ListedProvider) Sorted() iter.Seq[Listing] {
	return sortedValues(p.Models)
}

// Ref names a listing: its provider, the provider's ID for it, and the
// mode it runs a model in, when it does, as OpenAI serves GPT-6 Astra Pro
// as gpt-6-astra in mode pro.
type Ref struct {
	Provider ProviderID `json:"provider"`
	ID       string     `json:"id"`
	Mode     string     `json:"mode,omitempty"`
}

// String returns ref as "provider/id", followed by " (mode)" for a mode.
func (ref Ref) String() string {
	s := string(ref.Provider) + "/" + ref.ID
	if ref.Mode != "" {
		s += " (" + ref.Mode + ")"
	}
	return s
}

// Model is a model as models.dev lists it at /models: as its lab describes
// it, apart from any provider, and the providers that serve it.
type Model struct {
	ID ModelID `json:"id"`
	// Lab is the lab that made the model, the part of ID before its first
	// slash.
	Lab    ProviderID `json:"lab"`
	Name   string     `json:"name"`
	Family string     `json:"family,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type ModelType `json:"type,omitempty"`
	// Released and Updated are dates, as YYYY-MM-DD, or YYYY-MM when only
	// the month is known. Every model has both.
	Released string `json:"released"`
	Updated  string `json:"updated"`
	// Knowledge is the training cutoff, as YYYY-MM or YYYY-MM-DD.
	Knowledge    string       `json:"knowledge,omitempty"`
	OpenWeights  bool         `json:"open_weights"`
	License      string       `json:"license,omitempty"`
	Reasoning    bool         `json:"reasoning"`
	Capabilities Capabilities `json:"capabilities"`
	Limit        Limit        `json:"limit"`
	Weights      []Weight     `json:"weights,omitempty"`
	Links        []Link       `json:"links,omitempty"`
	// Derived is set for a model models.dev does not list, which the
	// mapping derived from a model that one of its offerings runs in a
	// mode, as GPT-6 Astra Pro is GPT-6 Astra in mode pro. The build reads
	// it; the catalog document leaves it out.
	Derived *Derivation `json:"-"`
	// Merged lists the models.dev models merged into this one, as
	// models.dev's openai/gpt-6-astra-fast is the fast tier of
	// openai/gpt-6-astra.
	Merged []ModelID `json:"merged,omitempty"`
	// Inferred is set for a model models.dev does not list, which the
	// mapping made for providers' model IDs that name its lab, as
	// shareUnlisted makes them. The build reads it; the catalog document
	// leaves it out.
	Inferred *Inference `json:"-"`
	// Finetuned is set for an inferred model that tunes another, as
	// NanoGPT's "huihui-ai/Llama-3.3-70B-Instruct-abliterated" tunes
	// meta/llama-3.3-70b-instruct; its lab is the tuned model's. The build
	// reads it; the catalog document leaves it out.
	Finetuned *Finetuned `json:"-"`
	// Providers are the providers that offer the model, in ID order: each
	// has an Offering of it under the model's ID.
	Providers []ProviderID `json:"providers"`
}

// Inference says how the mapping made an inferred model: how it found the
// lab, and the provider whose listing gives the model's facts, the lab's
// own if it lists the model.
type Inference struct {
	LabBy LabBy      `json:"lab_by"`
	Facts ProviderID `json:"facts"`
}

// Derivation says what a derived model is: the model it runs, and the mode
// it runs in.
type Derivation struct {
	From ModelID `json:"from"`
	Mode string  `json:"mode"`
}

// Weight is where a model's weights can be downloaded.
type Weight struct {
	Label        string `json:"label,omitempty"`
	URL          string `json:"url"`
	Format       string `json:"format,omitempty"`
	Quantization string `json:"quantization,omitempty"`
}

// Link is a page about a model, such as its model card.
type Link struct {
	Label string `json:"label,omitempty"`
	URL   string `json:"url"`
	Type  string `json:"type,omitempty"`
}

// Listing is a model as one provider lists it on models.dev, under the
// provider's own model ID.
type Listing struct {
	// ID is the model ID that requests to the provider name.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Model is the model the offering serves, which Basis says how the
	// mapping found. It is empty for a provider-specific offering, which
	// neither models.dev nor the mapping's rules tie to a model.
	Model ModelID `json:"model,omitempty"`
	// Basis says how the mapping tied the offering to Model.
	Basis Basis `json:"basis,omitempty"`
	// Lab is the lab a provider-specific offering's ID names, as LabOf
	// reads it, or empty when it names none.
	Lab ProviderID `json:"lab,omitempty"`
	// ModelsDev is the model models.dev ties the offering to, by its
	// canonical_model_id or as a lab's own: Model, unless a rule moved
	// it, as from a merged model. It is empty when models.dev ties it to
	// none.
	ModelsDev ModelID `json:"models_dev,omitempty"`
	// Tier and TierOf are set when the ID names a tier of another offering
	// of the provider that serves the same model, as Vercel's
	// "openai/gpt-6.1-sol-fast" names the fast tier of
	// "openai/gpt-6.1-sol".
	Tier        Tier   `json:"tier,omitempty"`
	TierOf      string `json:"tier_of,omitempty"`
	Description string `json:"description,omitempty"`
	Family      string `json:"family,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type      ModelType `json:"type,omitempty"`
	Released  string    `json:"released,omitempty"`
	Updated   string    `json:"updated,omitempty"`
	Knowledge string    `json:"knowledge,omitempty"`
	Status    Status    `json:"status,omitempty"`
	// Endpoint is how to call the listing, whole, when it is not the
	// provider's, as Azure's claude-fable-5 takes Anthropic Messages at a
	// base URL of its own. Listings.EndpointOf resolves it.
	Endpoint     *Endpoint    `json:"endpoint,omitempty"`
	OpenWeights  bool         `json:"open_weights"`
	Capabilities Capabilities `json:"capabilities"`
	Limit        Limit        `json:"limit"`
	// Reasoning lists the offering's reasoning controls. It is nil when the
	// offering does not reason.
	Reasoning *Reasoning `json:"reasoning,omitempty"`
	// Interleaved is set when the provider streams reasoning between
	// content parts.
	Interleaved *Interleaved `json:"interleaved,omitempty"`
	// Price is what the provider charges for the standard tier, nil when
	// models.dev lists no price.
	Price *Price `json:"price,omitempty"`
	// Tiers are the service tiers the provider offers besides standard, as
	// models.dev's experimental modes describe them.
	Tiers map[Tier]Option `json:"tiers,omitempty"`
	// Modes are the other ways the provider can run the model, such as
	// "pro", by name. A mode the mapping derived a model for names it in
	// its Model.
	Modes map[string]Option `json:"modes,omitempty"`
}

// ProviderSpecific reports whether o is tied to no model.
func (o Listing) ProviderSpecific() bool {
	return o.Model == ""
}

// Inferred reports whether the mapping's own rules, not models.dev, tied o
// to its model.
func (o Listing) Inferred() bool {
	switch o.Basis {
	case BasisID, BasisTier, BasisPro, BasisInferred, BasisRule:
		return true
	}
	return false
}

// Capabilities lists what a model can do.
type Capabilities struct {
	Tools       bool `json:"tools"`
	Attachments bool `json:"attachments"`
	// StructuredOutput and Temperature are nil when models.dev does not say.
	StructuredOutput *bool      `json:"structured_output,omitempty"`
	Temperature      *bool      `json:"temperature,omitempty"`
	Input            []Modality `json:"input"`
	Output           []Modality `json:"output"`
}

// Limit is a model's token limits. Input is zero when it is the context
// window.
type Limit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output"`
}

// Reasoning lists the reasoning controls of an offering, and how its model
// reasons when a request sets none: models.dev's controls, and the facts
// the rules set, as ApplyReasoning does. A nil fact is one neither lists.
type Reasoning struct {
	// Mandatory reports whether reasoning cannot be turned off: a rule's,
	// or else false when models.dev lists a toggle or an effort "none".
	Mandatory *bool `json:"mandatory,omitempty"`
	// DefaultEnabled reports whether the model reasons when a request says
	// nothing of reasoning, as a rule says.
	DefaultEnabled *bool `json:"default_enabled,omitempty"`
	// Efforts are the effort levels the offering takes, least first, as its
	// provider names them. "none", first, turns reasoning off, however the
	// provider's API does: the catalog lists it whenever reasoning can be
	// turned off, as ListOff does.
	Efforts []Effort `json:"efforts,omitempty"`
	// DefaultEffort is the effort the model reasons at when a request names
	// none, as a rule says, when Efforts lists it.
	DefaultEffort Effort `json:"default_effort,omitempty"`
	// Budget is the range of reasoning token budgets the offering takes, nil
	// when it takes none.
	Budget *Budget `json:"budget,omitempty"`
}

// Budget is a range of reasoning token budgets. A zero bound is unknown.
type Budget struct {
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`
}

// Interleaved describes how a provider streams reasoning between content
// parts.
type Interleaved struct {
	// Field names the response field that carries the reasoning text, or is
	// empty when models.dev does not name one.
	Field string `json:"field,omitempty"`
}

// Option is a choice that an offering offers on top of its standard
// request: a service tier or a mode. A request that takes it merges Body
// into the request body deeply, object by object, so that an option's
// {"reasoning": {"mode": "pro"}} keeps the request's other reasoning
// fields; and it adds Headers.
type Option struct {
	// Model is the model a mode runs, when the mapping derived one for it:
	// OpenAI's gpt-6.1-sol in mode pro runs openai/gpt-6.1-sol-pro, not the
	// offering's model, and lists among that model's offerings. It is
	// empty for a tier, and for a mode of no model of its own.
	Model   ModelID           `json:"model,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Price is what the option costs, nil when models.dev lists no price
	// of its own for it: then it costs what the offering does, as OpenAI's
	// pro mode costs what the model does.
	Price *Price `json:"price,omitempty"`
}

// Rates are prices in US dollars per million tokens. A nil rate is not
// listed.
type Rates struct {
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	CacheRead   *float64 `json:"cache_read,omitempty"`
	CacheWrite  *float64 `json:"cache_write,omitempty"`
	Reasoning   *float64 `json:"reasoning,omitempty"`
	InputAudio  *float64 `json:"input_audio,omitempty"`
	OutputAudio *float64 `json:"output_audio,omitempty"`
}

// Price is what an offering or an option costs: its rates, and the rates
// of longer prompts.
type Price struct {
	Rates
	// Tiers are the rates of prompts longer than a size, by size.
	Tiers []PriceTier `json:"tiers,omitempty"`
}

// PriceTier is the rates of prompts of more than Above tokens.
type PriceTier struct {
	Above int `json:"above"`
	Rates
	// Estimated reports that models.dev lists no such rates and the
	// mapping derived them, scaling the standard tier's long-prompt rates
	// as the option scales its short-prompt rates.
	Estimated bool `json:"estimated,omitempty"`
}

// At returns the rates of a prompt of prompt tokens: those of the largest
// tier the prompt is longer than, or p's own below every tier.
func (p Price) At(prompt int) Rates {
	rates, above := p.Rates, -1
	for _, t := range p.Tiers {
		if prompt > t.Above && t.Above > above {
			rates, above = t.Rates, t.Above
		}
	}
	return rates
}

// Encode returns c as an indented JSON document ending in a newline, as the
// build command writes it.
func Encode(c *Catalog) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("gateway: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode parses a catalog document, which must be of format Version.
func Decode(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("gateway: decode: %w", err)
	}
	if c.Version != Version {
		return nil, fmt.Errorf("%w: %d, want %d", ErrVersion, c.Version, Version)
	}
	return &c, nil
}
