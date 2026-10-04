# convaiinnovations/Laya English 421M

- **License:** [Apache-2.0](https://huggingface.co/convaiinnovations/laya/blob/main/README.md)
- **Release date:** Not documented in the original card; [2026-09-18](https://huggingface.co/api/models/convaiinnovations/laya) is the first HF publication (repository creation).
- **Modalities:** Input: text; Output: typed decisions (`choice`, `score`, `noul`)
- **Original model:** [convaiinnovations/laya](https://huggingface.co/convaiinnovations/laya)

## Description

Laya English combines a bidirectional ModernBERT-large encoder with a decision
head, answering typed questions about text or JSON states without generating
text. Its option-marker scorer supports request-defined choices, and training
uses reinforcement learning with proper scoring-rule rewards. The English
checkpoint targets tasks such as guardrails, routing and email triage.

## Run

```bash
self serve laya-english:421m
```

Here is a link to the API docs: [Decision API](/api/decision).

### Example Request

With the server running, open another terminal and send a request:

```bash
curl http://localhost:8080/v1/systemone \
	-H 'Content-Type: application/json' \
	-d '{
		"state": "I was charged twice for the same order.",
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

Default variant: `q4`. Sizes and SHA-256 hashes are pinned in the model registry.

### q4 (default)

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_english_ud_q4_k_m.gguf` | model | 400.5 MiB (419907712 bytes) | `e504de8fcac28e5dae1cf531fe1e1c0912703de5b82719ad84bd616e1257d835` | [mys/laya-GGUF](https://huggingface.co/mys/laya-GGUF/blob/main/laya_english_ud_q4_k_m.gguf) |

Total download: 400.5 MiB (419907712 bytes).

### q8

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_english_q8_0.gguf` | model | 430.6 MiB (451505440 bytes) | `6243305fb16cf22e53b932c220bd029be2adbfbec6ce120356517b8ae2f38382` | [mys/laya-GGUF](https://huggingface.co/mys/laya-GGUF/blob/main/laya_english_q8_0.gguf) |

Total download: 430.6 MiB (451505440 bytes).

### fp16

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_english_f16.gguf` | model | 806.9 MiB (846137888 bytes) | `ebeb7c65a3284bc07729179a4c5f19dfa2c3181b7519fd2beb02f4b0d37e8b5c` | [mys/laya-GGUF](https://huggingface.co/mys/laya-GGUF/blob/main/laya_english_f16.gguf) |

Total download: 806.9 MiB (846137888 bytes).

## Notes

- Text only; English only; up to 16 options per question. Non-English inputs can receive confidently wrong answers.
- The ggmlc-compiled graph has a 512-token context budget; context and option limits cannot be changed at runtime.
- The base model is weak on the typed-decisions workflows zero-shot. Upstream reports overconfidence, weak ordinal scores and label-sensitive `noul` answers; validate on your own data.
- This server loads one checkpoint, not the original Python family's automatic language router.
- Download size is not the total runtime memory requirement.
- Origin links use `main` because these registry entries pin hashes, not repository revisions.
