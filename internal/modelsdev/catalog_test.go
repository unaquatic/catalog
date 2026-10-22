package modelsdev

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestInterleavedUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Interleaved
		wantErr bool
	}{
		{name: "true", input: `true`, want: Interleaved{Enabled: true}},
		{name: "false", input: `false`, want: Interleaved{}},
		{name: "string", input: `"reasoning_text"`, want: Interleaved{Enabled: true, Field: "reasoning_text"}},
		{name: "object", input: `{"field":"reasoning_content"}`, want: Interleaved{Enabled: true, Field: "reasoning_content"}},
		{name: "invalid", input: `12`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Interleaved
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Unmarshal(%s) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInterleavedRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value Interleaved
		want  string
	}{
		{name: "bare true", value: Interleaved{Enabled: true}, want: `true`},
		{name: "bare false", value: Interleaved{}, want: `false`},
		{name: "named field", value: Interleaved{Enabled: true, Field: "reasoning"}, want: `{"field":"reasoning"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("Marshal = %s, want %s", data, tt.want)
			}
			var back Interleaved
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if back != tt.value {
				t.Errorf("round trip = %+v, want %+v", back, tt.value)
			}
		})
	}
}

func TestLifecycleDefaultsToActive(t *testing.T) {
	deprecated := StatusDeprecated
	tests := []struct {
		name  string
		model Model
		want  Status
	}{
		{name: "absent", model: Model{}, want: StatusActive},
		{name: "present", model: Model{Status: &deprecated}, want: StatusDeprecated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.Lifecycle(); got != tt.want {
				t.Errorf("Lifecycle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModalityPredicates(t *testing.T) {
	m := Model{Modalities: Modalities{
		Input:  []Modality{ModalityText, ModalityImage},
		Output: []Modality{ModalityText},
	}}

	tests := []struct {
		name           string
		mod            Modality
		accepts, emits bool
	}{
		{name: "text", mod: ModalityText, accepts: true, emits: true},
		{name: "image", mod: ModalityImage, accepts: true, emits: false},
		{name: "audio", mod: ModalityAudio, accepts: false, emits: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.Accepts(tt.mod); got != tt.accepts {
				t.Errorf("Accepts(%q) = %v, want %v", tt.mod, got, tt.accepts)
			}
			if got := m.Produces(tt.mod); got != tt.emits {
				t.Errorf("Produces(%q) = %v, want %v", tt.mod, got, tt.emits)
			}
		})
	}
}

func TestReleased(t *testing.T) {
	tests := []struct {
		name   string
		date   string
		wantOK bool
	}{
		{name: "valid", date: "2026-09-10", wantOK: true},
		{name: "empty", date: "", wantOK: false},
		{name: "malformed", date: "September 2026", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Model{ReleaseDate: tt.date}.Released()
			if ok != tt.wantOK {
				t.Fatalf("Released() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.Format(DateLayout) != tt.date {
				t.Errorf("Released() = %s, want %s", got.Format(DateLayout), tt.date)
			}
		})
	}
}

func TestCostAt(t *testing.T) {
	base := Price{Input: 1}
	mid := Price{Input: 2}
	top := Price{Input: 3}
	over := Price{Input: 9}
	tiered := Cost{Price: base, ContextOver200K: &over, Tiers: []CostTier{
		{Price: top, Tier: Tier{Type: TierContext, Size: 500_000}},
		{Price: mid, Tier: Tier{Type: TierContext, Size: 272_000}},
		{Price: Price{Input: 7}, Tier: Tier{Type: "other", Size: 1}},
	}}
	legacy := Cost{Price: base, ContextOver200K: &over}
	tests := []struct {
		name   string
		cost   Cost
		prompt int
		want   Price
	}{
		{name: "below every tier", cost: tiered, prompt: 272_000, want: base},
		{name: "over one tier", cost: tiered, prompt: 272_001, want: mid},
		{name: "over both tiers", cost: tiered, prompt: 600_000, want: top},
		{name: "legacy below", cost: legacy, prompt: 200_000, want: base},
		{name: "legacy over", cost: legacy, prompt: 200_001, want: over},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cost.At(tt.prompt); got.Input != tt.want.Input {
				t.Errorf("At(%d).Input = %v, want %v", tt.prompt, got.Input, tt.want.Input)
			}
		})
	}
}

// TestPriceCharge checks that each kind of token is charged its price per
// million, and cache tokens the input price when the catalog lists none.
func TestPriceCharge(t *testing.T) {
	read, write := 0.5, 6.25
	tests := []struct {
		name  string
		price Price
		want  float64
	}{
		{name: "cache prices", price: Price{Input: 5, Output: 25, CacheRead: &read, CacheWrite: &write}, want: 5*1 + 0.5*2 + 6.25*3 + 25*4},
		{name: "no cache prices", price: Price{Input: 5, Output: 25}, want: 5*1 + 5*2 + 5*3 + 25*4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.price.Charge(1_000_000, 2_000_000, 3_000_000, 4_000_000)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Charge = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPromptWindow checks that a model's prompt window is its input limit
// when the catalog lists one, and its context window otherwise.
func TestPromptWindow(t *testing.T) {
	input := 272_000.0
	if got := (Limit{Context: 400_000, Input: &input}).PromptWindow(); got != 272_000 {
		t.Errorf("with an input limit = %d, want 272000", got)
	}
	if got := (Limit{Context: 200_000}).PromptWindow(); got != 200_000 {
		t.Errorf("without an input limit = %d, want 200000", got)
	}
}

// TestDecodeRouting checks that a catalog decodes the fields that say how to
// reach a model: the provider's npm package and docs, a model's own package,
// its canonical ID and its kind, and that strict decoding accepts them.
func TestDecodeRouting(t *testing.T) {
	const doc = `{"vercel": {"id": "vercel", "name": "Vercel AI Gateway", "npm": "@ai-sdk/gateway",
		"doc": "https://vercel.com/docs/ai-gateway", "models": {
		"anthropic/claude-opus-5.5": {"id": "anthropic/claude-opus-5.5", "name": "Claude Opus 5.5",
			"canonical_model_id": "anthropic/claude-opus-5-5", "provider": {"npm": "@ai-sdk/anthropic"},
			"modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1000000, "output": 128000}},
		"cohere/embed-v5.0": {"id": "cohere/embed-v5.0", "name": "Embed 5", "type": "embedding",
			"modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 512, "output": 0}}}}}`
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.DisallowUnknownFields()
	var c Catalog
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("strict decode: %v", err)
	}
	p := c["vercel"]
	if p.NPM != "@ai-sdk/gateway" || p.Doc != "https://vercel.com/docs/ai-gateway" {
		t.Errorf("provider npm %q, doc %q", p.NPM, p.Doc)
	}
	opus := p.Models["anthropic/claude-opus-5.5"]
	if opus.CanonicalModelID != "anthropic/claude-opus-5-5" || opus.Override == nil || opus.Override.NPM != "@ai-sdk/anthropic" {
		t.Errorf("model canonical %q, override %+v", opus.CanonicalModelID, opus.Override)
	}
	if got := p.Models["cohere/embed-v5.0"].Type; got != ModelTypeEmbedding {
		t.Errorf("type = %q, want %q", got, ModelTypeEmbedding)
	}
}
