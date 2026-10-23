// Command catalog builds flow's model catalog from models.dev's catalog.json,
// following models.dev's structure: its models, as the site lists them at
// /models, and its providers with every model each lists, provider-specific
// ones included. Package gateway holds the catalog format, internal/build
// the steps that map models.dev to it, and internal/build/rules.json the
// rules those steps ask, which -rules replaces with a file of their own.
// aquatic, the repository of aquatic.dev, serves the catalog with a page
// that browses it.
// It keeps every provider, or those
// -providers names: "listed" keeps the providers the rules list.
//
//	go run ./cmd/catalog build -o catalog.json
//	go run ./cmd/catalog build -save-upstream models-dev.json -o catalog.json   # as go generate ./data/ does
//	go run ./cmd/catalog build -providers listed -o catalog-listed.json
//	go run ./cmd/catalog build -rules my-rules.json -notes
//	go run ./cmd/catalog build -in catalog-models-dev.json -providers anthropic,openai,vercel -notes
//	go run ./cmd/catalog snapshot -providers anthropic,openai -o snapshot.json   # the catalog flow compiles in, of this version
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/unaquatic/catalog/data"
	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/build"
	"github.com/unaquatic/catalog/internal/cmdline"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

func main() {
	cmdline.Main(context.Background(), rootCmd)
}

var rootCmd = &cmdline.Command[cmdline.NoOptions]{
	Name:     "catalog",
	Short:    "Build flow's model catalog",
	Banner:   "Build flow's model catalog from models.dev.",
	Commands: []cmdline.Node{buildCmd, updateCmd, remapCmd, applyCmd, joinCmd, snapshotCmd},
}

// SourceOptions choose the models.dev catalog to build from, and what to
// keep of it.
type SourceOptions struct {
	// Source is the models.dev origin to download catalog.json from.
	Source string
	// In is a file holding a models.dev catalog.json, read in place of
	// Source.
	In string
	// Providers is the comma-separated list of providers to keep, "listed"
	// for those the rules list, or "all".
	Providers string
	// Infer applies the mapping's own rules on top of models.dev's ties.
	Infer bool
	// Rules is a rules document to map by, in place of the one built in.
	Rules string
}

// BindSourceFlags binds the flags of o.
func BindSourceFlags(fs *flag.FlagSet, o *SourceOptions) {
	fs.StringVar(&o.Source, "source", modelsdev.DefaultSource, "Download models.dev's catalog.json from `url`")
	fs.StringVar(&o.In, "in", "", "Read models.dev's catalog.json from `file` instead of downloading it")
	fs.StringVar(&o.Providers, "providers", "all", "Keep the comma-separated `providers`, those the rules list for listed, or all of them for all")
	fs.StringVar(&o.Rules, "rules", "", "Map by the rules in `file`, in place of internal/build/rules.json")
	fs.BoolVar(&o.Infer, "infer", true, "Tie models models.dev leaves provider-specific by their IDs, merge tier models and derive pro models; false keeps models.dev's ties alone")
}

// BuildOptions are the flags of the build command.
type BuildOptions struct {
	SourceOptions
	// Out is the file to write, or "" for standard output.
	Out string
	// Notes lists every note of the report, not only how many there are.
	Notes bool
	// Upstream is a file to write models.dev's catalog.json to, as Indent
	// writes it, or "" for none.
	Upstream string
}

var buildCmd = &cmdline.Command[BuildOptions]{
	Name:  "build",
	Short: "Build the catalog from models.dev and write it",
	Flags: func(fs *flag.FlagSet, o *BuildOptions) {
		BindSourceFlags(fs, &o.SourceOptions)
		const out = "Write the catalog to `file` instead of standard output"
		fs.StringVar(&o.Out, "out", "", out)
		fs.StringVar(&o.Out, "o", "", out)
		fs.BoolVar(&o.Notes, "notes", false, "List the tier offerings and every note of the mapping")
		fs.StringVar(&o.Upstream, "save-upstream", "", "Write models.dev's catalog.json to `file` too, indented with its keys sorted")
	},
	Exclusive: [][]string{{"source", "in"}},
	Run: func(ctx context.Context, o BuildOptions, _ []string) error {
		return Build(ctx, o, os.Stdout, os.Stderr)
	},
}

