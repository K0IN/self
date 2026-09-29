# 12 · Registry site (GitHub Pages)

## What

- Static site from `models/registry.yml` + model cards.
- Code: `internal/site` (templates embedded), `cmd/registry-site`.
- Markdown via `github.com/yuin/goldmark` (GFM).

## Output (`site/`)

- `index.html`: table of models (id, description, capabilities, quants, size).
- `models/<name>/<tag>.html`: rendered readme, `self serve`, quants, files, size, sha256, HF links, edit link.
- `registry.yml`: raw copy.
- `.nojekyll`.

## Commands

- `just site` -> build into `site/`.
- `just site-serve` -> preview on http://127.0.0.1:8000.
- `SITE_REPO_URL` / `-repo` -> edit / source links.

## CI

- `.github/workflows/pages.yml`.
- On push to `main` touching `models/`, site code or the workflow.
- Runs registry + site tests, builds, deploys to Pages.
- Enable: repo settings -> Pages -> Source: GitHub Actions.

## Acceptance criteria

- Must: every model gets a page. Index links to it.
- Must: missing readme fails the build.
- Must: user text is HTML-escaped (description).
- Must: bundled registry renders (`TestBuildBundledRegistry`).
- Open: visual check in a browser not done yet.
