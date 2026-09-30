# Local settings

Local settings are optional YAML overrides for engine parameters. They let one machine tune threads, context, or acceleration without changing the public registry. The default file is `~/.ai-server/settings.yml`; set `AI_SERVER_SETTINGS` or pass `--settings-file` to use another path.

```yaml
version: 1

adapters:
  ggmlc-custom-decider:
    threads: 8
  ggmlc-laya:
    threads: 6

models:
  "decider-vision:2b":
    settings:
      context_size: 4096
    quants:
      8bit:
        flash_attn: "on"
```

## Precedence

Values are merged from lowest to highest priority:

1. Registry model `settings`.
2. Registry quant settings.
3. `adapters.<adapter>` settings in the local file, applied to every model using that adapter.
4. `models.<id>.settings` in the local file.
5. `models.<id>.quants.<quant>` in the local file.
6. Repeated `--set key=value` flags.

Later layers replace the same key; unrelated keys remain. Use `self settings <model>` to print the effective values and their source before starting a server.

## File rules

An omitted default file is fine. A path supplied explicitly by `--settings-file` or `AI_SERVER_SETTINGS` must exist and parse as version 1 YAML. Keep keys within the adapter's supported settings; unknown or invalid values are rejected before the engine starts.

```bash
self settings decider:2b-vision@4bit
self serve decider:2b-vision@4bit --settings-file ./settings.yml
self serve kev:4b@8bit --set threads=8 --set context_size=4096
```

For Docker, mount the settings file read-only and pass its path inside the container:

```bash
docker run --rm -p 8080:8080 \
  -v "$HOME/.ai-server/models:/models" \
  -v "$HOME/.ai-server/settings.yml:/etc/self/settings.yml:ro" \
  ghcr.io/k0in/self:latest serve kev:4b@8bit --host 0.0.0.0 \
  --settings-file /etc/self/settings.yml
```