// Build builds the catalog as o says, writes it to o.Out or to stdout, and
// models.dev's catalog.json to o.Upstream, and reports what the mapping did
// on stderr.
func Build(ctx context.Context, o BuildOptions, stdout, stderr io.Writer) error {
	data, source, err := ReadSource(ctx, o.SourceOptions)
	if err != nil {
		return err
	}
	if o.Upstream != "" {
		doc, err := Indent(data)
		if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		if err := os.WriteFile(o.Upstream, doc, 0o644); err != nil {
			return err
		}
	}
	c, rep, err := Map(data, source, o.SourceOptions, time.Now())
	if err != nil {
		return err
	}
	doc, err := gateway.Encode(c)
	if err != nil {
		return err
	}
	if o.Out == "" {
		if _, err := stdout.Write(doc); err != nil {
			return err
		}
	} else if err := os.WriteFile(o.Out, doc, 0o644); err != nil {
		return err
	}
	PrintReport(stderr, rep, o.Notes)
	return nil
}

// Load reads the models.dev catalog.json that o names and maps it, stamping
// the result as generated at now.
func Load(ctx context.Context, o SourceOptions, now time.Time) (*gateway.Catalog, build.Report, error) {
	data, source, err := ReadSource(ctx, o)
	if err != nil {
		return nil, build.Report{}, err
	}
	return Map(data, source, o, now)
}

// Map maps data, the models.dev catalog.json read from source, as o says,
// stamping the result as generated at now.
func Map(data []byte, source string, o SourceOptions, now time.Time) (*gateway.Catalog, build.Report, error) {
	b, err := modelsdev.DecodeBundle(data)
	if err != nil {
		return nil, build.Report{}, fmt.Errorf("%s: %w", source, err)
	}
	rules, err := ReadRules(o.Rules)
	if err != nil {
		return nil, build.Report{}, err
	}
	var providers, missing []gateway.ProviderID
	switch list := strings.TrimSpace(o.Providers); list {
	case "all", "":
	case "listed":
		providers, missing = rules.ListedIn(b)
	default:
		providers = ParseIDs(list)
	}
	c, rep, err := build.FromModelsDev(b, build.Options{
		Providers: providers,
		Infer:     o.Infer,
		Source:    source,
		Generated: now.UTC().Truncate(time.Second),
		Rules:     rules,
	})
	for _, id := range missing {
		rep.Notes = append(rep.Notes, build.Note{Provider: id, Text: "listed by the rules, but models.dev has no such provider"})
	}
	return c, rep, err
}

