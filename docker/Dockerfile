# syntax=docker/dockerfile:1.7

ARG CUDA_VARIANT=cuda-13

FROM nvidia/cuda:13.4.1-devel-ubuntu24.04 AS builder-cuda-13
FROM nvidia/cuda:12.8.1-devel-ubuntu24.04 AS builder-cuda-12
FROM ubuntu:24.04 AS builder-cpu

FROM builder-${CUDA_VARIANT} AS builder

ARG CUDA_VARIANT
ARG ENGINE_VARIANT=cuda-13.4
ARG LAYA_VARIANT=vulkan
ARG LLAMA_CPP_TAG=b11256
ARG TARGETARCH=amd64
ARG GO_VERSION=1.26.0

ENV DEBIAN_FRONTEND=noninteractive
ENV LAYA_VARIANT=${LAYA_VARIANT}
ENV LLAMA_CPP_TAG=${LLAMA_CPP_TAG}

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        cmake \
        curl \
        gcc \
        g++ \
        git \
        libvulkan1 \
        libvulkan-dev \
        mesa-vulkan-drivers \
        ninja-build \
        pkg-config \
        tar \
        vulkan-tools \
    && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH}" in \
        amd64) go_arch=amd64 ;; \
        arm64) go_arch=arm64 ;; \
        *) echo "unsupported Go architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl --fail --location --silent --show-error \
        "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" \
        | tar -C /usr/local -xzf -
ENV PATH=/usr/local/go/bin:${PATH}

RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh \
    | bash -s -- --to /usr/local/bin --tag 1.43.0

WORKDIR /src
COPY . .

RUN go test ./... \
    && just --unstable setup \
    && just --unstable engine verify bin/libexec/ai-server "${ENGINE_VARIANT}"

FROM nvidia/cuda:13.4.1-runtime-ubuntu24.04 AS runtime-cuda-13
FROM nvidia/cuda:12.8.1-runtime-ubuntu24.04 AS runtime-cuda-12
FROM ubuntu:24.04 AS runtime-cpu

FROM runtime-${CUDA_VARIANT} AS runtime

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG CUDA_VARIANT

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libgomp1 libvulkan1 mesa-vulkan-drivers vulkan-tools \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /src/bin/self /app/self
COPY --from=builder /src/bin/libexec /app/libexec

ENV AI_SERVER_RUNTIME_DIR=/app/libexec/ai-server
ENV LD_LIBRARY_PATH=/app/libexec/ai-server/lib
ENV AI_SERVER_MODELS=/models
ENV AI_SERVER_HOST=0.0.0.0
ENV AI_SERVER_PORT=8080
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility,graphics

RUN set -eu; \
        cd "$AI_SERVER_RUNTIME_DIR"; \
        for engine in laya clef ggmlc-custom-decider ggmlc-audio llama-server; do \
            test -x "$engine"; \
        done; \
        for lib in libllama.so libllama-common.so libllama-server-impl.so libmtmd.so libggml.so libggml-base.so libggml-cpu-x64.so; do \
            test -e "lib/$lib"; \
        done; \
        case "$CUDA_VARIANT" in \
            cuda-*) test -e lib/libggml-cuda.so ;; \
            cpu) ;; \
            *) exit 1 ;; \
        esac; \
        for backend in lib/libggml-*.so*; do \
            test -L "$(basename "$backend")"; \
            test "$(readlink "$(basename "$backend")")" = "$backend"; \
            test -e "$(basename "$backend")"; \
        done; \
        missing=$(ldd laya clef ggmlc-custom-decider ggmlc-audio llama-server lib/*.so* | grep 'not found' || true); \
        case "$CUDA_VARIANT" in \
            cuda-*) missing=$(printf '%s\n' "$missing" | grep -Fv 'libcuda.so.1 => not found' || true) ;; \
        esac; \
        test -z "$missing" || { printf '%s\n' "$missing" >&2; exit 1; }

EXPOSE 8080
VOLUME ["/models"]

ENTRYPOINT ["/app/self"]
CMD ["serve", "kev:0.5b"]
