package gateway

import (
	"reflect"
	"slices"
	"testing"
)

func TestVariantKeys(t *testing.T) {
	off := Offering{Variants: map[string]Variant{
		"thinking:fast": {Mode: ModeThinking, Tier: TierFast},
		"free":          {Tier: TierFree},
		"thinking":      {Mode: ModeThinking},
		"fast":          {Tier: TierFast},
		"flex":          {Tier: TierFlex},
	}}
	if got, want := off.VariantKeys(), []string{"flex", "fast", "free", "thinking", "thinking:fast"}; !slices.Equal(got, want) {
		t.Errorf("VariantKeys = %q, want %q", got, want)
	}
	if VariantKey(ModeThinking, TierFast) != "thinking:fast" || VariantKey("", TierFast) != "fast" || VariantID("lab/m", "") != "lab/m" {
		t.Error("VariantKey or VariantID")
	}
}

// TestCatalogLookup checks that a variant ID finds its offering and variant, with
// keys of one and two segments, and a model whose own ID holds colons.
func TestCatalogLookup(t *testing.T) {
	c := &Catalog{Providers: map[ProviderID]Provider{"p": {
		Models: map[ModelID]Offering{
			"lab/m": {Variants: map[string]Variant{
				"fast": {Tier: TierFast}, "thinking:fast": {Mode: ModeThinking, Tier: TierFast}}},
			"lab/ft:m:org": {Variants: map[string]Variant{"fast": {Tier: TierFast}}},
		},
	}}}
	tests := []struct {
		id      string
		model   ModelID
		variant string
		ok      bool
	}{
		{"lab/m", "lab/m", "", true},
		{"lab/m:fast", "lab/m", "fast", true},
		{"lab/m:thinking:fast", "lab/m", "thinking:fast", true},
		{"lab/m:turbo", "", "", false},
		{"lab/ft:m:org", "lab/ft:m:org", "", true},
		{"lab/ft:m:org:fast", "lab/ft:m:org", "fast", true},
		{"nope", "", "", false},
	}
	for _, tt := range tests {
		m, v, ok := c.Lookup("p", tt.id)
		if m != tt.model || v != tt.variant || ok != tt.ok {
			t.Errorf("Lookup(%s) = %q, %q, %v", tt.id, m, v, ok)
		}
	}
}

func TestMergeBody(t *testing.T) {
	base := map[string]any{"additionalModelRequestFields": map[string]any{"reasoning": map[string]any{"effort": "high"}}, "a": 1.0}
	over := map[string]any{"additionalModelRequestFields": map[string]any{"service_tier": "ultrafast"}, "a": 2.0}
	got := mergeBody(base, over)
	want := map[string]any{"additionalModelRequestFields": map[string]any{"reasoning": map[string]any{"effort": "high"},
		"service_tier": "ultrafast"}, "a": 2.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeBody = %v, want %v", got, want)
	}
	if base["a"] != 1.0 || len(base["additionalModelRequestFields"].(map[string]any)) != 1 {
		t.Errorf("mergeBody changed its base: %v", base)
	}
	if mergeBody(nil, nil) != nil {
		t.Error("mergeBody(nil, nil) is not nil")
	}
}
