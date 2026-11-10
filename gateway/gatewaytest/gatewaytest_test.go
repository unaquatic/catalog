package gatewaytest

import (
	"maps"
	"slices"
	"testing"

	"github.com/unaquatic/catalog/gateway"
)

// TestFromAPILabs checks that a model listed under a lab's model ID is that
// lab's, asked for by that ID, and that a model listed under a name of the
// provider's own is the model of a lab of the provider's ID.
func TestFromAPILabs(t *testing.T) {
	c := FromAPI(t, `{
"provider-a":{"id":"provider-a","name":"Provider A","models":{
  "lab-a/model-a":{"id":"lab-a/model-a","name":"Model A","limit":{"context":400000,"output":128000}},
  "model-x":{"id":"model-x","name":"Model X","limit":{"context":100000,"output":8000}}
}}}`)

	want := []gateway.ModelID{"lab-a/model-a", "provider-a/model-x"}
	if got := slices.Sorted(maps.Keys(c.Models)); !slices.Equal(got, want) {
		t.Errorf("catalog models = %v, want %v", got, want)
	}
	if off, ok := c.Providers["provider-a"].Models["lab-a/model-a"]; !ok || off.ProviderOptions.ID != "lab-a/model-a" {
		t.Errorf("provider-a offers lab-a/model-a = %+v, %v; want it asked for as lab-a/model-a", off.ProviderOptions, ok)
	}
}
