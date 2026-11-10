package gatewaytest

import (
	"maps"
	"slices"
	"testing"

	"github.com/unaquatic/catalog/gateway"
)

// TestFromAPILabs checks that a model naming its lab's model in
// canonical_model_id is that lab's, that the lab is named but is no
// provider, that each provider serving it asks for it by its own ID, and
// that a model naming none is the model of a lab of its provider's ID.
func TestFromAPILabs(t *testing.T) {
	c := FromAPI(t, `{
"provider-a":{"id":"provider-a","name":"Provider A","models":{
  "model-a":{"id":"model-a","name":"Model A","canonical_model_id":"lab-a/model-a",
    "limit":{"context":400000,"output":128000}},
  "model-x":{"id":"model-x","name":"Model X","limit":{"context":100000,"output":8000}}
}},
"provider-b":{"id":"provider-b","name":"Provider B","models":{
  "model-a":{"id":"model-a","name":"Model A","canonical_model_id":"lab-a/model-a",
    "limit":{"context":400000,"output":128000}}
}}}`)

	for _, id := range []gateway.ModelID{"lab-a/model-a", "provider-a/model-x"} {
		if _, ok := c.Models[id]; !ok {
			t.Errorf("catalog lists no model %s; it lists %v", id, slices.Sorted(maps.Keys(c.Models)))
		}
	}
	if _, ok := c.Models["provider-a/model-a"]; ok {
		t.Error("model-a is a model of provider-a's lab too")
	}
	if got := slices.Sorted(maps.Keys(c.Providers)); !slices.Equal(got, []gateway.ProviderID{"provider-a", "provider-b"}) {
		t.Errorf("providers = %v, want provider-a and provider-b alone", got)
	}
	if lab, ok := c.Labs["lab-a"]; !ok || lab.Name != "Lab A" {
		t.Errorf("lab-a = %+v, %v; want it named Lab A", lab, ok)
	}
	for _, pid := range []gateway.ProviderID{"provider-a", "provider-b"} {
		off, ok := c.Providers[pid].Models["lab-a/model-a"]
		if !ok || off.ProviderOptions.ID != "model-a" {
			t.Errorf("%s offers lab-a/model-a = %+v, %v; want it as model-a", pid, off.ProviderOptions, ok)
		}
	}
}
