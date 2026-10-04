# 12 · Documentation and registry site (GitHub Pages)

## What

- One VitePress site in `docs/` (Node 22, `vitepress`, `yaml`): the user guide
  (`index.md`, `docker.md`, `settings.md`, `cli.md`) and API reference pages,
  plus a model catalog generated from the registry.
- Config: `docs/.vitepress/config.mjs` (`base` from `DOCS_BASE`, default `/`;
  local search; sidebar). Theme: `docs/.vitepress/theme/` (`ModelCatalog.vue`,
  `ApiField.vue`, `custom.css`).
- `docs/generate-registry.mjs` runs before every build (`npm run build`). It reads
  `models/registry.yml` (or `REGISTRY_FILE`) and the model cards and writes:
  - `docs/public/models.yml`: the exact registry text. Published as
    `https://k0in.github.io/self/models.yml`, which is the registry `self` loads (see 03).
  - `docs/public/registry.json`: catalog data (id, capabilities, quants, files, sizes).
  - `docs/registry/index.md` (search page) and `docs/registry/<name>/<tag>.md`: the model card
    plus registry details and a table of quants, adapters, files, sizes, sha256.
- A missing model card fails the build.
- `docs/public/examples/settings.yml` is the example local settings file.
- Generated and build output (all in `.gitignore`): `docs/registry/`, `docs/public/models.yml`,
  `docs/public/registry.json`, `docs/node_modules/`, `docs/.vitepress/dist/`, `site/`.

## Commands

- `just site` -> `npm install` + `npm run build` in `docs/`, then copy `docs/.vitepress/dist` to `site/`.
- `just serve-site` -> `just site`, then starts the local VitePress server on port 8000 (local builds use base `/`).
- `cd docs && npm run dev` -> live-reloading dev server.
- `DOCS_BASE=/self/` -> base path used by GitHub Pages.

## CI

- `.github/workflows/pages.yml` ("Build and deploy documentation").
- On every push to `main` (the `paths` filter is commented out) and on manual dispatch.
- Builds the site with `DOCS_BASE=/self/`, uploads `docs/.vitepress/dist`, deploys to Pages.
- It does not run the Go tests. Run `go test ./...` before merging registry changes.
- Enable: repo settings -> Pages -> Source: GitHub Actions.

## Acceptance criteria

- Must: every registry model gets a page and a catalog entry.
- Must: missing readme fails the build (`generate-registry.mjs`) and `go test` (`internal/registry` `TestBundledRegistry`).
- Must: after a build, `docs/public/models.yml` is byte-identical to `models/registry.yml` (copies in a working tree can be stale until the next build).
- Open: visual check in a browser not done yet.
