# convaiinnovations/Laya Multilingual 322M

- **License:** [Apache-2.0](https://huggingface.co/convaiinnovations/laya-multilingual/blob/main/README.md)
- **Release date:** Not documented in the original card; [2026-09-19](https://huggingface.co/api/models/convaiinnovations/laya-multilingual) is the first HF publication (repository creation).
- **Modalities:** Input: text; Output: typed decisions (`choice`, `score`, `noul`)
- **Original model:** [convaiinnovations/laya-multilingual](https://huggingface.co/convaiinnovations/laya-multilingual)

## Description

Laya Multilingual is a non-autoregressive decision model built on mmBERT-base
with a trained decision head. It reads a state and typed questions and returns
option probabilities in one forward pass, without text generation. The
checkpoint covers more than 100 languages and is the family's alternative to
the English ModernBERT checkpoint for non-English inputs.

## Run

```bash
self serve laya-multilingual:322m
```

Here is a link to the API docs: [Decision API](/api/decision).

### Example Request

With the server running, open another terminal and send a request:

```bash
curl http://localhost:8080/v1/systemone \
	-H 'Content-Type: application/json' \
	-d '{
		"state": "Me cobraron dos veces por el mismo pedido.",
		"questions": {
			"department": {
				"type": "choice",
				"instructions": "Which department should handle this support ticket?",
				"criteria": {
					"billing": "Payments and refunds",
					"technical": "Technical problems"
				}
			}
		}
	}'
```

The server returns a JSON decision for the `department` question.

## Engine Parameters

Served by the upstream `laya` daemon through the `ggmlc-laya` adapter.
These are startup settings, not per-request API fields.

| Parameter | Default | What it controls |
| --- | --- | --- |
| `threads` | `4` | Number of CPU workers; accepts integers from 1 to 1024. |
| `cuda_graph` | `true` for `auto` / CUDA devices; otherwise `false` | Capture a CUDA graph for the live request shape. |

Override startup settings with `--set <parameter>=<value>`.

## Quantizations and Files

Default variant: `q8`. Sizes and SHA-256 hashes are pinned in the model registry.

### q4

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_multilingual_ud_q4_k_m.gguf` | model | 499.6 MiB (523916512 bytes) | `7c0e47f3dfaf1242929d69e91afb8868fcc5ee78092e7784e1aa2ee85adc9840` | [mys/laya-multilingual-GGUF](https://huggingface.co/mys/laya-multilingual-GGUF/blob/main/laya_multilingual_ud_q4_k_m.gguf) |

Total download: 499.6 MiB (523916512 bytes).

### q8 (default)

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_multilingual_q8_0.gguf` | model | 345.0 MiB (361712736 bytes) | `757c1a4b1f0f41824113dde76d6cd06b0881d37b796103a338de77f9f9b935c3` | [mys/laya-multilingual-GGUF](https://huggingface.co/mys/laya-multilingual-GGUF/blob/main/laya_multilingual_q8_0.gguf) |

Total download: 345.0 MiB (361712736 bytes).

## Notes

- Text only; up to 16 options per question. Language coverage does not imply equal accuracy, especially for low-resource languages.
- The ggmlc-compiled graph has a 1024-token context budget; context and option limits cannot be changed at runtime. Upstream Python's 8192-token override is not an adapter setting here.
- Upstream reports overconfidence, weak zero-shot typed-workflow accuracy, position bias in ordinal scores and under-reported `noul` positives. Validate outputs on your own data.
- This server loads one checkpoint; it does not automatically route English inputs to the English model.
- The registered q4 file is larger than q8; sizes reflect the actual compiled artifacts, not a conventional quantization size ordering.
- Download size is not the total runtime memory requirement.
- Origin links use `main` because these registry entries pin hashes, not repository revisions.
