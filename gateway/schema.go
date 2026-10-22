package gateway

import (
	_ "embed"
	"net/http"
)

// SchemaPath is where a catalog server serves SchemaDocument, beside the
// catalog at DocumentPath.
const SchemaPath = "/schema.json"

// SchemaURL is where the JSON Schema of the catalog document is published,
// beside the catalog at DefaultSource. A catalog's Schema names it.
const SchemaURL = DefaultSource + SchemaPath

// SchemaDocument is the JSON Schema (draft 2020-12) of the catalog
// document: generated from Catalog's JSON, and described in its terms by
// catalog.schema.descriptions.json, by TestSchema, whose -update flag
// rewrites catalog.schema.json.
//
//go:embed catalog.schema.json
var SchemaDocument []byte

// schema serves SchemaDocument.
func (h *Handler) schema(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(SchemaDocument)
}
