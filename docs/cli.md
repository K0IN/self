# CLI and completions

The CLI uses Cobra. Run `self --help` for command help and `self completion` for generated shell scripts.

## Install completion

Bash:

```bash
mkdir -p ~/.local/share/bash-completion/completions
self completion bash > ~/.local/share/bash-completion/completions/self
```

Zsh:

```bash
mkdir -p ~/.zfunc
self completion zsh > ~/.zfunc/_self
fpath=(~/.zfunc $fpath)
autoload -U compinit && compinit
```

Fish:

```bash
mkdir -p ~/.config/fish/completions
self completion fish > ~/.config/fish/completions/self.fish
```

The completion files are generated from the installed binary, so regenerate them after upgrading `self`.

## Use the Docker image as `self`

If you do not want to install the native binary, define a shell function that
forwards every argument to the container. Add the function for your shell to
your profile, such as `~/.bashrc` or `~/.zshrc`:

Bash and Zsh:

```bash
self() {
	docker run --rm \
		--publish 8080:8080 \
		--volume "$HOME/.ai-server/models:/models" \
		ghcr.io/k0in/self:latest "$@"
}
```

Fish:

```fish
function self
		docker run --rm \
				--publish 8080:8080 \
				--volume "$HOME/.ai-server/models:/models" \
				ghcr.io/k0in/self:latest $argv
end
```

Reload the profile, then use the command as usual:

```bash
self serve kev:4b --host 0.0.0.0 --port 8080
self ls
self completion bash
```

The container image starts `kev:0.5b` when no command is supplied. Use the
`cpu` image tag instead of `latest` on a host without NVIDIA CUDA support:

```bash
ghcr.io/k0in/self:cpu
```
