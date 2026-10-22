package modelsdev

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDecodeBundle(t *testing.T) {
	const doc = `{"providers": {"anthropic": {"id": "anthropic", "name": "Anthropic", "npm": "@ai-sdk/anthropic",
		"models": {"claude-opus-5-5": {"id": "claude-opus-5-5", "name": "Claude Opus 5.5",
			"modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1000000, "output": 128000}}}}},
		"models": {"anthropic/claude-opus-5-5": {"id": "anthropic/claude-opus-5-5", "name": "Claude Opus 5.5",
			"description": "d", "release_date": "2026-09-22", "last_updated": "2026-09-22", "license": "proprietary",
			"modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1000000, "output": 128000},
			"weights": [{"label": "HF", "url": "https://huggingface.co/x", "format": "safetensors", "quantization": "fp8"}],
			"benchmarks": [{"name": "SWE-bench", "score": 81.5, "metric": "resolved", "source": "https://x", "date": "2026-09-22",
				"version": "1", "variant": "verified", "dataset": "d", "harness": "h"}],
			"links": [{"label": "Card", "url": "https://x/card", "type": "model_card"}]}}}`
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.DisallowUnknownFields()
	var strict Bundle
	if err := dec.Decode(&strict); err != nil {
		t.Fatalf("strict decode: %v", err)
	}
	b, err := DecodeBundle([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m := b.Models["anthropic/claude-opus-5-5"]
	if m.License != "proprietary" || len(m.Weights) != 1 || m.Benchmarks[0].Score != 81.5 || m.Links[0].Type != "model_card" {
		t.Errorf("lab model = %+v", m)
	}
	if _, ok := b.Providers["anthropic"].Models["claude-opus-5-5"]; !ok {
		t.Error("bundle lacks the provider's model")
	}
	if _, err := DecodeBundle([]byte(`{"anthropic": {"id": "anthropic", "models": {}}}`)); err == nil ||
		!strings.Contains(err.Error(), "not models.dev's catalog.json") {
		t.Errorf("DecodeBundle(api.json) = %v, want an error naming catalog.json", err)
	}
}

// TestProbeLiveBundle strictly decodes a downloaded catalog.json to detect
// fields the types do not model. It is skipped unless CATALOG_BUNDLE points
// at one.
func TestProbeLiveBundle(t *testing.T) {
	path := os.Getenv("CATALOG_BUNDLE")
	if path == "" {
		t.Skip("CATALOG_BUNDLE not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var b Bundle
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("strict decode found an unmodelled field: %v", err)
	}
	t.Logf("providers=%d models=%d", len(b.Providers), len(b.Models))
}
