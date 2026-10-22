package build

import "github.com/unaquatic/catalog/gateway"

// TuneKind says how a fine-tune is made from the model it tunes.
type TuneKind string

// The ways a fine-tune is made, as Hugging Face's base_model tags name them.
const (
	// TuneFinetune is trained further.
	TuneFinetune TuneKind = "finetune"
	// TuneMerge merges the model with others.
	TuneMerge TuneKind = "merge"
	// TuneQuantized stores the model at a lower precision.
	TuneQuantized TuneKind = "quantized"
	// TuneAdapter is an adapter, such as a LoRA, on top of the model.
	TuneAdapter TuneKind = "adapter"
)

// FinetuneBase is the model a fine-tune tunes, as its maker names it, and
// how the fine-tune is made from it.
type FinetuneBase struct {
	Base string
	Kind TuneKind
}

// Finetuned says what a fine-tune is: the model it tunes, as its maker
// names it (Base, when known) and as the catalog does (Of, when it lists
// it); who tuned it (By, when the provider's ID says); and how. A
// fine-tune's lab is the lab of the model it tunes.
type Finetuned struct {
	Base string          `json:"base,omitempty"`
	Of   gateway.ModelID `json:"of,omitempty"`
	By   string          `json:"by,omitempty"`
	Kind TuneKind        `json:"kind"`
}

// Service says what a provider's model ID is that is not one model, but a
// service built on models. The catalog leaves services out.
type Service string

// Services.
const (
	// ServiceRouter picks a model for each request, as OpenRouter's
	// "openrouter/auto" does.
	ServiceRouter Service = "router"
	// ServicePreset is a named policy over the provider's models, as
	// TrustedRouter's "trustedrouter/zdr" alias and The Grid's "agent-max"
	// tier, which any model that meets its contract serves.
	ServicePreset Service = "preset"
	// ServiceTool is an application built on models, as NanoGPT's
	// "universal-summarizer" and "nano-gpt-help".
	ServiceTool Service = "tool"
)
