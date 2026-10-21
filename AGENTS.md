# Catalog

The model catalog of flow: built from models.dev's catalog.json by the mapping in `gateway`, following the rules in `gateway/rules.json`, and served with a page that browses it by `cmd/catalog`. Flow reads it with `gateway.Store`.

## Testing

```sh
go test ./...                                          # all tests
go test ./gateway/ -run TestFromModelsDevGolden -update   # regenerate gateway/testdata/catalog.golden.json
go run ./cmd/catalog serve                             # browse the catalog at http://localhost:4100
```

- A change to the mapping or the rules updates the golden catalog and commits the reviewed `testdata/catalog.golden.json` diff.
- Flow compiles in a snapshot of the catalog (`internal/catalog/snapshot.json` in flow). Regenerate it there with `go generate ./internal/catalog/` while `go run ./cmd/catalog serve` runs.
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
