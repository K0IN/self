# Clef Flash GGUF

[Cloudflare Clef Flash](https://huggingface.co/Cloudflare/clef-flash) is
available as llama.cpp GGUF quantizations from
[Bartowski](https://huggingface.co/bartowski/Cloudflare_clef-flash-GGUF).

The recommended general-purpose file is `Cloudflare_clef-flash-Q4_K_M.gguf`
(5.84 GB). For image input, also download
`mmproj-Cloudflare_clef-flash-f16.gguf` (918 MB).

Run the GGUF with a recent `llama-server` release:

```bash
just serve-clef
```

This requires `llama-server` on your PATH and downloads the model through
llama.cpp's Hugging Face integration. The equivalent command is:

```bash
llama-server -hf bartowski/Cloudflare_clef-flash-GGUF:Q4_K_M
```

For a manual download, pass the projector alongside the model:

```bash
llama-server \
  -m Cloudflare_clef-flash-Q4_K_M.gguf \
  --mmproj mmproj-Cloudflare_clef-flash-f16.gguf
```

These files are not registered as a `self` model. `self` currently serves
System One decision adapters, while Clef's typed-decision behavior requires
its separate `joint_head.safetensors` runtime. The Bartowski GGUF repository
contains the llama.cpp backbone and projector, but not that joint head.