// ReadRules returns the rules in file, or the rules built in when file is
// empty.
func ReadRules(file string) (*build.Rules, error) {
	if file == "" {
		return build.DefaultRules(), nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	rules, err := build.DecodeRules(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return rules, nil
}

// ReadSource returns the models.dev catalog.json that o names, and the name
// of where it came from.
func ReadSource(ctx context.Context, o SourceOptions) (data []byte, source string, err error) {
	if o.In != "" {
		data, err = os.ReadFile(o.In)
		return data, o.In, err
	}
	resp, err := modelsdev.NewFetcher(o.Source, nil).FetchPath(ctx, modelsdev.BundlePath, "")
	if err != nil {
		return nil, "", err
	}
	return resp.Data, o.Source, nil
}

// Indent returns the JSON document data with every object's keys sorted and
// two spaces of indent, its numbers as data writes them, so that one
// download of models.dev's catalog.json diffs line by line against another.
func Indent(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseIDs reads a comma-separated list of provider IDs, leaving out empty
// entries and repeats, in order.
func ParseIDs(list string) []gateway.ProviderID {
	var ids []gateway.ProviderID
	for id := range strings.SplitSeq(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, gateway.ProviderID(id))
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// PrintReport writes a summary of rep to w, and its tier offerings and notes
// when all is set.
func PrintReport(w io.Writer, rep build.Report, all bool) {
	n := rep.Bases
	fmt.Fprintf(w, "catalog: %d models, %d providers, %d listings grouped into %d offerings with %d variants and %d aliases\n",
		rep.Models, rep.Providers, rep.Listings, rep.Offerings, rep.Variants, rep.AliasIDs)
	fmt.Fprintf(w, "catalog: models.dev ties %d by canonical_model_id and %d as a lab's own; rules tie %d by ID, %d as a tier, %d as pro, %d as inferred; %d provider-specific\n",
		n[build.BasisCanonical], n[build.BasisLab], n[build.BasisID], n[build.BasisTier], n[build.BasisPro],
		n[build.BasisInferred], n[build.BasisNone])
	if n[build.BasisRule] > 0 {
		fmt.Fprintf(w, "catalog: %d listings tied to the model a rule names\n", n[build.BasisRule])
	}
	if len(rep.Merged) > 0 || len(rep.Derived) > 0 {
		fmt.Fprintf(w, "catalog: merged %v; derived %v\n", rep.Merged, rep.Derived)
	}
	if len(rep.Inferred) > 0 {
		fmt.Fprintf(w, "catalog: %d models inferred from providers' model IDs\n", len(rep.Inferred))
	}
	if len(rep.Services) > 0 {
		fmt.Fprintf(w, "catalog: %d listings left out as routers, presets or tools\n", len(rep.Services))
	}
	if len(rep.Unknown) > 0 {
		fmt.Fprintf(w, "catalog: %d listings left out: no rule finds their model or lab\n", len(rep.Unknown))
	}
	if !all {
		fmt.Fprintf(w, "catalog: %d tier listings, %d notes (-notes lists them)\n", len(rep.Tiers), len(rep.Notes))
		return
	}
	for _, s := range rep.Services {
		fmt.Fprintf(w, "service: %s\n", s)
	}
	for _, u := range rep.Unknown {
		fmt.Fprintf(w, "unknown: %s\n", u)
	}
	for _, t := range rep.Tiers {
		fmt.Fprintf(w, "tier: %s\n", t)
	}
	for _, note := range rep.Notes {
		fmt.Fprintf(w, "note: %s\n", note)
	}
}

// SnapshotOptions are the flags of the snapshot command.
type SnapshotOptions struct {
	// Source is the catalog server to download the catalog from, or "" for
	// the catalog this command embeds, data.Catalog.
	Source string
	// Providers is the comma-separated list of providers to keep.
	Providers string
	// Out is the file to write.
	Out string
}

var snapshotCmd = &cmdline.Command[SnapshotOptions]{
	Name:  "snapshot",
	Short: "Write the catalog of a few providers, of this version or from a catalog server, for flow to embed",
	Flags: func(fs *flag.FlagSet, o *SnapshotOptions) {
		fs.StringVar(&o.Source, "source", "", "Download the catalog from the catalog server at `url`, instead of taking the one this version embeds")
		fs.StringVar(&o.Providers, "providers", "anthropic,openai", "Keep the comma-separated `providers`")
		const out = "Write the snapshot to `file`"
		fs.StringVar(&o.Out, "out", "snapshot.json", out)
		fs.StringVar(&o.Out, "o", "snapshot.json", out)
	},
	Run: func(ctx context.Context, o SnapshotOptions, _ []string) error {
		return Snapshot(ctx, o, os.Stderr)
	},
}

// Snapshot takes the catalog this command embeds, data.Catalog, which is the
// catalog of the module version it is built at, or else downloads the one
// the catalog server o.Source names; keeps o.Providers and the models they
// offer; and writes it, compact, to o.Out. go run builds the command at the
// version the running module requires, so a snapshot that program
// generates holds the catalog of the version it builds with.
func Snapshot(ctx context.Context, o SnapshotOptions, stderr io.Writer) error {
	var c *gateway.Catalog
	if o.Source == "" {
		var err error
		if c, err = data.Catalog(); err != nil {
			return fmt.Errorf("the embedded catalog: %w", err)
		}
	} else {
		resp, err := modelsdev.NewFetcher(o.Source, nil).FetchPath(ctx, gateway.DocumentPath, "")
		if err != nil {
			return err
		}
		if c, err = gateway.Decode(resp.Data); err != nil {
			return fmt.Errorf("%s: %w", o.Source, err)
		}
	}
	kept := build.Keep(c, ParseIDs(o.Providers)...)
	doc, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	if err := os.WriteFile(o.Out, append(doc, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "snapshot: %d providers, %d models, %d bytes to %s\n", len(kept.Providers), len(kept.Models), len(doc)+1, o.Out)
	return nil
}
