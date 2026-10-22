# Catalog

The model catalog of flow: built from models.dev's catalog.json by the mapping in `gateway`, following the rules in `gateway/rules.json`, and served with a page that browses it by `cmd/catalog`. Flow reads it with `gateway.Store`.

## Testing

```sh
go test ./...                                          # all tests
go test ./gateway/ -run TestFromModelsDevGolden -update   # regenerate gateway/testdata/catalog.golden.json
go test ./gateway/ -run TestSchema -update                # regenerate gateway/catalog.schema.json
go run ./cmd/catalog serve                             # browse the catalog at http://localhost:4100
go generate ./data/                                    # update data/ from models.dev
```

- A change to the mapping or the rules updates the golden catalog and commits the reviewed `testdata/catalog.golden.json` diff.
- Reasoning facts models.dev lacks (`mandatory`, `default_enabled`, `default_effort`) come from the `reasoning` rules at the end of `gateway/rules.json`, one per set of models that share them, each citing the lab's docs in `why`, and reach the models' offerings at every provider (`gateway.ApplyReasoning`). When Anthropic releases a model, add it from the "Thinking" and "Default effort" rows of platform.claude.com/docs/en/models/overview, with its dated IDs. The build notes a rule whose `mandatory` models.dev now lists itself at every provider: drop that fact from the rule.
- `gateway/catalog.schema.json` is the JSON Schema of the catalog document, in the document's own terms: `internal/jsonschema` generates its structure from `gateway.Catalog`'s JSON, names its definitions in snake_case as the document's keys are, and describes them from `gateway/catalog.schema.descriptions.json`, keyed by those names, not from Go doc comments. Write the descriptions for readers of the JSON, naming JSON keys, never Go identifiers. A change to the catalog's types updates the descriptions and regenerates the schema; generation fails on a definition or property without a description, or a description of one that is gone. `TestSchema` fails while the schema is out of date, and validates the golden catalog and `data/catalog.json` against it. Every catalog's `"$schema"` is `gateway.SchemaURL`, where CI publishes it beside `catalog.json`, and `catalog serve` serves it at `/schema.json`.
- `data/` holds models.dev's catalog.json (`models-dev.json`, keys sorted and indented) and the catalog built from it (`catalog.json`). Update both with `go generate ./data/`, and after a change to the mapping or the rules, so the committed catalog matches the code; commit them in a commit of their own.
- Flow compiles in a snapshot of the catalog (`internal/catalog/snapshot.json` in flow). Regenerate it there with `go generate ./internal/catalog/`, which reads the deployed catalog at `gateway.DefaultSource`; add `-source http://127.0.0.1:4100` to the `go:generate` line to snapshot a local `go run ./cmd/catalog serve` instead.
- CI (`.github/workflows`): `ci-<event>.yml` files only trigger, and call the reusable `run-checks.yml`, `pages-deploy.yml` and `workers-build.yml`. `refresh.yml` runs every six hours: it runs `go generate ./data/`, and when the data changed, other than its `generated` time, commits `data/` as monofufu and calls `release.yml`, which tags it `data-YYYY.MM.DD.N`, attaches both files to a GitHub release, and deploys the site to https://aquatic.dev/catalog/ (`wrangler.jsonc`) and, once the repository variable `PAGES` is `true` (GitHub Pages needs the repository public), to https://unaquatic.github.io/catalog/. Data tags are not semver, so Go ignores them; keep `vX.Y.Z` tags for releases of the code.
- `cmd/catalog/index.html` embeds type16, the font of flow's landing page (`cmd/landing/index.html` in flow). Copy the font across when either changes.

### Go 1.27+ style — IMPORTANT

This module targets **`go 1.27`** (`go.mod:3`) and the toolchain is Go 1.27.0. **Write modern Go. Do not write Go that looks like it was written in 2018.**

Required:

- **Generics over `interface{}`.** Type parameters, generic type aliases (1.24). `any`, never `interface{}`.
- **Iterators (1.23).** Return `iter.Seq[T]` / `iter.Seq2[K, V]` for sequences; range over functions. Do not hand back `[]T` copies or take visitor callbacks where an iterator fits.
- **`slices` / `maps` (1.21+).** `slices.Sort`, `slices.Contains`, `slices.Clone`, `maps.Keys`, etc. Never hand-roll these loops.
- **Builtins**: `min`, `max`, `clear` (1.21). No manual ternary-style helpers.
- **`for i := range n` (1.22)** instead of `for i := 0; i < n; i++`.
- **`errors.AsType[T](err)` (1.26)** instead of declaring a var and calling `errors.As`. Use `errors.Join` for multi-error, `%w` for wrapping.
- **`sync.WaitGroup.Go(fn)` (1.25)** instead of `wg.Add(1)` + `go func(){ defer wg.Done() }()`.
- **`testing/synctest`** for concurrency tests, `b.Loop()` (1.24) for benchmarks, `t.ArtifactDir()` (1.26) for test output files.
- **`math/rand/v2`** (never `math/rand`), `log/slog` (never `log` for structured output), `os.Root` (1.24) for sandboxed FS access.
- Loop variables are per-iteration since 1.22 — **do not** write `x := x` shadow copies.
- **Promoted fields in composite literals (1.27).** Set an embedded struct's fields directly — `Store{q: db, codec: c, db: db}`, not `Store{Objects: Objects{q: db, codec: c}, db: db}`.

Do **not** use `encoding/json/v2` — it is still `GOEXPERIMENT`-gated in 1.26 and will not build.

### Export by default

**Declare structs and methods exported (capitalised) by default.** Write `type Buffer struct` and `func (b *Buffer) Render()`, not `type buffer struct` / `func (b *buffer) render()`.

- When it is a genuinely local implementation detail that has been requested to stay private.
- This applies to struct types, struct fields, methods, and constructors.
- Naming still follows stdlib rules above: exported does not mean stuttering (`buffer.Buffer` is fine, `buffer.NewBuffer` is not) and every exported identifier needs a doc comment starting with its name.
