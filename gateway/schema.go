package gateway

import _ "embed"

// schemaPath is where SchemaURL is, beside the catalog at DefaultSource.
const schemaPath = "/schema.json"

// SchemaURL is where the JSON Schema of the catalog document is published,
// beside the catalog at DefaultSource. A catalog's Schema names it.
const SchemaURL = DefaultSource + schemaPath

// SchemaDocument is the JSON Schema (draft 2020-12) of the catalog
// document: generated from Catalog's JSON, and described in its terms by
// catalog.schema.descriptions.json, by TestSchema, whose -update flag
// rewrites catalog.schema.json.
//
//go:embed catalog.schema.json
var SchemaDocument []byte
