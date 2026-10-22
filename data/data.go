// Package data holds our catalog, catalog.json, the catalog's source of
// truth; models.dev's catalog.json it last took changes from, as
// models-dev.json; and the patch of those changes, as patch.json. All are
// committed.
//
// go generate takes models.dev's changes: it downloads its catalog.json,
// diffs it, as the mapping sees it, with models-dev.json, and applies the
// diff to catalog.json, which keeps what it holds of its own, and keeps
// what it changed that models.dev changed too, as a conflict in patch.json.
// Edit catalog.json by hand for what models.dev lacks or has wrong.
//
// Serve the catalog with no network with go run ./cmd/catalog serve
// -catalog data/catalog.json.
package data

//go:generate go run ../cmd/catalog update -base models-dev.json -catalog catalog.json -patch patch.json -save-upstream models-dev.json
