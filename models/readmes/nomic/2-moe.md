# Nomic Embed v2 MoE

Nomic Embed v2 is a multilingual mixture-of-experts text embedding model with
Matryoshka representations. It is served through llama.cpp's embeddings API.

Source: [nomic-ai/nomic-embed-text-v2-moe](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe).
GGUF: [nomic-ai/nomic-embed-text-v2-moe-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF).
License: Apache-2.0.

```bash
self serve nomic:2-moe
```

The model supports multilingual retrieval and task prefixes. The default
response keeps the full vector dimension; dimensionality reduction can be
applied by a downstream vector store when appropriate.