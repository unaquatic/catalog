package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unaquatic/catalog/gateway"
)

// edited writes fixture, the models.dev catalog.json, changed by edit, to a
// file of dir, and returns its name.
func edited(t *testing.T, dir, name string, edit func(doc map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

// listing returns provider p's listing id in models.dev catalog.json doc.
func listing(doc map[string]any, p, id string) map[string]any {
	return doc["providers"].(map[string]any)[p].(map[string]any)["models"].(map[string]any)[id].(map[string]any)
}

// TestUpdate checks that update applies what models.dev changed to our
// catalog: a new price, a dropped model, which goes from every provider;
// that it keeps what our catalog changed and added, the price it changed too
// as a conflict; and that it writes the patch, the new catalog.json and a
// new generated time. Remap then finds our own changes alone, and apply of
// a patch of them restores them.
func TestUpdate(t *testing.T) {
	dir := t.ArtifactDir()
	base := edited(t, dir, "base.json", func(map[string]any) {})
	theirs := edited(t, dir, "theirs.json", func(doc map[string]any) {
		listing(doc, "anthropic", "claude-opus-4-8")["cost"].(map[string]any)["input"] = 4.0
		listing(doc, "anthropic", "claude-opus-5-5")["cost"].(map[string]any)["input"] = 9.0
		delete(doc["providers"].(map[string]any)["anthropic"].(map[string]any)["models"].(map[string]any), "claude-sonnet-4-5")
	})

	// Our catalog: the base, mapped, with a name, a default effort and a
	// price of our own.
	catalog := filepath.Join(dir, "catalog.json")
	var stderr strings.Builder
	if err := Build(t.Context(), BuildOptions{SourceOptions: SourceOptions{In: base, Providers: "all", Infer: true}, Out: catalog}, io.Discard, &stderr); err != nil {
		t.Fatal(err)
	}
	ours := readTestCatalog(t, catalog)
	opus := ours.Models["anthropic/claude-opus-4-8"]
	opus.Name = "Ours"
	ours.Models[opus.ID] = opus
	off := ours.Providers["anthropic"].Models["anthropic/claude-opus-5-5"]
	off.Price.Input = 7
	ours.Providers["anthropic"].Models["anthropic/claude-opus-5-5"] = off
	writeTestCatalog(t, catalog, ours)

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	patch, upstream := filepath.Join(dir, "patch.json"), filepath.Join(dir, "upstream.json")
	stderr.Reset()
	o := UpdateOptions{SourceOptions: SourceOptions{In: theirs, Providers: "all", Infer: true}, Base: base, Catalog: catalog, Patch: patch, Upstream: upstream}
	if err := Update(t.Context(), o, now, &stderr); err != nil {
		t.Fatal(err)
	}
	got := readTestCatalog(t, catalog)
	if p := got.Providers["anthropic"].Models["anthropic/claude-opus-4-8"].Price; p == nil || p.Input != 4 {
		t.Errorf("opus 4.8 price = %+v, want models.dev's new input 4", p)
	}
	if p := got.Providers["anthropic"].Models["anthropic/claude-opus-5-5"].Price; p == nil || p.Input != 7 {
		t.Errorf("opus 5.5 price = %+v, want ours, 7, kept against models.dev's 9", p)
	}
	if m := got.Models["anthropic/claude-opus-4-8"]; m.Name != "Ours" || m.Providers[0] != "anthropic" {
		t.Errorf("opus 4.8 = %q at %v, want our name kept, and its providers", m.Name, m.Providers)
	}
	if _, ok := got.Providers["anthropic"].Models["anthropic/claude-sonnet-4-5"]; ok {
		t.Error("sonnet 4.5 is still offered at anthropic, which models.dev dropped")
	}
	if !got.Generated.Equal(now) {
		t.Errorf("generated = %v, want %v", got.Generated, now)
	}
	if !strings.Contains(stderr.String(), "1 conflicts") || !strings.Contains(stderr.String(), "claude-opus-5-5/price/input: ours changed it too") {
		t.Errorf("stderr = %q, want the conflict of opus 5.5's price", stderr.String())
	}
	var p Patch
	if data, err := os.ReadFile(patch); err != nil || json.Unmarshal(data, &p) != nil || len(p.Ops) == 0 || len(p.Conflicts) != 1 {
		t.Errorf("patch = %+v, %v; want its ops and one conflict", p, err)
	}
	if _, err := os.Stat(upstream); err != nil {
		t.Errorf("the new catalog.json: %v", err)
	}

	// Remap finds our own changes alone: the name, and the price.
	remap := filepath.Join(dir, "remap.json")
	if err := Remap(t.Context(), RemapOptions{SourceOptions: SourceOptions{In: theirs, Providers: "all", Infer: true}, Catalog: catalog, Patch: remap}, io.Discard); err != nil {
		t.Fatal(err)
	}
	var r Patch
	if data, err := os.ReadFile(remap); err != nil || json.Unmarshal(data, &r) != nil || len(r.Ops) != 2 {
		t.Fatalf("remap = %+v, %v; want our two changes", r.Ops, err)
	}
	// Applied, it makes the catalog the mapping's.
	if err := Apply(ApplyOptions{Catalog: catalog, Patch: remap}, now, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := readTestCatalog(t, catalog); got.Models["anthropic/claude-opus-4-8"].Name != "Claude Opus 4.8" ||
		got.Providers["anthropic"].Models["anthropic/claude-opus-5-5"].Price.Input != 9 {
		t.Error("apply of the remap left our changes")
	}
}

// TestUpdateUnchanged checks that update of the version of models.dev the
// catalog took last changes nothing, the generated time included.
func TestUpdateUnchanged(t *testing.T) {
	dir := t.ArtifactDir()
	catalog := filepath.Join(dir, "catalog.json")
	if err := Build(t.Context(), BuildOptions{SourceOptions: SourceOptions{In: fixture, Providers: "all", Infer: true}, Out: catalog}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	o := UpdateOptions{SourceOptions: SourceOptions{In: fixture, Providers: "all", Infer: true}, Base: fixture, Catalog: catalog}
	if err := Update(t.Context(), o, time.Now().Add(time.Hour), &stderr); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(catalog); string(after) != string(before) {
		t.Error("update of the same version changed the catalog")
	}
	if !strings.Contains(stderr.String(), "0 added, 0 replaced, 0 removed, 0 conflicts") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func readTestCatalog(t *testing.T, file string) *gateway.Catalog {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeTestCatalog(t *testing.T, file string, c *gateway.Catalog) {
	t.Helper()
	data, err := gateway.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateAgain checks that update of changes our catalog holds already,
// as after a run whose new catalog.json was not saved, stamps no new
// generated time; and that update when models.dev's catalog.json is the
// one it last took changes from leaves the patch file be, so that it keeps
// the last change.
func TestUpdateAgain(t *testing.T) {
	dir := t.ArtifactDir()
	base := edited(t, dir, "base.json", func(map[string]any) {})
	theirs := edited(t, dir, "theirs.json", func(doc map[string]any) {
		listing(doc, "anthropic", "claude-opus-4-8")["cost"].(map[string]any)["input"] = 4.0
	})
	catalog, patch, upstream := filepath.Join(dir, "catalog.json"), filepath.Join(dir, "patch.json"), filepath.Join(dir, "upstream.json")
	if err := Build(t.Context(), BuildOptions{SourceOptions: SourceOptions{In: base, Providers: "all", Infer: true}, Out: catalog}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	update := func(from string, now time.Time) []byte {
		t.Helper()
		o := UpdateOptions{SourceOptions: SourceOptions{In: theirs, Providers: "all", Infer: true}, Base: from, Catalog: catalog, Patch: patch, Upstream: upstream}
		if err := Update(t.Context(), o, now, io.Discard); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(catalog)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	applied := update(base, first)
	if c := readTestCatalog(t, catalog); !c.Generated.Equal(first) {
		t.Fatalf("generated = %v, want %v, as the patch changed the catalog", c.Generated, first)
	}

	// The same changes again, from the same base: nothing to change.
	if again := update(base, first.Add(time.Hour)); string(again) != string(applied) {
		t.Error("update of changes the catalog holds already changed it, its generated time at least")
	}

	// From the saved catalog.json, as the next run reads it: no change of
	// models.dev's, so the patch file keeps the last change.
	if err := os.WriteFile(patch, []byte("last\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := update(upstream, first.Add(2*time.Hour)); string(again) != string(applied) {
		t.Error("update of an unchanged models.dev changed the catalog")
	}
	if data, _ := os.ReadFile(patch); string(data) != "last\n" {
		t.Errorf("patch file = %q, want it left be when models.dev changed nothing", data)
	}
}
