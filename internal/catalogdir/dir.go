package catalogdir

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

	"github.com/unaquatic/catalog/gateway"
)

// A catalog directory holds a catalog split into files, so that a change of
// one lab's models or one provider's offerings is a change of one file:
//
//	catalog.json          Head: the catalog but its models and providers
//	models/<lab>.json     the models of a lab, by model ID
//	providers/<id>.json   a provider, with its offerings
//
// ReadDir reads one, and WriteDir writes one.
const (
	HeadFile     = "catalog.json"
	ModelsDir    = "models"
	ProvidersDir = "providers"
)

// Head is a catalog but its models and providers, as a catalog directory's
// HeadFile holds it.
type Head struct {
	Schema    string                                `json:"$schema,omitempty"`
	Version   int                                   `json:"version"`
	Source    string                                `json:"source,omitempty"`
	Generated time.Time                             `json:"generated"`
	Spellings gateway.Spellings                     `json:"spellings,omitempty"`
	Labs      map[gateway.ProviderID]gateway.Lab    `json:"labs,omitempty"`
	Families  map[string]gateway.Family             `json:"families,omitempty"`
	Efforts   map[gateway.Effort]gateway.EffortName `json:"efforts,omitempty"`
	Variant   *gateway.VariantNames                 `json:"variant,omitempty"`
}

// ReadDir reads the catalog in catalog directory dir, as ReadFS reads one.
func ReadDir(dir string) (*gateway.Catalog, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	defer root.Close()
	return ReadFS(root.FS())
}

// ReadFS reads the catalog in fsys, a catalog directory, such as one a
// program embeds, which must be of format Version, as Decode reads one
// document. It fails on a lab's file that holds another lab's model, and
// on a provider's file that holds another provider.
func ReadFS(fsys fs.FS) (*gateway.Catalog, error) {
	var h Head
	if err := readJSON(fsys, HeadFile, &h); err != nil {
		return nil, err
	}
	if h.Version != gateway.Version {
		return nil, fmt.Errorf("%w: %d, want %d", gateway.ErrVersion, h.Version, gateway.Version)
	}
	c := &gateway.Catalog{Schema: h.Schema, Version: h.Version, Source: h.Source, Generated: h.Generated,
		Spellings: h.Spellings, Labs: h.Labs, Families: h.Families, Efforts: h.Efforts, Variant: h.Variant,
		Models: map[gateway.ModelID]gateway.Model{}, Providers: map[gateway.ProviderID]gateway.Provider{}}
	labs, err := jsonFiles(fsys, ModelsDir)
	if err != nil {
		return nil, err
	}
	for _, lab := range labs {
		var models map[gateway.ModelID]gateway.Model
		if err := readJSON(fsys, ModelsDir+"/"+lab+".json", &models); err != nil {
			return nil, err
		}
		for id, m := range models {
			if id.Lab() != gateway.ProviderID(lab) {
				return nil, fmt.Errorf("gateway: %s holds %s, a model of another lab", ModelsDir+"/"+lab+".json", id)
			}
			c.Models[id] = m
		}
	}
	providers, err := jsonFiles(fsys, ProvidersDir)
	if err != nil {
		return nil, err
	}
	for _, id := range providers {
		var p gateway.Provider
		if err := readJSON(fsys, ProvidersDir+"/"+id+".json", &p); err != nil {
			return nil, err
		}
		if p.ID != gateway.ProviderID(id) {
			return nil, fmt.Errorf("gateway: %s holds provider %s", ProvidersDir+"/"+id+".json", p.ID)
		}
		c.Providers[p.ID] = p
	}
	return c, nil
}

// WriteDir writes c as catalog directory dir, creating it, and removes the
// files of labs and providers c no longer holds. Every file is indented as
// Encode writes the whole document.
func WriteDir(c *gateway.Catalog, dir string) error {
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
	byLab := map[gateway.ProviderID]map[gateway.ModelID]gateway.Model{}
	for id, m := range c.Models {
		if byLab[id.Lab()] == nil {
			byLab[id.Lab()] = map[gateway.ModelID]gateway.Model{}
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

// readJSON decodes the JSON file name of fsys into v.
func readJSON(fsys fs.FS, name string, v any) error {
	data, err := fs.ReadFile(fsys, name)
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
	return writeJSON(root, dir+"/"+id+".json", v)
}

// jsonFiles returns the names of the JSON files of dir in fsys, without
// their extension, in order; none when there is no dir.
func jsonFiles(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
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
	names, err := jsonFiles(root.FS(), dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if !slices.Contains(keep, T(name)) {
			if err := root.Remove(dir + "/" + name + ".json"); err != nil {
				errs = append(errs, fmt.Errorf("gateway: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}
