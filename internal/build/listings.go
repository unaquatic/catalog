package build

import (
	"iter"
	"maps"
	"slices"

	"github.com/unaquatic/catalog/gateway"
)

// Listings is what FromModelsDev maps models.dev's catalog to before it
// groups it into offerings: the models, and every provider's models as
// models.dev lists them, by the provider's model IDs.
type Listings struct {
	Models    map[gateway.ModelID]gateway.Model
	Providers map[gateway.ProviderID]ListedProvider
}

// Listing returns the listing ref names, and reports whether l lists it.
func (l *Listings) Listing(ref Ref) (Listing, bool) {
	o, ok := l.Providers[ref.Provider].Models[ref.ID]
	return o, ok
}

// EndpointOf returns how to call the listing ref names: its own endpoint,
// or else its provider's. It reports whether l lists it.
func (l *Listings) EndpointOf(ref Ref) (gateway.Endpoint, bool) {
	p, ok := l.Providers[ref.Provider]
	if !ok {
		return gateway.Endpoint{}, false
	}
	o, ok := p.Models[ref.ID]
	if !ok {
		return gateway.Endpoint{}, false
	}
	if o.Endpoint != nil {
		return *o.Endpoint, true
	}
	return p.Endpoint, true
}

// ListingsOf returns the listings that serve model m, in provider and then
// ID order, with the refs that name them: those tied to m, and those that
// run it in a mode, as OpenAI's gpt-6-astra runs GPT-6 Astra Pro.
func (l *Listings) ListingsOf(m gateway.ModelID) iter.Seq2[Ref, Listing] {
	return func(yield func(Ref, Listing) bool) {
		for p := range sortedOf(l.Providers) {
			for o := range sortedOf(p.Models) {
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
	ID   gateway.ProviderID   `json:"id"`
	Name string               `json:"name"`
	Kind gateway.ProviderKind `json:"kind"`
	Type gateway.ProviderType `json:"type"`
	// Endpoint is how to call the provider's API, unless a listing has an
	// endpoint of its own.
	Endpoint gateway.Endpoint   `json:"endpoint"`
	Doc      string             `json:"doc,omitempty"`
	Models   map[string]Listing `json:"models"`
}

// Sorted returns p's listings in ID order.
func (p ListedProvider) Sorted() iter.Seq[Listing] {
	return sortedOf(p.Models)
}

// Ref names a listing: its provider, the provider's ID for it, and the
// mode it runs a model in, when it does, as OpenAI serves GPT-6 Astra Pro
// as gpt-6-astra in mode pro.
type Ref struct {
	Provider gateway.ProviderID `json:"provider"`
	ID       string             `json:"id"`
	Mode     string             `json:"mode,omitempty"`
}

// String returns ref as "provider/id", followed by " (mode)" for a mode.
func (ref Ref) String() string {
	s := string(ref.Provider) + "/" + ref.ID
	if ref.Mode != "" {
		s += " (" + ref.Mode + ")"
	}
	return s
}

// Inference says how the mapping made an inferred model: how it found the
// lab, and the provider whose listing gives the model's facts, the lab's
// own if it lists the model.
type Inference struct {
	LabBy LabBy              `json:"lab_by"`
	Facts gateway.ProviderID `json:"facts"`
}

// Derivation says what a derived model is: the model it runs, and the mode
// it runs in.
type Derivation struct {
	From gateway.ModelID `json:"from"`
	Mode string          `json:"mode"`
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
	Model gateway.ModelID `json:"model,omitempty"`
	// Basis says how the mapping tied the offering to Model.
	Basis Basis `json:"basis,omitempty"`
	// Lab is the lab a provider-specific offering's ID names, as LabOf
	// reads it, or empty when it names none.
	Lab gateway.ProviderID `json:"lab,omitempty"`
	// ModelsDev is the model models.dev ties the offering to, by its
	// canonical_model_id or as a lab's own: Model, unless a rule moved
	// it, as from a merged model. It is empty when models.dev ties it to
	// none.
	ModelsDev gateway.ModelID `json:"models_dev,omitempty"`
	// Tier and TierOf are set when the ID names a tier of another offering
	// of the provider that serves the same model, as Vercel's
	// "openai/gpt-6.1-sol-fast" names the fast tier of
	// "openai/gpt-6.1-sol".
	Tier        gateway.Tier `json:"tier,omitempty"`
	TierOf      string       `json:"tier_of,omitempty"`
	Description string       `json:"description,omitempty"`
	Family      string       `json:"family,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type      gateway.ModelType `json:"type,omitempty"`
	Released  string            `json:"released,omitempty"`
	Updated   string            `json:"updated,omitempty"`
	Knowledge string            `json:"knowledge,omitempty"`
	Status    gateway.Status    `json:"status,omitempty"`
	// Endpoint is how to call the listing, whole, when it is not the
	// provider's, as Azure's claude-fable-5 takes Anthropic Messages at a
	// base URL of its own. Listings.EndpointOf resolves it.
	Endpoint     *gateway.Endpoint    `json:"endpoint,omitempty"`
	OpenWeights  bool                 `json:"open_weights"`
	Capabilities gateway.Capabilities `json:"capabilities"`
	Limit        gateway.Limit        `json:"limit"`
	// Reasoning lists the offering's reasoning controls. It is nil when the
	// offering does not reason.
	Reasoning *gateway.Reasoning `json:"reasoning,omitempty"`
	// Interleaved is set when the provider streams reasoning between
	// content parts.
	Interleaved *gateway.Interleaved `json:"interleaved,omitempty"`
	// Price is what the provider charges for the standard tier, nil when
	// models.dev lists no price.
	Price *gateway.Price `json:"price,omitempty"`
	// Tiers are the service tiers the provider offers besides standard, as
	// models.dev's experimental modes describe them.
	Tiers map[gateway.Tier]Option `json:"tiers,omitempty"`
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
	Model   gateway.ModelID   `json:"model,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Price is what the option costs, nil when models.dev lists no price
	// of its own for it: then it costs what the offering does, as OpenAI's
	// pro mode costs what the model does.
	Price *gateway.Price `json:"price,omitempty"`
}
