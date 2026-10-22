# Catalog

The model catalog of flow: `data/catalog.json`, our own data and its source of truth, which takes models.dev's changes as patches, through the mapping in `gateway` and the rules in `gateway/rules.json`, and which `cmd/catalog` serves with a page that browses it. Flow reads it with `gateway.Store`.

## Testing

```sh
go test ./...                                          # all tests
go test ./gateway/ -run TestFromModelsDevGolden -update   # regenerate gateway/testdata/catalog.golden.json
go test ./gateway/ -run TestSchema -update                # regenerate gateway/catalog.schema.json
go run ./cmd/catalog serve                             # browse the catalog at http://localhost:4100
go generate ./data/                                    # apply models.dev's changes to data/catalog.json
go run ./cmd/catalog remap -in data/models-dev.json     # write remap.json: how the current mapping differs from data/catalog.json
go run ./cmd/catalog apply -patch remap.json            # apply a reviewed patch to data/catalog.json
```

- A change to the mapping or the rules updates the golden catalog and commits the reviewed `testdata/catalog.golden.json` diff.
- Reasoning facts models.dev lacks (`mandatory`, `default_enabled`, `default_effort`) come from the `reasoning` rules at the end of `gateway/rules.json`, one per set of models that share them, each citing the lab's docs in `why`, and reach the offerings of the models that models.dev's changes add, at every provider (`gateway.ApplyReasoning`); change a fact of a model the catalog holds in `data/catalog.json`, or through remap. When Anthropic releases a model, add it from the "Thinking" and "Default effort" rows of platform.claude.com/docs/en/models/overview, with its dated IDs. The build notes a rule whose `mandatory` models.dev now lists itself at every provider: drop that fact from the rule.
- The catalog names the reasoning efforts its offerings take (`efforts`) and the tiers and modes its variants run in (`variant.tiers`, `variant.modes`), each its ID with a capital first letter (`medium` is Medium) unless a `name` rule matching its `effort`, `tier` or `mode` in `gateway/rules.json` names it otherwise (`xhigh` is Extra High).
- `gateway/catalog.schema.json` is the JSON Schema of the catalog document, in the document's own terms: `internal/jsonschema` generates its structure from `gateway.Catalog`'s JSON, names its definitions in snake_case as the document's keys are, and describes them from `gateway/catalog.schema.descriptions.json`, keyed by those names, not from Go doc comments. Write the descriptions for readers of the JSON, naming JSON keys, never Go identifiers. A change to the catalog's types updates the descriptions and regenerates the schema; generation fails on a definition or property without a description, or a description of one that is gone. `TestSchema` fails while the schema is out of date, and validates the golden catalog and `data/catalog.json` against it. Every catalog's `"$schema"` is `gateway.SchemaURL`, where CI publishes it beside `catalog.json`, and `catalog serve` serves it at `/schema.json`.
- `data/catalog.json` is our catalog and its source of truth: edit it by hand for what models.dev lacks or has wrong. `go generate ./data/` runs `catalog update`, which maps `data/models-dev.json`, the models.dev version the catalog last took changes from, and models.dev's current catalog.json alike, diffs them, and applies the diff to `data/catalog.json` as a three-way merge (`internal/merge`): it takes what models.dev changed, keeps what the catalog holds of its own, drops what models.dev drops, and keeps the catalog's value where both changed one, listing it as a conflict in `data/patch.json`, which holds the whole patch. A change to the mapping or the rules does not reach the catalog that way, since both versions map alike: run `catalog remap -in data/models-dev.json`, review `remap.json`, keep the operations to apply, which apply drops our own data with too, and run `catalog apply`. Commit `data/` in a commit of its own.
- A change of the catalog's format bumps `gateway.Version`, and migrates `data/catalog.json` in the same commit: `update`, `remap` and `apply` read our catalog only at the current version, so the next refresh fails on one left behind. Set its `version` to the new one, run `catalog remap -in data/models-dev.json`, review `remap.json`, keeping the operations of the format change and dropping those that would undo our own data, and run `catalog apply`.
- Flow compiles in a snapshot of the catalog (`internal/catalog/snapshot.json` in flow). Regenerate it there with `go generate ./internal/catalog/`, which reads the deployed catalog at `gateway.DefaultSource`; add `-source http://127.0.0.1:4100` to the `go:generate` line to snapshot a local `go run ./cmd/catalog serve` instead.
- CI (`.github/workflows`): `ci-<event>.yml` files only trigger, and call the reusable `run-checks.yml`, `pages-deploy.yml` and `workers-build.yml`. `refresh.yml` runs every six hours: it runs `go generate ./data/` and the tests, and when anything in `data/` changed, force-pushes it as one commit, by the repository variables `GIT_USER_NAME` and `GIT_USER_EMAIL`, or else GitHub Actions' bot, to the branch `data/refresh` and opens a pull request of it, or updates the one open; when nothing changed, it closes that one. Its body takes the shape of `.opencode/commands/pr.md` in flow (Summary, Verification, Notes for reviewers), as `.github/scripts/refresh-pr-body.sh` writes it, and its one commit the repository's commit style. Review the pull request, `data/patch.json` and its conflicts above all, and squash-merge it: the repository squash-merges alone, with the commit's subject and message as the squash commit's. A push to main that changes `data/catalog.json`, as that merge is, makes `ci-develop.yml` call `release.yml`, which tags it `data-YYYY.MM.DD.N`, attaches the catalog, models.dev's catalog.json and the patch to a GitHub release, and deploys the site to https://aquatic.dev/catalog/ (`wrangler.jsonc`) and, once the repository variable `PAGES` is `true` (GitHub Pages needs the repository public), to https://unaquatic.github.io/catalog/. Data tags are not semver, so Go ignores them; keep `vX.Y.Z` tags for releases of the code.
- The repository's settings, which the flow above needs: squash merging alone, its commit titled and described as the pull request's one commit is, branches deleted on merge, and GitHub Actions allowed to open pull requests:
  ```sh
  gh api -X PATCH repos/unaquatic/catalog -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false -f squash_merge_commit_title=COMMIT_OR_PR_TITLE -f squash_merge_commit_message=COMMIT_MESSAGES -F delete_branch_on_merge=true
  gh api -X PUT repos/unaquatic/catalog/actions/permissions/workflow -f default_workflow_permissions=read -F can_approve_pull_request_reviews=true
  ```
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
