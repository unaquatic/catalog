// Package modelsdev resolves model metadata from the models.dev catalog.
package modelsdev

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"
)

// DateLayout is the date format used by catalog release and update fields.
const DateLayout = "2006-01-02"

// ProviderID identifies a provider in the catalog.
type ProviderID string

// ModelID identifies a model within a provider.
type ModelID string

// Status reports the lifecycle state of a model. Entries without an explicit
// status are active.
type Status string

// Recognized model lifecycle states.
const (
	StatusActive     Status = "active"
	StatusAlpha      Status = "alpha"
	StatusBeta       Status = "beta"
	StatusDeprecated Status = "deprecated"
)

// Modality is a content kind a model accepts or produces.
type Modality string

// Recognized content modalities.
const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
	ModalityPDF   Modality = "pdf"
)

// APIShape names the request format a provider endpoint expects.
type APIShape string

// Recognized provider request formats.
const (
	ShapeCompletions APIShape = "completions"
	ShapeResponses   APIShape = "responses"
)

// ReasoningOptionType names a reasoning control exposed by a model.
type ReasoningOptionType string

// Recognized reasoning controls.
const (
	ReasoningEffort       ReasoningOptionType = "effort"
	ReasoningToggle       ReasoningOptionType = "toggle"
	ReasoningBudgetTokens ReasoningOptionType = "budget_tokens"
)

// Catalog maps provider identifiers to their catalog entries.
type Catalog map[ProviderID]Provider

// Clone returns a copy of the catalog whose maps and slices are independent of
// the original. Values reached through pointer fields are shared, so a cloned
// catalog protects against adding, removing and replacing entries but not
// against writing through a pointer such as Model.Cost.
func (c Catalog) Clone() Catalog {
	out := make(Catalog, len(c))
	for id, p := range c {
		out[id] = p.Clone()
	}
	return out
}

// Model returns the entry of model id of provider p, and reports whether the
// catalog lists it.
func (c Catalog) Model(p ProviderID, id ModelID) (Model, bool) {
	m, ok := c[p].Models[id]
	return m, ok
}

// Provider is a catalog provider and the models it offers.
type Provider struct {
	ID   ProviderID `json:"id"`
	Name string     `json:"name"`
	API  string     `json:"api,omitempty"`
	Env  []string   `json:"env,omitempty"`
	// NPM names the AI SDK package that talks to the provider, such as
	// "@ai-sdk/anthropic", which tells the request format its API takes.
	NPM string `json:"npm,omitempty"`
	// Doc links to the provider's documentation of its models.
	Doc    string            `json:"doc,omitempty"`
	Models map[ModelID]Model `json:"models"`
}

// Clone returns a copy of the provider with its own model map. See
// Catalog.Clone for the limits of the copy.
func (p Provider) Clone() Provider {
	p.Env = slices.Clone(p.Env)
	models := make(map[ModelID]Model, len(p.Models))
	for id, m := range p.Models {
		models[id] = m.Clone()
	}
	p.Models = models
	return p
}

// Model describes a single catalog model. Pointer fields are absent from the
// catalog rather than false or zero.
type Model struct {
	ID          ModelID `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Family      *string `json:"family,omitempty"`
	ReleaseDate string  `json:"release_date"`
	LastUpdated string  `json:"last_updated"`
	Knowledge   *string `json:"knowledge,omitempty"`

	Attachment       bool  `json:"attachment"`
	Reasoning        bool  `json:"reasoning"`
	ToolCall         bool  `json:"tool_call"`
	OpenWeights      bool  `json:"open_weights"`
	Temperature      *bool `json:"temperature,omitempty"`
	StructuredOutput *bool `json:"structured_output,omitempty"`

	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
	Interleaved      *Interleaved      `json:"interleaved,omitempty"`
	Modalities       Modalities        `json:"modalities"`
	Limit            Limit             `json:"limit"`
	Cost             *Cost             `json:"cost,omitempty"`
	Experimental     *Experimental     `json:"experimental,omitempty"`
	Status           *Status           `json:"status,omitempty"`
	Override         *Override         `json:"provider,omitempty"`

	// CanonicalModelID names the model as the lab that made it does, such
	// as "anthropic/claude-opus-5-5", for a provider that serves another
	// lab's model under a name of its own. It is empty when the catalog does
	// not say.
	CanonicalModelID string `json:"canonical_model_id,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type ModelType `json:"type,omitempty"`
}

