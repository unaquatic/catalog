// Package gateway defines flow's model catalog, the document that
// https://aquatic.dev/catalog/catalog.json serves, and reads it.
//
// A Catalog holds the models, one per ID such as
// "anthropic/claude-opus-5-5", and the providers, each with its Offering of
// every model it serves, keyed by the model's ID. An Offering is one way to
// call the model, with the ProviderOptions to send: the provider's model
// ID, request fields, and an Endpoint when it is not the provider's. Its
// Variants are the tiers and modes it offers on top, by name, such as
// "fast"; its Aliases are the provider's other model IDs of the model.
// Resolve and Variants combine an offering and a variant into the call a
// client sends.
//
// Decode reads a catalog document, and a Store keeps one downloaded from
// DefaultSource, cached in a file. SchemaDocument is the document's JSON
// Schema.
package gateway

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"
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

// Spellings maps a word of model IDs, in lower case, to how model names
// spell it, as "glm" to "GLM" and "minimax" to "MiniMax". NameOf spells the
// words of a model's ID with them.
type Spellings map[string]string

// Lab returns the lab part of id, the text before its first slash.
func (id ModelID) Lab() ProviderID {
	before, _, _ := strings.Cut(string(id), "/")
	return ProviderID(before)
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

// ProviderType tells a provider the rules select as official from a
// community one, as every other provider models.dev lists is.
type ProviderType string

// Provider types.
const (
	ProviderOfficial  ProviderType = "official"
	ProviderCommunity ProviderType = "community"
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
	// Type is official for a provider an official rule names, and
	// community for any other.
	Type ProviderType `json:"type"`
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
	// Released and Updated are dates, as YYYY-MM-DD; a catalog of a month
	// alone breaks the schema. Every model has both.
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
	// Merged lists the models.dev models merged into this one, as
	// models.dev's openai/gpt-6-astra-fast is the fast tier of
	// openai/gpt-6-astra.
	Merged []ModelID `json:"merged,omitempty"`
	// Providers are the providers that offer the model, in ID order: each
	// has an Offering of it under the model's ID.
	Providers []ProviderID `json:"providers"`
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
