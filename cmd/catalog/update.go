package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/cmdline"
	"github.com/unaquatic/catalog/internal/merge"
)

// Patch is a change of the catalog, as update and remap write it and apply
// applies it: JSON Patch operations, each with the value it replaces, and
// the operations that were not applied, with what the catalog holds there.
type Patch struct {
	// Source says where the change comes from.
	Source string `json:"source"`
	// Ops are the operations, in path order.
	Ops []merge.Op `json:"ops"`
	// Conflicts are the operations not applied.
	Conflicts []merge.Conflict `json:"conflicts,omitempty"`
}

// Counts returns how many of p's operations add, replace and remove.
func (p Patch) Counts() (added, replaced, removed int) {
	for _, op := range p.Ops {
		switch op.Op {
		case merge.OpAdd:
			added++
		case merge.OpReplace:
			replaced++
		case merge.OpRemove:
			removed++
		}
	}
	return added, replaced, removed
}

// unpatched reports the paths of a catalog that no patch changes: those the
// tool sets, and models' providers, which Catalog.Link recomputes from the
// offerings.
func unpatched(path []string) bool {
	if len(path) == 1 {
		return slices.Contains([]string{"$schema", "version", "source", "generated"}, path[0])
	}
	return len(path) == 3 && path[0] == "models" && path[2] == "providers"
}

// UpdateOptions are the flags of the update command.
type UpdateOptions struct {
	SourceOptions
	// Base is models.dev's catalog.json that Catalog last took changes from.
	Base string
	// Catalog is our catalog, which update reads and writes.
	Catalog string
	// Patch is a file to write the patch to, or "" for none.
	Patch string
	// Upstream is a file to write the new models.dev catalog.json to, as
	// Indent writes it, or "" for none.
	Upstream string
}

var updateCmd = &cmdline.Command[UpdateOptions]{
	Name:  "update",
	Short: "Apply what models.dev changed since its last version to our catalog",
	Flags: func(fs *flag.FlagSet, o *UpdateOptions) {
		BindSourceFlags(fs, &o.SourceOptions)
		fs.StringVar(&o.Base, "base", "data/models-dev.json", "Read models.dev's catalog.json that the catalog last took changes from, from `file`")
		fs.StringVar(&o.Catalog, "catalog", "data/catalog.json", "Read our catalog from `file`, and write it back changed")
		fs.StringVar(&o.Patch, "patch", "", "Write the patch to `file` too")
		fs.StringVar(&o.Upstream, "save-upstream", "", "Write models.dev's new catalog.json to `file`, indented with its keys sorted")
	},
	Exclusive: [][]string{{"source", "in"}},
	Run: func(ctx context.Context, o UpdateOptions, _ []string) error {
		return Update(ctx, o, time.Now(), os.Stderr)
	},
}

// Update applies to our catalog, o.Catalog, what models.dev changed from
// o.Base to the catalog.json o.SourceOptions names, as the catalog sees it:
// both mapped by the current mapping and rules, diffed, and the diff
// applied, as merge.Apply applies it, so that the catalog keeps what it
// holds of its own, and holds what it changed too, as conflicts. It writes
// the catalog, the new catalog.json to o.Upstream, and the patch to
// o.Patch, but only when models.dev's catalog.json is not o.Base's, as
// Indent writes them: a patch file keeps the last change models.dev made.
// It reports the patch on stderr. A catalog the patch changes is stamped
// as generated at now.
func Update(ctx context.Context, o UpdateOptions, now time.Time, stderr io.Writer) error {
	data, source, err := ReadSource(ctx, o.SourceOptions)
	if err != nil {
		return err
	}
	baseData, err := os.ReadFile(o.Base)
	if err != nil {
		return err
	}
	upstream, err := Indent(data)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	theirs, _, err := Map(data, source, o.SourceOptions, now)
	if err != nil {
		return err
	}
	base, _, err := Map(baseData, o.Base, o.SourceOptions, now)
	if err != nil {
		return err
	}
	ours, err := readCatalog(o.Catalog, base)
	if err != nil {
		return err
	}
	docs, err := generic(base, theirs)
	if err != nil {
		return err
	}
	p := Patch{Source: source, Ops: merge.Diff(docs[0].v, docs[1].v, unpatched)}
	if o.In != "" {
		// A file says nothing of where models.dev served it.
		source = ours.Source
	}
	if err := applyAndWrite(o.Catalog, ours, &p, source, now); err != nil {
		return err
	}
	if o.Upstream != "" {
		if err := os.WriteFile(o.Upstream, upstream, 0o644); err != nil {
			return err
		}
	}
	file := o.Patch
	if bytes.Equal(upstream, baseData) {
		file = ""
	}
	return finish(file, p, stderr)
}

// RemapOptions are the flags of the remap command.
type RemapOptions struct {
	SourceOptions
	// Catalog is our catalog.
	Catalog string
	// Patch is the file to write the patch to.
	Patch string
}

var remapCmd = &cmdline.Command[RemapOptions]{
	Name:  "remap",
	Short: "Write how the current mapping of models.dev differs from our catalog, as a patch to review and apply",
	Flags: func(fs *flag.FlagSet, o *RemapOptions) {
		BindSourceFlags(fs, &o.SourceOptions)
		fs.StringVar(&o.Catalog, "catalog", "data/catalog.json", "Read our catalog from `file`")
		const out = "Write the patch to `file`"
		fs.StringVar(&o.Patch, "out", "remap.json", out)
		fs.StringVar(&o.Patch, "o", "remap.json", out)
	},
	Exclusive: [][]string{{"source", "in"}},
	Run: func(ctx context.Context, o RemapOptions, _ []string) error {
		return Remap(ctx, o, os.Stderr)
	},
}

