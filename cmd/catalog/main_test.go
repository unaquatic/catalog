package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/unaquatic/catalog/data"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/cmdline"
)

// fixture is the models.dev catalog.json the gateway package tests map.
const fixture = "../../gateway/testdata/catalog.json"

func TestBuildFromFile(t *testing.T) {
	out := filepath.Join(t.ArtifactDir(), "catalog.json")
	var stderr strings.Builder
	err := Build(t.Context(), BuildOptions{SourceOptions: SourceOptions{In: fixture, Providers: "all", Infer: true}, Out: out}, io.Discard, &stderr)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatalf("decode the built catalog: %v", err)
	}
	if c.Source != fixture || len(c.Providers) != 7 || len(c.Models) != 18 || c.Generated.IsZero() {
		t.Errorf("catalog from %q with %d providers, %d models, generated %v; want the fixture's 7 and 18",
			c.Source, len(c.Providers), len(c.Models), c.Generated)
	}
	want := "catalog: 18 models, 7 providers, 34 listings grouped into 30 offerings with 11 variants and 1 aliases\n" +
		"catalog: models.dev ties 13 by canonical_model_id and 4 as a lab's own; rules tie 8 by ID, 1 as a tier, 2 as pro, 5 as inferred; 0 provider-specific\n" +
		"catalog: merged [openai/gpt-6-astra-fast]; derived [openai/gpt-6-astra-pro openai/gpt-6.1-sol-pro]\n"
	if !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("report = %q, want it to start with %q", stderr.String(), want)
	}
}

