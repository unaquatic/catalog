package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A catalog directory holds a catalog split into files, so that a change of
// one lab's models or one provider's offerings is a change of one file:
//
//	catalog.json          Head: the catalog but its models and providers
//	models/<lab>.json     the models of a lab, by model ID
//	providers/<id>.json   a provider, with its offerings
//
// ReadDir reads one, and Catalog.WriteDir writes one.
const (
	HeadFile     = "catalog.json"
	ModelsDir    = "models"
	ProvidersDir = "providers"
)

// Head is a catalog but its models and providers, as a catalog directory's
// HeadFile holds it.
type Head struct {
	Schema    string                `json:"$schema,omitempty"`
	Version   int                   `json:"version"`
	Source    string                `json:"source,omitempty"`
	Generated time.Time             `json:"generated"`
	Spellings Spellings             `json:"spellings,omitempty"`
	Labs      map[ProviderID]Lab    `json:"labs,omitempty"`
	Families  map[string]Family     `json:"families,omitempty"`
	Efforts   map[Effort]EffortName `json:"efforts,omitempty"`
	Variant   *VariantNames         `json:"variant,omitempty"`
}

// ReadDir reads the catalog in catalog directory dir, which must be of
// format Version, as Decode reads one document. It fails on a lab's file
// that holds another lab's model, and on a provider's file that holds
// another provider.
func ReadDir(dir string) (*Catalog, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	defer root.Close()
	var h Head
	if err := readJSON(root, HeadFile, &h); err != nil {
		return nil, err
	}
	if h.Version != Version {
		return nil, fmt.Errorf("%w: %d, want %d", ErrVersion, h.Version, Version)
	}
	c := &Catalog{Schema: h.Schema, Version: h.Version, Source: h.Source, Generated: h.Generated,
		Spellings: h.Spellings, Labs: h.Labs, Families: h.Families, Efforts: h.Efforts, Variant: h.Variant,
		Models: map[ModelID]Model{}, Providers: map[ProviderID]Provider{}}
	labs, err := jsonFiles(root, ModelsDir)
	if err != nil {
		return nil, err
	}
	for _, lab := range labs {
		var models map[ModelID]Model
		if err := readJSON(root, filepath.Join(ModelsDir, lab+".json"), &models); err != nil {
			return nil, err
		}
		for id, m := range models {
			if id.Lab() != ProviderID(lab) {
				return nil, fmt.Errorf("gateway: %s holds %s, a model of another lab", filepath.Join(ModelsDir, lab+".json"), id)
			}
			c.Models[id] = m
		}
	}
	providers, err := jsonFiles(root, ProvidersDir)
	if err != nil {
		return nil, err
	}
	for _, id := range providers {
		var p Provider
		if err := readJSON(root, filepath.Join(ProvidersDir, id+".json"), &p); err != nil {
			return nil, err
		}
		if p.ID != ProviderID(id) {
			return nil, fmt.Errorf("gateway: %s holds provider %s", filepath.Join(ProvidersDir, id+".json"), p.ID)
		}
		c.Providers[p.ID] = p
	}
	return c, nil
}

// WriteDir writes c as catalog directory dir, creating it, and removes the
// files of labs and providers c no longer holds. Every file is indented as
// Encode writes the whole document.
func (c *Catalog) WriteDir(dir string) error {
	for _, sub := range []string{ModelsDir, ProvidersDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	defer root.Close()
	h := Head{Schema: c.Schema, Version: c.Version, Source: c.Source, Generated: c.Generated,
		Spellings: c.Spellings, Labs: c.Labs, Families: c.Families, Efforts: c.Efforts, Variant: c.Variant}
	if err := writeJSON(root, HeadFile, h); err != nil {
		return err
	}
	byLab := map[ProviderID]map[ModelID]Model{}
	for id, m := range c.Models {
		if byLab[id.Lab()] == nil {
			byLab[id.Lab()] = map[ModelID]Model{}
		}
		byLab[id.Lab()][id] = m
	}
	var errs []error
	for lab, models := range byLab {
		errs = append(errs, writeNamed(root, ModelsDir, string(lab), models))
	}
	for id, p := range c.Providers {
		errs = append(errs, writeNamed(root, ProvidersDir, string(id), p))
	}
	errs = append(errs, removeStale(root, ModelsDir, slices.Collect(maps.Keys(byLab))),
		removeStale(root, ProvidersDir, slices.Collect(maps.Keys(c.Providers))))
	return errors.Join(errs...)
}

// readJSON decodes the JSON file name of root into v.
func readJSON(root *os.Root, name string, v any) error {
	data, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("gateway: %s: %w", name, err)
	}
	return nil
}

// writeJSON writes v to the file name of root, indented, with a newline.
func writeJSON(root *os.Root, name string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("gateway: %s: %w", name, err)
	}
	if err := root.WriteFile(name, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	return nil
}

// writeNamed writes v to the file of dir that id names, as id.json, when id
// is a name a file can have.
func writeNamed(root *os.Root, dir, id string, v any) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("gateway: %q is no name for a file of %s", id, dir)
	}
	return writeJSON(root, filepath.Join(dir, id+".json"), v)
}

// jsonFiles returns the names of the JSON files of dir in root, without
// their extension, in order; none when there is no dir.
func jsonFiles(root *os.Root, dir string) ([]string, error) {
	entries, err := fs.ReadDir(root.FS(), dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && e.Type().IsRegular() {
			names = append(names, name)
		}
	}
	return names, nil
}

// removeStale removes the JSON files of dir in root whose names keep does
// not hold.
func removeStale[T ~string](root *os.Root, dir string, keep []T) error {
	names, err := jsonFiles(root, dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if !slices.Contains(keep, T(name)) {
			if err := root.Remove(filepath.Join(dir, name+".json")); err != nil {
				errs = append(errs, fmt.Errorf("gateway: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}
