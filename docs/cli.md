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
