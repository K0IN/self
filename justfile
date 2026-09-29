# self — local AI model server
#
#   just            list recipes
#   just setup      build self + fetch the bundled decision engine
#   just serve      serve the default model (kev:0.5b)

set shell := ["bash", "-euo", "pipefail", "-c"]

# Engine build recipes: `just engine build`, `just engine deps`, ...
# The dispatcher forwards these to the specific engine justfiles.
mod engine "engines"

bin         := "bin"
engine_dir  := bin / "libexec/ai-server"
ggmlc       := env_var_or_default("GGMLC_VERSION", "v0.9.6")
model       := env_var_or_default("MODEL", "kev:0.5b")
port        := env_var_or_default("PORT", "8080")
url         := "http://127.0.0.1:" + port

default:
    @just --list

# Build self + fetch/build all engines (one-time setup)
setup: build runtime engine-decider

# Build the self binary into bin/
build:
    go build -trimpath -o {{bin}}/self ./cmd/self

# Download the upstream ggmlc Laya engine into bin/libexec/ai-server (variant: auto | cuda-sm80 | cuda-sm86 | cuda-sm89 | vulkan)
runtime variant="auto":
    just engine runtime {{variant}} {{ggmlc}}

# Build our llama.cpp-based Decider engine into bin/libexec/ai-server (variant: cuda-12.8 | vulkan | cpu)
engine-decider variant="cuda-12.8":
    just engine build {{variant}}

# Build missing engines required by the selected model.
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

# --- onboarding new models ---

# Inspect a Hugging Face GGUF repo and print a registry entry: just onboard Mapika/decider-4b-GGUF
onboard repo *flags="": build
    {{bin}}/self onboard {{repo}} {{flags}}

# Download + start + probe a registry model: just check-model decider:4b
check-model m=model *flags="": build bootstrap
    {{bin}}/self check {{m}} {{flags}}

# Serve a model: just serve kev:4b --port 9000
serve m=model *flags="": build bootstrap
    {{bin}}/self serve {{m}} --port {{port}} {{flags}}

# Serve with engine + request logs
serve-verbose m=model *flags="": build bootstrap
    {{bin}}/self serve {{m}} --port {{port}} --verbose {{flags}}

# Serve on CPU only
serve-cpu m=model *flags="": build bootstrap
    {{bin}}/self serve {{m}} --port {{port}} --device cpu {{flags}}

# Alias command that requires a decision model
decision m=model *flags="": build bootstrap
    {{bin}}/self decision {{m}} --port {{port}} {{flags}}

# Download a model without serving: just pull kev:4b 8bit
pull m=model quant="": build
    {{bin}}/self pull {{m}} {{ if quant != "" { "--quant " + quant } else { "" } }}

# List registry models and what is downloaded
list: build
    {{bin}}/self list

# Unit tests
test:
    go test ./...

# Unit tests with the race detector
test-race:
    go test -race ./...

# Integration test against the real engine (downloads kev:0.5b if missing)
test-integration: build
    #!/usr/bin/env bash
    set -euo pipefail
    gguf=$({{bin}}/self pull kev:0.5b)
    SELF_TEST_ENGINE="$PWD/{{engine_dir}}/laya" SELF_TEST_GGUF="$gguf" \
      go test ./internal/adapters/ggmlclaya -run Integration -count=1 -v

# gofmt + go vet + tests
check:
    test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
    go vet ./...
    go test ./...

# Format code
fmt:
    gofmt -w .

# --- smoke requests against a running server (PORT=8080) ---

# GET /health
health:
    curl -s {{url}}/health; echo

# GET /v1/model
model-info:
    curl -s {{url}}/v1/model; echo

# POST a text System One request
try-text:
    curl -s {{url}}/v1/systemone -H 'Content-Type: application/json' -d @examples/text.json; echo

# POST all example requests in examples/
try-all:
    #!/usr/bin/env bash
    for f in examples/*.json; do
      echo "== $f"
      curl -s {{url}}/v1/systemone -H 'Content-Type: application/json' -d @"$f"; echo
    done

# Package a release tarball: dist/self-<os>-<arch>.tar.gz
release: build
    #!/usr/bin/env bash
    set -euo pipefail
    name="self-$(go env GOOS)-$(go env GOARCH)"
    rm -rf dist/$name && mkdir -p dist/$name
    cp {{bin}}/self dist/$name/
    cp -r {{bin}}/libexec dist/$name/
    tar -C dist -czf dist/$name.tar.gz $name
    echo "dist/$name.tar.gz"

# Show effective engine settings of a model and where each value comes from
settings m=model *flags="": build
    {{bin}}/self settings {{m}} {{flags}}

# Render the registry overview (GitHub Pages) into site/
site:
    go run ./cmd/registry-site -out site

# Render the registry site and serve it on http://127.0.0.1:8000
site-serve: site
    python3 -m http.server 8000 --bind 127.0.0.1 --directory site

# Remove build output (keeps downloaded models)
clean:
    rm -rf {{bin}}/self dist site
