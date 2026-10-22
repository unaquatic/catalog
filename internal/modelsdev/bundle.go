package modelsdev

import (
	"encoding/json"
	"fmt"
)

// BundlePath is the models.dev document holding both the providers' model
// lists, as api.json has them, and the labs' models, as models.json has
// them: catalog.json, with models of every type, as the models.dev site
// lists them.
const BundlePath = "/catalog.json?type=all"

// Bundle is the models.dev catalog.json document.
type Bundle struct {
	// Providers are the providers and the models each lists, by provider.
	Providers Catalog `json:"providers"`
	// Models are the labs' models by canonical ID, such as
	// "anthropic/claude-opus-5-5", which the models.dev site lists at
	// /models. A provider's model names one in its canonical_model_id.
	Models map[string]LabModel `json:"models"`
}

// LabModel is a model as the lab that made it describes it, apart from any
// provider that serves it.
type LabModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Family      *string `json:"family,omitempty"`
	// Type is the kind of model, or empty for a language model.
	Type        ModelType `json:"type,omitempty"`
	ReleaseDate string    `json:"release_date"`
	LastUpdated string    `json:"last_updated"`
	Knowledge   *string   `json:"knowledge,omitempty"`

	Attachment       bool  `json:"attachment"`
	Reasoning        bool  `json:"reasoning"`
	ToolCall         bool  `json:"tool_call"`
	OpenWeights      bool  `json:"open_weights"`
	Temperature      *bool `json:"temperature,omitempty"`
	StructuredOutput *bool `json:"structured_output,omitempty"`

	Modalities Modalities `json:"modalities"`
	Limit      Limit      `json:"limit"`

	// License names the license of open weights, such as "Apache-2.0".
	License    string      `json:"license,omitempty"`
	Weights    []Weight    `json:"weights,omitempty"`
	Benchmarks []Benchmark `json:"benchmarks,omitempty"`
	Links      []Link      `json:"links,omitempty"`
}

// Weight is where a model's weights can be downloaded.
type Weight struct {
	Label        string `json:"label,omitempty"`
	URL          string `json:"url"`
	Format       string `json:"format,omitempty"`
	Quantization string `json:"quantization,omitempty"`
}

// Benchmark is a model's score on a benchmark, as its source reports it.
type Benchmark struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	Metric  string  `json:"metric,omitempty"`
	Source  string  `json:"source"`
	Date    string  `json:"date,omitempty"`
	Version string  `json:"version,omitempty"`
	Variant string  `json:"variant,omitempty"`
	Dataset string  `json:"dataset,omitempty"`
	Harness string  `json:"harness,omitempty"`
}

// Link is a page about a model, such as its model card.
type Link struct {
	Label string `json:"label,omitempty"`
	URL   string `json:"url"`
	// Type is the kind of page, such as "model_card" or "announcement".
	Type string `json:"type,omitempty"`
}

// DecodeBundle parses a catalog.json document.
func DecodeBundle(data []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("catalog: decode bundle: %w", err)
	}
	if b.Providers == nil || b.Models == nil {
		return Bundle{}, fmt.Errorf("catalog: decode bundle: the document lacks providers or models; it is not models.dev's catalog.json")
	}
	return b, nil
}
