FROM nvidia/cuda:13.4.1-devel-ubuntu24.04

ARG GO_VERSION=1.27.1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates cmake curl fish git just nodejs npm ninja-build ripgrep sqlite3 zsh libvulkan1 mesa-vulkan-drivers \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz \
    && useradd --create-home --shell /usr/bin/fish vscode \
    && mkdir -p /workspaces \
    && chown vscode:vscode /workspaces \
    && mkdir -p /home/ubuntu/go \
    && chown -R ubuntu:ubuntu /home/ubuntu/go

ENV PATH=/usr/local/go/bin:${PATH} \
    GOPATH=/home/ubuntu/go

RUN printf '\nalias self="go run /workspaces/universal-ai-server/cmd/self"\n' >> /etc/bash.bashrc \
    && printf '\nalias self="go run /workspaces/universal-ai-server/cmd/self"\n' >> /etc/zsh/zshrc \
    && mkdir -p /etc/fish/conf.d \
    && printf 'alias self "go run /workspaces/universal-ai-server/cmd/self"\n' > /etc/fish/conf.d/self.fish \
    && chsh -s /usr/bin/fish vscode

USER vscode
WORKDIR /workspaces/universal-ai-server
