// Package catalogdir reads and writes a catalog directory, our catalog as
// data/catalog commits it: catalog.json, the catalog's head, a file per lab's
// models in models, and a file per provider in providers, so that a change of
// one lab or provider is a change of one file.
package catalogdir
