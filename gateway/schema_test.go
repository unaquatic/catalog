package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/unaquatic/catalog/internal/jsonschema"
)

// datePattern is a date of a model, as its release, update and training
// cutoff: YYYY-MM-DD, or YYYY-MM where models.dev knows only the month.
const datePattern = `^\d{4}-\d{2}(-\d{2})?$`

// TestSchema checks that catalog.schema.json is the schema of Catalog's
// JSON, described by catalog.schema.descriptions.json, which must describe
// every definition and property and nothing else; and that the golden
// catalog and the committed data/catalog.json keep to it. -update rewrites
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
	schema, err := jsonschema.Generate(reflect.TypeFor[Catalog](), jsonschema.Options{
		ID:           SchemaURL,
		Title:        "Catalog",
		Descriptions: descs,
		Consts:       consts,
		Properties: map[string]map[string]any{
			"catalog.version":    {"const": Version},
			"model.released":     {"pattern": datePattern},
			"offering.released":  {"pattern": datePattern},
			"model.updated":      {"pattern": datePattern},
			"offering.updated":   {"pattern": datePattern},
			"model.knowledge":    {"pattern": datePattern},
			"offering.knowledge": {"pattern": datePattern},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("catalog.schema.json", schema, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if !bytes.Equal(schema, SchemaDocument) {
		t.Error("catalog.schema.json is out of date: go test ./gateway/ -run TestSchema -update")
	}

	for _, name := range []string{"testdata/catalog.golden.json", "../data/catalog.json"} {
		doc, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := jsonschema.Validate(schema, doc); err != nil {
			t.Errorf("%s breaks the schema:\n%v", name, err)
		}
	}
}
