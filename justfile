# self — local AI model server
#
#   just            list recipes
#   just setup      build self + install all bundled engines
#   just serve      serve the default model (kev:0.5b)

set shell := ["bash", "-euo", "pipefail", "-c"]

mod engine "engines"

bin         := "bin"
engine_dir  := bin / "libexec/ai-server"
ggmlc       := env_var_or_default("GGMLC_VERSION", "v0.9.6")
model       := env_var_or_default("MODEL", "kev:0.5b")
port        := env_var_or_default("PORT", "8080")
variant     := env_var_or_default("ENGINE_VARIANT", "cuda-13.4")
laya_variant := env_var_or_default("LAYA_VARIANT", "auto")

# Dev runs use the checked-out registry, not the published one.
export AI_SERVER_REGISTRY := env_var_or_default("AI_SERVER_REGISTRY", justfile_directory() / "models/registry.yml")
# Dev runs print the engine command and engine/request logs; AI_SERVER_VERBOSE=0 turns it off.
export AI_SERVER_VERBOSE := env_var_or_default("AI_SERVER_VERBOSE", "1")

default:
    @just --list

# Build self and all bundled engines.
setup: build (runtime laya_variant) (engine-decider variant) && verify-engines

[private]
verify-engines:
    just engine verify "{{justfile_directory()}}/{{engine_dir}}" "{{variant}}"

# Build the self binary into bin/
build:
    go build -trimpath -o {{bin}}/self ./cmd/self

[private]
runtime variant="auto":
    just engine runtime {{variant}} {{ggmlc}}

[private]
engine-decider variant="cuda-13.4":
    just engine build {{variant}}

[private]
bootstrap:
        #!/usr/bin/env bash
        set -euo pipefail
        if [[ ! -x "{{engine_dir}}/laya" ]] || ! LD_LIBRARY_PATH="{{engine_dir}}/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" "{{engine_dir}}/laya" help >/dev/null 2>&1; then
            rm -f "{{engine_dir}}/laya"
            just runtime
        fi
        if [[ ! -x "{{engine_dir}}/ggmlc-custom-decider" || ! -x "{{engine_dir}}/clef" || ! -x "{{engine_dir}}/llama-server" || ! -e "{{engine_dir}}/lib/libllama-server-impl.so" ]]; then
            just engine-decider
        fi
        if [[ ! -x "{{engine_dir}}/ggmlc-audio" ]]; then
            just engine audio "${AUDIO_VARIANT:-}"
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
