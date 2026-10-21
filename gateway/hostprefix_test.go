package gateway

import (
	"testing"

	"github.com/unaquatic/catalog/modelsdev"
)

// TestHostPrefix checks that hostPrefix finds a word before the model only
// where it is a host's: one the rules strip at the provider, the
// provider's own ID, or another provider's before a brand, and never a
// lab's or a brand's name, nor a provider's ID before other words.
func TestHostPrefix(t *testing.T) {
	providers := modelsdev.Catalog{}
	for _, id := range []modelsdev.ProviderID{"gitlab", "aihubmix", "databricks", "edenai", "llama", "cerebras", "groq",
		"bee", "nvidia", "synthetic", "pareto", "nova", "mistral", "google", "openai"} {
		providers[id] = modelsdev.Provider{ID: id}
	}
	b := &builder{rules: DefaultRules(), bundle: modelsdev.Bundle{Providers: providers},
		labs: map[ProviderID]bool{"mistral": true, "google": true, "nvidia": true, "openai": true, "anthropic": true, "meta": true}}
	for _, tt := range []struct {
		provider ProviderID
		model    string
		want     string
	}{
		{"gitlab", "duo-chat-opus-4-8", "duo-chat-"},
		{"aihubmix", "coding-minimax-m2.7", "coding-"},
		{"aihubmix", "alicloud-glm-5.1", "alicloud-"},
		// A product's or plan's word is its provider's alone.
		{"openai", "duo-chat-opus-4-8", ""},
		{"google", "deep-research-preview-04-2026", ""},
		// The provider's own ID, before any word.
		{"bee", "bee-buzz", "bee-"},
		{"databricks", "databricks-gpt-oss-120b", "databricks-"},
		// Another provider's ID, before a brand or a lab's name.
		{"edenai", "databricks-gpt-oss-20b", "databricks-"},
		{"llama", "cerebras-llama-4-scout-17b-16e-instruct", "cerebras-"},
		{"llama", "groq-llama-4-maverick-17b-128e-instruct", "groq-"},
		{"edenai", "databricks-qwen3-next-80b-a3b-instruct", "databricks-"},
		// Another provider's ID before other words is the model's.
		{"nvidia", "synthetic-video-detector", ""},
		{"openai", "pareto-latest", ""},
		// A lab's or a brand's name is the model's own.
		{"mistral", "mistral-medium-2508", ""},
		{"edenai", "nova-pro-v1", ""},
		{"edenai", "llama-guard-4-12b", ""},
	} {
		if got := b.hostPrefix(tt.provider, tt.model); got != tt.want {
			t.Errorf("hostPrefix(%s, %s) = %q, want %q", tt.provider, tt.model, got, tt.want)
		}
	}
}