// ModelType is a kind of model other than a language model.
type ModelType string

// Recognized model kinds.
const (
	ModelTypeEmbedding ModelType = "embedding"
	ModelTypeReranking ModelType = "reranking"
	ModelTypeVideo     ModelType = "video"
)

// Clone returns a copy of the model with its own slices and request overrides.
// See Catalog.Clone for the limits of the copy.
func (m Model) Clone() Model {
	m.ReasoningOptions = slices.Clone(m.ReasoningOptions)
	m.Modalities.Input = slices.Clone(m.Modalities.Input)
	m.Modalities.Output = slices.Clone(m.Modalities.Output)
	if m.Override != nil {
		o := *m.Override
		o.Body = maps.Clone(o.Body)
		o.Headers = maps.Clone(o.Headers)
		m.Override = &o
	}
	return m
}

// Modalities lists the content kinds a model accepts and produces.
type Modalities struct {
	Input  []Modality `json:"input"`
	Output []Modality `json:"output"`
}

// Limit reports model token limits. Input is absent when the catalog does not
// distinguish it from the context window.
type Limit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input,omitempty"`
	Output  float64  `json:"output"`
}

// PromptWindow returns the most tokens a prompt can hold: Input when the
// catalog lists it, and Context otherwise.
func (l Limit) PromptWindow() int {
	if l.Input != nil && *l.Input > 0 {
		return int(*l.Input)
	}
	return int(l.Context)
}

// Price lists per-token prices in catalog currency units.
type Price struct {
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	CacheRead   *float64 `json:"cache_read,omitempty"`
	CacheWrite  *float64 `json:"cache_write,omitempty"`
	Reasoning   *float64 `json:"reasoning,omitempty"`
	InputAudio  *float64 `json:"input_audio,omitempty"`
	OutputAudio *float64 `json:"output_audio,omitempty"`
}

// Cost is the base price of a model plus any context-dependent overrides.
type Cost struct {
	Price
	Tiers           []CostTier `json:"tiers,omitempty"`
	ContextOver200K *Price     `json:"context_over_200k,omitempty"`
}

// CostTier is a price that applies above a context threshold.
type CostTier struct {
	Price
	Tier Tier `json:"tier"`
}

// Tier is the threshold at which a cost tier takes effect.
type Tier struct {
	Type string  `json:"type"`
	Size float64 `json:"size"`
}

// TierContext is the Tier type whose Size is a prompt size in tokens.
const TierContext = "context"

// At returns the price of a request whose prompt holds prompt tokens: that
// of the context tier with the largest size the prompt exceeds, or the base
// price below every tier. A catalog without tiers can price prompts over
// 200,000 tokens in ContextOver200K.
func (c Cost) At(prompt int) Price {
	price, size := c.Price, 0.0
	for _, t := range c.Tiers {
		if t.Tier.Type == TierContext && float64(prompt) > t.Tier.Size && t.Tier.Size >= size {
			price, size = t.Price, t.Tier.Size
		}
	}
	if len(c.Tiers) == 0 && c.ContextOver200K != nil && prompt > 200_000 {
		return *c.ContextOver200K
	}
	return price
}

// Charge returns what a request costs, in catalog currency units, when it
// reads input tokens anew, reads cacheRead tokens from a prompt cache,
// writes cacheWrite tokens to one, and writes output tokens. Prices are per
// million tokens, and a cache price the catalog lacks is the input price.
func (p Price) Charge(input, cacheRead, cacheWrite, output int) float64 {
	read := cmp.Or(p.CacheRead, &p.Input)
	write := cmp.Or(p.CacheWrite, &p.Input)
	return (p.Input*float64(input) +
		*read*float64(cacheRead) +
		*write*float64(cacheWrite) +
		p.Output*float64(output)) / 1_000_000
}

