FROM mcr.microsoft.com/devcontainers/go:2-1.27-trixie

RUN apt-get update \
    && apt-get install -y --no-install-recommends fish just ripgrep sqlite3 libvulkan1 mesa-vulkan-drivers \
    && rm -rf /var/lib/apt/lists/* \
    && chsh -s /usr/bin/fish vscode
