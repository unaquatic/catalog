package modelsdev

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestProbeLiveFeed strictly decodes a full catalog snapshot to detect fields
// the types do not model. It is skipped unless CATALOG_FEED points at
// a downloaded api.json.
func TestProbeLiveFeed(t *testing.T) {
	path := os.Getenv("CATALOG_FEED")
	if path == "" {
		t.Skip("CATALOG_FEED not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read feed: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var strict Catalog
	if err := dec.Decode(&strict); err != nil {
		t.Errorf("strict decode found unmodelled field: %v", err)
	}

	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	models := 0
	for _, p := range c {
		models += len(p.Models)
	}
	t.Logf("providers=%d models=%d", len(c), models)

	m, ok := c["openai"].Models["gpt-6-astra"]
	if !ok {
		t.Fatal("openai/gpt-6-astra missing from feed")
	}
	t.Logf("gpt-6-astra name=%q status=%s tools=%v ctx=%.0f reasoning_options=%d",
		m.Name, m.Lifecycle(), m.ToolCall, m.Limit.Context, len(m.ReasoningOptions))
}
