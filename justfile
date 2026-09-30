# self — local AI model server
#
#   just            list recipes
#   just setup      build self + fetch the bundled decision engine
#   just serve      serve the default model (kev:0.5b)

set shell := ["bash", "-euo", "pipefail", "-c"]

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

# Start the real engine for every model and check the API answers correctly.
# Downloads each model once. Add SELF_TEST_QUANTS=all or SELF_TEST_DEVICE=cpu as needed.
test-models: build bootstrap
    SELF_TEST_MODELS=all go test ./tests/models -run TestModels -v -count=1 -timeout 2h

# Same for one model, e.g. `just test-model kev:0.5b` or `just test-model kev:4b@q8`.
test-model model: build bootstrap
    SELF_TEST_MODELS='{{model}}' go test ./tests/models -run TestModels -v -count=1 -timeout 1h

# Format code
fmt:
    gofmt -w .

# Render the registry overview (GitHub Pages) into site/
site:
    cd docs && npm install && npm run build
    rm -rf site && cp -r docs/.vitepress/dist site

# Render the registry site and serve it on port 8000 (Pages builds use base /self/, see pages.yml)
site-serve: site
    cd docs && npx vitepress preview . --port 8000
