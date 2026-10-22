<p align="center">
  <a href="https://github.com/unaquatic/catalog">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
      <img src="docs/logo-light.svg" alt="catalog">
    </picture>
  </a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27-555?labelColor=888&logo=go&logoColor=white&style=flat-square" alt="Go"></a>
  <a href="https://github.com/unaquatic/catalog/blob/main/LICENSE"><img src="https://img.shields.io/github/license/unaquatic/catalog?labelColor=888&color=555&style=flat-square" alt="License"></a>
</p>

<p align="center"><b>The model catalog of flow, built from models.dev, written in golang.</b></p>

---

## Install

```sh
go get github.com/unaquatic/catalog
```

## Usage

```sh
go run ./cmd/catalog build -o catalog.json        # build the catalog from models.dev
go run ./cmd/catalog serve                        # serve it, and browse it at http://localhost:4100
go run ./cmd/catalog snapshot -providers anthropic,openai -o snapshot.json   # the catalog flow compiles in
go generate ./data/                               # update data/ from models.dev
go run ./cmd/catalog serve -in data/models-dev.json   # serve the committed data, with no network
```

## Hosted catalog

CI rebuilds the catalog from models.dev every six hours and, when it changed, commits `data/`, publishes a `data-YYYY.MM.DD.N` release with both files, and deploys the browser and `catalog.json` to:

- https://aquatic.dev/catalog/ — what flow reads (`gateway.DefaultSource`)
- https://unaquatic.github.io/catalog/ — a mirror

`catalog.json` names its JSON Schema (draft 2020-12) in `"$schema"`: https://aquatic.dev/catalog/schema.json, generated from the Go types into `gateway/catalog.schema.json`.

## Packages

- `gateway` — the catalog format, the mapping from models.dev to it and its rules (`gateway/rules.json`), the HTTP handler, and the `Store` that clients such as flow read it with.
- `gateway/gatewaytest` — catalogs for tests, built from models.dev api.json documents.
- `modelsdev` — models.dev's catalog.json, and the cache and fetcher of downloads.
- `cmd/catalog` — builds the catalog, serves it with a page that browses it, and writes snapshots.
- `data` — models.dev's catalog.json (`models-dev.json`) and the catalog built from it (`catalog.json`), committed so that each update is a diff.

## License

Catalog is released under the [MIT License](LICENSE).