// ReasoningOption describes one reasoning control. Values applies to effort
// controls; Min and Max apply to token budgets. A nil entry in Values means the
// control may be left unset.
type ReasoningOption struct {
	Type   ReasoningOptionType `json:"type"`
	Values []*string           `json:"values,omitempty"`
	Min    *float64            `json:"min,omitempty"`
	Max    *float64            `json:"max,omitempty"`
}

// Experimental holds unstable catalog extensions.
type Experimental struct {
	Modes map[string]Mode `json:"modes,omitempty"`
}

// Mode is an alternate billing and request configuration for a model.
type Mode struct {
	Cost     *Cost     `json:"cost,omitempty"`
	Override *Override `json:"provider,omitempty"`
}

// Override replaces provider-level request settings for a single model.
type Override struct {
	// NPM names the AI SDK package that talks to the model in place of the
	// provider's.
	NPM     string            `json:"npm,omitempty"`
	API     string            `json:"api,omitempty"`
	Shape   APIShape          `json:"shape,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Interleaved reports whether a model streams reasoning alongside content, and
// which response field carries it.
type Interleaved struct {
	// Enabled reports whether interleaved reasoning is supported.
	Enabled bool
	// Field names the response field carrying reasoning text. It is empty when
	// the catalog does not name one.
	Field string
}

// UnmarshalJSON decodes the boolean, string and object forms the catalog uses
// for interleaved reasoning.
func (i *Interleaved) UnmarshalJSON(data []byte) error {
	var enabled bool
	if err := json.Unmarshal(data, &enabled); err == nil {
		*i = Interleaved{Enabled: enabled}
		return nil
	}
	var field string
	if err := json.Unmarshal(data, &field); err == nil {
		*i = Interleaved{Enabled: true, Field: field}
		return nil
	}
	var obj struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("catalog: decode interleaved: %w", err)
	}
	*i = Interleaved{Enabled: true, Field: obj.Field}
	return nil
}

// MarshalJSON encodes the object form when a field is named and the boolean
// form otherwise, matching the catalog representation.
func (i Interleaved) MarshalJSON() ([]byte, error) {
	if i.Field == "" {
		return json.Marshal(i.Enabled)
	}
	return json.Marshal(struct {
		Field string `json:"field"`
	}{Field: i.Field})
}

// Lifecycle returns the model status, defaulting to StatusActive when the
// catalog omits one.
func (m Model) Lifecycle() Status {
	if m.Status == nil {
		return StatusActive
	}
	return *m.Status
}

// Released reports the model release date and whether it could be parsed.
func (m Model) Released() (time.Time, bool) {
	t, err := time.Parse(DateLayout, m.ReleaseDate)
	return t, err == nil
}

// Accepts reports whether the model takes input of the given modality.
func (m Model) Accepts(mod Modality) bool {
	return slices.Contains(m.Modalities.Input, mod)
}

// Produces reports whether the model emits output of the given modality.
func (m Model) Produces(mod Modality) bool {
	return slices.Contains(m.Modalities.Output, mod)
}

// Modes returns the names of the model's alternate modes, such as "fast",
// in sorted order, or none when the catalog lists none.
func (m Model) Modes() []string {
	if m.Experimental == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(m.Experimental.Modes))
}

// Mode returns the mode of the model named name, and reports whether the
// catalog lists it.
func (m Model) Mode(name string) (Mode, bool) {
	if m.Experimental == nil {
		return Mode{}, false
	}
	mode, ok := m.Experimental.Modes[name]
	return mode, ok
}

// CostIn returns what the model costs in mode, or in its standard mode when
// mode is empty: the mode's own cost when the catalog lists one, and the
// model's otherwise. It is nil when the catalog lists neither.
func (m Model) CostIn(mode string) *Cost {
	if md, ok := m.Mode(mode); ok && md.Cost != nil {
		return md.Cost
	}
	return m.Cost
}
