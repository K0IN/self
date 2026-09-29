FROM mcr.microsoft.com/devcontainers/go:2-1.27-trixie

RUN apt-get update \
    && apt-get install -y --no-install-recommends fish just ripgrep sqlite3 libvulkan1 mesa-vulkan-drivers \
    && rm -rf /var/lib/apt/lists/* \
    && printf '\nalias self="go run /workspaces/universal-ai-server/cmd/self"\n' >> /etc/bash.bashrc \
    && printf '\nalias self="go run /workspaces/universal-ai-server/cmd/self"\n' >> /etc/zsh/zshrc \
    && mkdir -p /etc/fish/conf.d \
    && printf 'alias self "go run /workspaces/universal-ai-server/cmd/self"\n' > /etc/fish/conf.d/self.fish \
    && chsh -s /usr/bin/fish vscode