// TestBuildSavesUpstream checks that build -save-upstream writes the
// models.dev catalog.json it read, the same document, indented with its keys
// sorted, so that the same document writes the same file whatever order its
// keys come in.
func TestBuildSavesUpstream(t *testing.T) {
	dir := t.ArtifactDir()
	up := filepath.Join(dir, "models-dev.json")
	if err := buildCmd.Execute(t.Context(), []string{"-in", fixture, "-save-upstream", up, "-o", filepath.Join(dir, "catalog.json")}); err != nil {
		t.Fatalf("build: %v", err)
	}
	saved, err := os.ReadFile(up)
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(saved, &a); err != nil {
		t.Fatalf("saved upstream: %v", err)
	}
	if err := json.Unmarshal(orig, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("the saved upstream differs from the document read")
	}
	if !strings.HasPrefix(string(saved), "{\n  \"") || !strings.HasSuffix(string(saved), "}\n") {
		t.Errorf("saved upstream starts %q; want it indented", string(saved[:min(len(saved), 20)]))
	}
	again, err := Indent([]byte(`{"b": 1.50, "a": {"d": [2, "<x>"], "c": null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"a\": {\n    \"c\": null,\n    \"d\": [\n      2,\n      \"<x>\"\n    ]\n  },\n  \"b\": 1.50\n}\n"; string(again) != want {
		t.Errorf("Indent = %q, want %q: keys sorted, numbers as written, no HTML escapes", again, want)
	}
}

func TestBuildWithoutRules(t *testing.T) {
	out := filepath.Join(t.ArtifactDir(), "plain.json")
	if err := buildCmd.Execute(t.Context(), []string{"-in", fixture, "-infer=false", "-o", out}); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, _ := os.ReadFile(out)
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Model("openai/gpt-6-astra-fast"); !ok || len(c.Models) != 13 {
		t.Errorf("%d models, astra fast listed %v; want models.dev's 13, astra fast among them", len(c.Models), ok)
	}
	if _, ok := c.Providers["llmgateway"].Unlisted["gpt-6-astra"]; !ok {
		t.Errorf("llmgateway unlisted = %v, want gpt-6-astra among them without the rules", c.Providers["llmgateway"].Unlisted)
	}
}

func TestBuildProviders(t *testing.T) {
	out := filepath.Join(t.ArtifactDir(), "subset.json")
	if err := buildCmd.Execute(t.Context(), []string{"-in", fixture, "-providers", "vercel, anthropic", "-o", out}); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, _ := os.ReadFile(out)
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	// models.dev's 13, less astra fast, merged, and Vercel's Seedance 2.0
	// Fast, inferred; with OpenAI not kept, no provider offers a pro mode,
	// so no pro model is derived.
	if len(c.Providers) != 2 || len(c.Models) != 13 {
		t.Errorf("%d providers and %d models, want 2 and 13", len(c.Providers), len(c.Models))
	}
}

// TestBuildListed checks that a build with -providers listed keeps the
// providers the rules list, of those the fixture has, and notes the
// listed ones it lacks rather than failing; and that a build keeps all of
// them by default.
func TestBuildListed(t *testing.T) {
	out := filepath.Join(t.ArtifactDir(), "listed.json")
	var stderr strings.Builder
	err := Build(t.Context(), BuildOptions{SourceOptions: SourceOptions{In: fixture, Providers: "listed", Infer: true}, Out: out, Notes: true},
		io.Discard, &stderr)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	data, _ := os.ReadFile(out)
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's llmgateway is not listed.
	want := []gateway.ProviderID{"anthropic", "github-copilot", "openai", "opencode", "openrouter", "vercel"}
	if got := slices.Sorted(maps.Keys(c.Providers)); !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
	if !strings.Contains(stderr.String(), "note: zai: listed by the rules, but models.dev has no such provider") {
		t.Errorf("report lacks a note of listed zai, which the fixture lacks:\n%s", stderr.String())
	}
	var def BuildOptions
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	buildCmd.Flags(fs, &def)
	if def.Providers != "all" {
		t.Errorf("-providers defaults to %q, want all", def.Providers)
	}
}

func TestBuildErrors(t *testing.T) {
	apiJSON := filepath.Join(t.ArtifactDir(), "api.json")
	if err := os.WriteFile(apiJSON, []byte(`{"anthropic": {"id": "anthropic", "name": "Anthropic", "models": {}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	badRules := filepath.Join(t.ArtifactDir(), "bad-rules.json")
	if err := os.WriteFile(badRules, []byte(`{"version": 1, "rules": [{"match": {"maker": "x"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		args  []string
		check func(error) bool
	}{
		{"source and in", []string{"-source", "https://example.com", "-in", fixture},
			func(err error) bool { _, ok := errors.AsType[*cmdline.UsageError](err); return ok }},
		{"provider absent from the fixture", []string{"-in", fixture, "-providers", "nope", "-o", os.DevNull},
			func(err error) bool { return errors.Is(err, gateway.ErrUnknownProvider) }},
		{"api.json, not catalog.json", []string{"-in", apiJSON, "-o", os.DevNull},
			func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "not models.dev's catalog.json")
			}},
		{"missing file", []string{"-in", "nope.json"},
			func(err error) bool { return errors.Is(err, os.ErrNotExist) }},
		{"rules that say nothing", []string{"-in", fixture, "-rules", badRules, "-o", os.DevNull},
			func(err error) bool {
				return errors.Is(err, gateway.ErrRules) && strings.Contains(err.Error(), badRules)
			}},
		{"missing rules", []string{"-in", fixture, "-rules", "nope-rules.json", "-o", os.DevNull},
			func(err error) bool { return errors.Is(err, os.ErrNotExist) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := buildCmd.Execute(t.Context(), tt.args); !tt.check(err) {
				t.Errorf("build %q: err = %v", tt.args, err)
			}
		})
	}
}

// TestBuildRules checks that a build maps by the rules -rules names: with a
// rule that lists only Vercel, -providers listed keeps it alone.
func TestBuildRules(t *testing.T) {
	rules := filepath.Join(t.ArtifactDir(), "rules.json")
	if err := os.WriteFile(rules, []byte(`{"version": 1, "rules": [{"match": {"provider": "vercel"}, "listed": true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.ArtifactDir(), "vercel.json")
	if err := buildCmd.Execute(t.Context(), []string{"-in", fixture, "-rules", rules, "-providers", "listed", "-o", out}); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, _ := os.ReadFile(out)
	c, err := gateway.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(c.Providers)); !slices.Equal(got, []gateway.ProviderID{"vercel"}) {
		t.Errorf("providers = %v, want vercel alone", got)
	}
}

func TestParseIDs(t *testing.T) {
	got := ParseIDs(" openai, anthropic,,openai ")
	if want := []gateway.ProviderID{"anthropic", "openai"}; !slices.Equal(got, want) {
		t.Errorf("ParseIDs = %v, want %v", got, want)
	}
}

func TestPrintReport(t *testing.T) {
	rep := gateway.Report{Models: 3, Providers: 2, Listings: 5, Offerings: 4, Variants: 6,
		Bases: map[gateway.Basis]int{gateway.BasisCanonical: 2, gateway.BasisLab: 1, gateway.BasisID: 1, gateway.BasisNone: 1},
		Tiers: []gateway.Note{{Provider: "vercel", Model: "x-fast", Text: "tier fast of x"}},
		Notes: []gateway.Note{{Provider: "vercel", Text: "something"}}}
	var short, long strings.Builder
	PrintReport(&short, rep, false)
	PrintReport(&long, rep, true)
	wantShort := "catalog: 3 models, 2 providers, 5 listings grouped into 4 offerings with 6 variants and 0 aliases\n" +
		"catalog: models.dev ties 2 by canonical_model_id and 1 as a lab's own; rules tie 1 by ID, 0 as a tier, 0 as pro, 0 as inferred; 1 provider-specific\n" +
		"catalog: 1 tier listings, 1 notes (-notes lists them)\n"
	if short.String() != wantShort {
		t.Errorf("short report = %q, want %q", short.String(), wantShort)
	}
	if !strings.Contains(long.String(), "tier: vercel/x-fast: tier fast of x\n") ||
		!strings.Contains(long.String(), "note: vercel: something\n") {
		t.Errorf("long report = %q, want the tier offering and the note", long.String())
	}
}

func TestLoaderForCatalogFile(t *testing.T) {
	dir := t.ArtifactDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, fmt.Appendf(nil, `{"version": %d, "source": "file", "providers": {}, "models": {}}`, gateway.Version), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoaderFor(ServeOptions{Catalog: good})(t.Context())
	if err != nil || c.Source != "file" {
		t.Errorf("load %s = %+v, %v", good, c, err)
	}
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"version": 4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoaderFor(ServeOptions{Catalog: old})(t.Context()); !errors.Is(err, gateway.ErrVersion) {
		t.Errorf("load %s: err = %v, want ErrVersion", old, err)
	}
}

func TestRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, err := gateway.NewHandler(&gateway.Catalog{Version: gateway.Version, Source: "0"})
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		load := func(context.Context) (*gateway.Catalog, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("source down")
			}
			return &gateway.Catalog{Version: gateway.Version, Source: strconv.Itoa(calls)}, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			Refresh(ctx, time.Hour, load, h)
			close(done)
		}()

		for _, want := range []string{"1", "1", "3"} { // the second load fails
			time.Sleep(time.Hour)
			synctest.Wait()
			if got := h.Catalog().Source; got != want {
				t.Errorf("after %d refreshes the catalog is %q, want %q", calls, got, want)
			}
		}
		cancel()
		<-done
	})
}

// lines is a writer that sends what each Write writes.
type lines chan string

func (l lines) Write(p []byte) (int, error) {
	l <- string(p)
	return len(p), nil
}

// fixtureHandler serves the catalog built from the fixture.
func fixtureHandler(t *testing.T) *gateway.Handler {
	t.Helper()
	c, _, err := Load(t.Context(), SourceOptions{In: fixture, Providers: "all", Infer: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h, err := gateway.NewHandler(c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestListenAndServe(t *testing.T) {
	h := fixtureHandler(t)
	ctx, cancel := context.WithCancel(t.Context())
	out := make(lines, 1)
	served := make(chan error, 1)
	go func() { served <- ListenAndServe(ctx, "127.0.0.1:0", h, out) }()

	line := <-out
	_, rest, ok := strings.Cut(line, "http://")
	addr, _, _ := strings.Cut(rest, " ")
	if !ok || !strings.Contains(line, "with 18 models") {
		t.Fatalf("server says %q", line)
	}
	resp, err := http.Get("http://" + addr + "/v1/models/openai/gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ultrafast"`) {
		t.Errorf("GET gpt-6.1-sol: status %d, body %s", resp.StatusCode, body)
	}
	cancel()
	if err := <-served; err != nil {
		t.Errorf("ListenAndServe after cancel: %v", err)
	}
}

func TestSite(t *testing.T) {
	srv := httptest.NewServer(Site(fixtureHandler(t)))
	defer srv.Close()
	tests := []struct {
		path, wantType, wantBody string
		wantStatus               int
	}{
		{"/", "text/html; charset=utf-8", "<title>Catalog</title>", http.StatusOK},
		{"/catalog.json", "application/json", `"anthropic/claude-opus-5-5"`, http.StatusOK},
		{"/schema.json", "application/schema+json", `"$id": "https://aquatic.dev/catalog/schema.json"`, http.StatusOK},
		{"/v1/providers/opencode", "application/json", `"big-pickle"`, http.StatusOK},
		{"/index.html", "application/json", `"not_found"`, http.StatusNotFound},
	}
	for _, tt := range tests {
		resp, err := srv.Client().Get(srv.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.wantStatus || resp.Header.Get("Content-Type") != tt.wantType || !strings.Contains(string(body), tt.wantBody) {
			t.Errorf("GET %s: status %d, type %q, body has %q: %v", tt.path, resp.StatusCode,
				resp.Header.Get("Content-Type"), tt.wantBody, strings.Contains(string(body), tt.wantBody))
		}
	}
}

// TestPageIsSelfContained checks that the page embeds its font, type16, a
// copy of the one flow's landing page embeds, so that it needs nothing but
// itself.
func TestPageIsSelfContained(t *testing.T) {
	if !strings.Contains(string(page), `font-family: "type16"`) || !strings.Contains(string(page), "url(data:font/ttf;base64,") {
		t.Error("the page does not embed the type16 font")
	}
}

// TestSnapshot checks that snapshot keeps the providers it names, and the
// models they offer alone: of the catalog it embeds, the data package's,
// and of a catalog server's.
func TestSnapshot(t *testing.T) {
	dir := t.ArtifactDir()
	check := func(name string, o SnapshotOptions) *gateway.Catalog {
		t.Helper()
		o.Out = filepath.Join(dir, name)
		if err := Snapshot(t.Context(), o, io.Discard); err != nil {
			t.Fatal(err)
		}
		c := readTestCatalog(t, o.Out)
		if len(c.Providers) != 2 || c.Providers["anthropic"].ID == "" || c.Providers["openai"].ID == "" {
			t.Errorf("%s providers = %v, want anthropic and openai", name, slices.Sorted(maps.Keys(c.Providers)))
		}
		for id, m := range c.Models {
			if !slices.ContainsFunc(m.Providers, func(p gateway.ProviderID) bool { return p == "anthropic" || p == "openai" }) {
				t.Errorf("%s keeps %s, which neither offers", name, id)
			}
		}
		return c
	}
	embedded := check("embedded.json", SnapshotOptions{Providers: "anthropic,openai"})
	if want, err := data.Catalog(); err != nil || !embedded.Generated.Equal(want.Generated) {
		t.Errorf("embedded snapshot generated %v, want the data package's catalog's", embedded.Generated)
	}

	srv := httptest.NewServer(Site(fixtureHandler(t)))
	defer srv.Close()
	check("served.json", SnapshotOptions{Source: srv.URL, Providers: "anthropic,openai"})
}
