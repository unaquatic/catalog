// Package data holds our catalog, the catalog's source of truth, as the
// catalog directory catalog: catalog/catalog.json, a file per lab's models
// in catalog/models, and a file per provider in catalog/providers, as
// gateway.ReadDir reads them; models.dev's catalog.json it last took
// changes from, as models-dev.json; and the patch of those changes, as
// patch.json. All are committed.
//
// A program reads the catalog of the version it builds with from Catalog,
// with no network:
//
//	c, err := data.Catalog()
//
// go generate takes models.dev's changes: it downloads its catalog.json,
// diffs it, as the mapping sees it, with models-dev.json, and applies the
// diff to the catalog, which keeps what it holds of its own, and keeps what
// it changed that models.dev changed too, as a conflict in patch.json. Edit
// the catalog's files by hand for what models.dev lacks or has wrong.
//
// Serve the catalog with no network with go run ./cmd/catalog serve
// -catalog data/catalog, and write it as one catalog.json with go run
// ./cmd/catalog join.
package data

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/unaquatic/catalog/gateway"
)

//go:generate go run ../cmd/catalog update -base models-dev.json -catalog catalog -patch patch.json -save-upstream models-dev.json

//go:embed catalog
var files embed.FS

// Catalog returns the catalog this package embeds, read once, as
// gateway.ReadFS reads it.
var Catalog = sync.OnceValues(func() (*gateway.Catalog, error) {
	dir, err := fs.Sub(files, "catalog")
	if err != nil {
		return nil, err
	}
	return gateway.ReadFS(dir)
})
