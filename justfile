# self — local AI model server
#
#   just            list recipes
#   just setup      build self + fetch the bundled decision engine
#   just serve      serve the default model (kev:0.5b)

set shell := ["bash", "-euo", "pipefail", "-c"]

[private]
mod engine "engines"

bin         := "bin"
engine_dir  := bin / "libexec/ai-server"
ggmlc       := env_var_or_default("GGMLC_VERSION", "v0.9.6")
model       := env_var_or_default("MODEL", "kev:0.5b")
port        := env_var_or_default("PORT", "8080")

default:
    @just --list

# Build self and all bundled engines.
setup: build runtime engine-decider

# Build the self binary into bin/
build:
    go build -trimpath -o {{bin}}/self ./cmd/self

[private]
runtime variant="auto":
    just engine runtime {{variant}} {{ggmlc}}

[private]
engine-decider variant="cuda-12.8":
    just engine build {{variant}}

[private]
bootstrap:
        #!/usr/bin/env bash
        set -euo pipefail
        if [[ ! -x "{{engine_dir}}/laya" ]] || ! LD_LIBRARY_PATH="{{engine_dir}}/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" "{{engine_dir}}/laya" help >/dev/null 2>&1; then
            rm -f "{{engine_dir}}/laya"
            just runtime
        fi
        if [[ ! -x "{{engine_dir}}/ggmlc-custom-decider" ]]; then
            just engine-decider
        fi

# Serve a model.
serve m=model *flags="": build bootstrap
    {{bin}}/self serve {{m}} --port {{port}} {{flags}}

# gofmt + go vet + tests
check:
    test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
    go vet ./...
    go test ./...

# Format code
fmt:
    gofmt -w .

# Render the registry overview (GitHub Pages) into site/
site:
    cd docs && npm install && npm run build
    rm -rf site && cp -r docs/.vitepress/dist site

# Render the registry site and serve it on http://127.0.0.1:8000
site-serve: site
    python3 -m http.server 8000 --bind 127.0.0.1 --directory site