// Remap writes to o.Patch the patch that turns our catalog, o.Catalog, into
// the catalog the current mapping and rules make of the catalog.json
// o.SourceOptions names: a change of the mapping, which update does not
// carry, since it maps both versions of models.dev alike. Applied whole, it
// drops what the catalog holds of its own too, so review it, and keep the
// operations to apply.
func Remap(ctx context.Context, o RemapOptions, stderr io.Writer) error {
	data, source, err := ReadSource(ctx, o.SourceOptions)
	if err != nil {
		return err
	}
	mapped, _, err := Map(data, source, o.SourceOptions, time.Now())
	if err != nil {
		return err
	}
	ours, err := readCatalog(o.Catalog, nil)
	if err != nil {
		return err
	}
	docs, err := generic(ours, mapped)
	if err != nil {
		return err
	}
	return finish(o.Patch, Patch{Source: "remap of " + source, Ops: merge.Diff(docs[0].v, docs[1].v, unpatched)}, stderr)
}

// ApplyOptions are the flags of the apply command.
type ApplyOptions struct {
	// Catalog is our catalog, which apply reads and writes.
	Catalog string
	// Patch is the patch to apply.
	Patch string
}

var applyCmd = &cmdline.Command[ApplyOptions]{
	Name:  "apply",
	Short: "Apply a patch, as update and remap write them, to our catalog",
	Flags: func(fs *flag.FlagSet, o *ApplyOptions) {
		fs.StringVar(&o.Catalog, "catalog", "data/catalog.json", "Read our catalog from `file`, and write it back changed")
		fs.StringVar(&o.Patch, "patch", "remap.json", "Apply the patch in `file`")
	},
	Run: func(_ context.Context, o ApplyOptions, _ []string) error {
		return Apply(o, time.Now(), os.Stderr)
	},
}

// Apply applies the patch in o.Patch to our catalog, o.Catalog, as
// merge.Apply does, writes the catalog, stamped as generated at now when the
// patch changes it, and reports the operations it did not apply on stderr.
func Apply(o ApplyOptions, now time.Time, stderr io.Writer) error {
	data, err := os.ReadFile(o.Patch)
	if err != nil {
		return err
	}
	// Numbers as json.Number, as merge reads documents, so that they
	// compare with the catalog's exactly.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var p Patch
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("%s: %w", o.Patch, err)
	}
	ours, err := readCatalog(o.Catalog, nil)
	if err != nil {
		return err
	}
	if err := applyAndWrite(o.Catalog, ours, &p, ours.Source, now); err != nil {
		return err
	}
	return finish("", p, stderr)
}

// readCatalog reads our catalog from file, or returns fallback when there is
// none and fallback is not nil.
func readCatalog(file string, fallback *gateway.Catalog) (*gateway.Catalog, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) && fallback != nil {
		return fallback, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := gateway.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return c, nil
}

// doc is a catalog as merge reads documents.
type doc struct{ v any }

// generic returns catalogs cs as merge reads documents, in order, or the
// first error.
func generic(cs ...*gateway.Catalog) (docs []doc, err error) {
	for _, c := range cs {
		data, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		v, err := merge.Decode(data)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc{v})
	}
	return docs, nil
}

// applyAndWrite applies p's operations to ours, sets p's conflicts, and
// writes the result to file: from source, linked as Catalog.Link links it,
// and stamped as generated at now when it is not ours, its generated time
// aside. An operation ours holds already changes nothing, so it stamps
// nothing either.
func applyAndWrite(file string, ours *gateway.Catalog, p *Patch, source string, now time.Time) error {
	before, err := gateway.Encode(ours)
	if err != nil {
		return err
	}
	docs, err := generic(ours)
	if err != nil {
		return err
	}
	merged, conflicts := merge.Apply(docs[0].v, p.Ops)
	p.Conflicts = conflicts
	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	c, err := gateway.Decode(data)
	if err != nil {
		return fmt.Errorf("the patched catalog: %w", err)
	}
	c.Schema, c.Version, c.Source, c.Generated = gateway.SchemaURL, gateway.Version, source, ours.Generated
	c.Link()
	out, err := gateway.Encode(c)
	if err != nil {
		return err
	}
	if !bytes.Equal(out, before) {
		c.Generated = now.UTC().Truncate(time.Second)
		if out, err = gateway.Encode(c); err != nil {
			return err
		}
	}
	return os.WriteFile(file, out, 0o644)
}

// finish writes p to file, when one is named, indented, and reports it on
// stderr: how many operations add, replace and remove, and each conflict.
func finish(file string, p Patch, stderr io.Writer) error {
	if file != "" {
		if p.Ops == nil {
			p.Ops = []merge.Op{}
		}
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	added, replaced, removed := p.Counts()
	fmt.Fprintf(stderr, "catalog: %d added, %d replaced, %d removed, %d conflicts\n", added, replaced, removed, len(p.Conflicts))
	for _, c := range p.Conflicts {
		fmt.Fprintf(stderr, "conflict: %s %s: %s\n", c.Op.Op, c.Path, c.Why)
	}
	return nil
}
