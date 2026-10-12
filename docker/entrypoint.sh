#!/bin/sh
set -eu

case "${AI_SERVER_CUDA_VARIANT:-cpu}" in
    cuda-*)
        gpu_ids=$(nvidia-smi --query-gpu=uuid --format=csv,noheader 2>/dev/null) || gpu_ids=
        case "$gpu_ids" in
            GPU-*) ;;
            *)
                echo "Error: this CUDA container requires an NVIDIA GPU, but no GPU is accessible. Start it with --gpus=all and ensure the host NVIDIA driver and NVIDIA Container Toolkit are installed, or use the CPU image." >&2
                exit 1
                ;;
        esac
        ;;
esac

exec /app/self "$@"