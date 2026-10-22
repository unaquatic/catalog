package gateway_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/catalogdir"
	"github.com/unaquatic/catalog/internal/jsonschema"
)

// update makes TestSchema rewrite catalog.schema.json.
var update = flag.Bool("update", false, "rewrite catalog.schema.json")

// dayPattern is a model's release and update date: YYYY-MM-DD. A refresh
// that brings one of a month alone fails here, so that it reaches no
// release; give it its day in data/catalog.
const dayPattern = `^\d{4}-\d{2}-\d{2}$`

// monthPattern is a model's training cutoff: YYYY-MM-DD, or YYYY-MM, as labs
// most often give it.
const monthPattern = `^\d{4}-\d{2}(-\d{2})?$`

// TestSchema checks that catalog.schema.json is the schema of Catalog's
// JSON, described by catalog.schema.descriptions.json, which must describe
// every definition and property and nothing else; and that the golden
// catalog and the committed data/catalog keep to it. -update rewrites
// the schema.
func TestSchema(t *testing.T) {
	var descs jsonschema.Descriptions
	data, err := os.ReadFile("catalog.schema.descriptions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &descs); err != nil {
		t.Fatalf("catalog.schema.descriptions.json: %v", err)
	}
	consts, err := jsonschema.ReadConsts(".")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Generate(reflect.TypeFor[gateway.Catalog](), jsonschema.Options{
		ID:           gateway.SchemaURL,
		Title:        "Catalog",
		Descriptions: descs,
		Consts:       consts,
		Properties: map[string]map[string]any{
			"catalog.version":    {"const": gateway.Version},
			"model.released":     {"pattern": dayPattern},
			"offering.released":  {"pattern": dayPattern},
			"model.updated":      {"pattern": dayPattern},
			"offering.updated":   {"pattern": dayPattern},
			"model.knowledge":    {"pattern": monthPattern},
			"offering.knowledge": {"pattern": monthPattern},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("catalog.schema.json", schema, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if !bytes.Equal(schema, gateway.SchemaDocument) {
		t.Error("catalog.schema.json is out of date: go test ./gateway/ -run TestSchema -update")
	}

	ours, err := catalogdir.ReadDir("../data/catalog")
	if err != nil {
		t.Fatal(err)
	}
	joined, err := gateway.Encode(ours)
	if err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() ([]byte, error){
		"the golden catalog": func() ([]byte, error) { return os.ReadFile("../internal/build/testdata/catalog.golden.json") },
		"../data/catalog":    func() ([]byte, error) { return joined, nil },
	} {
		doc, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if err := jsonschema.Validate(schema, doc); err != nil {
			t.Errorf("%s breaks the schema:\n%v", name, err)
		}
	}
}
