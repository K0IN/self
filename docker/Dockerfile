# syntax=docker/dockerfile:1.7

ARG CUDA_VARIANT=cpu

FROM nvidia/cuda:13.4.1-runtime-ubuntu24.04 AS runtime-cuda-13
FROM nvidia/cuda:12.8.1-runtime-ubuntu24.04 AS runtime-cuda-12
FROM ubuntu:24.04 AS runtime-cpu

FROM runtime-${CUDA_VARIANT} AS runtime

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libvulkan1 mesa-vulkan-drivers \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY bin/self /app/self
COPY bin/libexec /app/libexec

ENV AI_SERVER_RUNTIME_DIR=/app/libexec/ai-server
ENV AI_SERVER_MODELS=/models
ENV AI_SERVER_HOST=0.0.0.0
ENV AI_SERVER_PORT=8080
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

EXPOSE 8080
VOLUME ["/models"]

ENTRYPOINT ["/app/self"]
CMD ["serve", "kev:0.5b"]
