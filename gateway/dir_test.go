package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// dirCatalog is a small catalog of two labs and two providers.
func dirCatalog() *Catalog {
	return &Catalog{Schema: SchemaURL, Version: Version, Source: "https://models.dev",
		Generated: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		Labs:      map[ProviderID]Lab{"acme": {ID: "acme", Name: "Acme"}},
		Models: map[ModelID]Model{
			"acme/a": {ID: "acme/a", Lab: "acme", Name: "A", Providers: []ProviderID{"acme", "gw"}},
			"beta/b": {ID: "beta/b", Lab: "beta", Name: "B", Providers: []ProviderID{"gw"}},
		},
		Providers: map[ProviderID]Provider{
			"acme": {ID: "acme", Name: "Acme", Models: map[ModelID]Offering{"acme/a": {Name: "A"}}},
			"gw":   {ID: "gw", Name: "Gateway", Models: map[ModelID]Offering{"acme/a": {Name: "A"}, "beta/b": {Name: "B"}}},
		},
	}
}

// TestDir checks that a catalog written as a directory reads back the same,
// a file per lab and per provider, and that a rewrite of a catalog that
// lost a lab and a provider removes their files.
func TestDir(t *testing.T) {
	dir := filepath.Join(t.ArtifactDir(), "catalog")
	c := dirCatalog()
	if err := c.WriteDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{HeadFile, "models/acme.json", "models/beta.json", "providers/acme.json", "providers/gw.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	got, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("ReadDir = %+v, want %+v", got, c)
	}

	delete(c.Models, "beta/b")
	delete(c.Providers, "gw")
	if err := c.WriteDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models/beta.json", "providers/gw.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s: %v, want it removed", name, err)
		}
	}
}

// TestReadDirMisplaced checks that ReadDir fails on a model in another
// lab's file, a provider in another provider's file, and another version.
func TestReadDirMisplaced(t *testing.T) {
	for name, tt := range map[string]struct{ file, content, want string }{
		"model":    {"models/acme.json", `{"beta/x": {"id": "beta/x", "lab": "beta", "name": "X"}}`, "a model of another lab"},
		"provider": {"providers/acme.json", `{"id": "gw", "name": "Gateway", "models": {}}`, "holds provider gw"},
		"version":  {HeadFile, `{"version": 1}`, "unsupported catalog version"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.ArtifactDir(), "catalog")
			if err := dirCatalog().WriteDir(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadDir(dir); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ReadDir = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestWriteDirName checks that WriteDir refuses an ID that is no file name.
func TestWriteDirName(t *testing.T) {
	c := dirCatalog()
	c.Providers["../x"] = Provider{ID: "../x"}
	if err := c.WriteDir(filepath.Join(t.ArtifactDir(), "catalog")); err == nil || !strings.Contains(err.Error(), "is no name for a file") {
		t.Errorf("WriteDir = %v, want it to refuse ../x", err)
	}
}
