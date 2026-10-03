# syntax=docker/dockerfile:1.7

ARG CUDA_VARIANT=cpu

FROM nvidia/cuda:13.4.1-runtime-ubuntu24.04 AS runtime-cuda-13
FROM nvidia/cuda:12.8.1-runtime-ubuntu24.04 AS runtime-cuda-12
FROM ubuntu:24.04 AS runtime-cpu

FROM runtime-${CUDA_VARIANT} AS runtime

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG CUDA_VARIANT

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libgomp1 libvulkan1 mesa-vulkan-drivers \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY bin/self /app/self
COPY bin/libexec /app/libexec

ENV AI_SERVER_RUNTIME_DIR=/app/libexec/ai-server
ENV LD_LIBRARY_PATH=/app/libexec/ai-server/lib
ENV AI_SERVER_MODELS=/models
ENV AI_SERVER_HOST=0.0.0.0
ENV AI_SERVER_PORT=8080
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

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
        test -z "$missing" || { printf '%s\n' "$missing" >&2; exit 1; }

EXPOSE 8080
VOLUME ["/models"]

ENTRYPOINT ["/app/self"]
CMD ["serve", "kev:0.5b"]
