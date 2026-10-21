// Package data holds models.dev's catalog.json, as models-dev.json, and the
// catalog built from it, as catalog.json. Both are committed, so that every
// update shows as a diff what changed upstream and what the mapping made of
// it. Regenerate both from models.dev with go generate.
//
// Serve them with no network with go run ./cmd/catalog serve -in
// data/models-dev.json, or the catalog alone with -catalog data/catalog.json.
package data

//go:generate go run ../cmd/catalog build -save-upstream models-dev.json -o catalog.json
