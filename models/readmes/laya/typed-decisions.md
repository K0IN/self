# convaiinnovations/Laya Typed Decisions 421M

- **License:** [Apache-2.0](https://huggingface.co/convaiinnovations/laya-typed-decisions/blob/main/README.md)
- **Release date:** Not documented in the original card; [2026-09-18](https://huggingface.co/api/models/convaiinnovations/laya-typed-decisions) is the first HF publication (repository creation).
- **Modalities:** Input: text; Output: typed decisions (`choice`, `score`, `noul`)
- **Original model:** [convaiinnovations/laya-typed-decisions](https://huggingface.co/convaiinnovations/laya-typed-decisions)

## Description

Laya Typed Decisions fine-tunes the English ModernBERT-large decision model
on the typed-decisions benchmark's training split. It specializes in four
workflows: agent-trace observability, customer service, invoice processing
and security incidents. Like the base Laya model, it returns typed answers
and probabilities without generating text.

## Run

```bash
self serve laya-typed-decisions:421m
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
| `laya_typed_decisions_ud_q4_k_m.gguf` | model | 404.0 MiB (423581888 bytes) | `42919cae9c11f744a16b8f3136a3356c586a558108080050e832ba14bbb0010c` | [mys/laya-typed-decisions-GGUF](https://huggingface.co/mys/laya-typed-decisions-GGUF/blob/main/laya_typed_decisions_ud_q4_k_m.gguf) |

Total download: 404.0 MiB (423581888 bytes).

### q8

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `laya_typed_decisions_q8_0.gguf` | model | 434.1 MiB (455179648 bytes) | `6eb6ef58bc4f99bd9db40381604108af782ffc314aa00da6641f429991ca1f18` | [mys/laya-typed-decisions-GGUF](https://huggingface.co/mys/laya-typed-decisions-GGUF/blob/main/laya_typed_decisions_q8_0.gguf) |

Total download: 434.1 MiB (455179648 bytes).

## Notes

- Text only; English only; up to 16 options per question.
- The ggmlc-compiled graph has a 1024-token context budget; context and option limits cannot be changed at runtime.
- This is a specialist for four synthetic workflows, not a general zero-shot upgrade. Behavior elsewhere can be worse than the base model.
- Upstream reports overconfidence and temperatures fitted on training data; treat confidence as uncalibrated until validated on held-out domain data. Temperature fitting is not exposed by this adapter.
- Download size is not the total runtime memory requirement.
- Origin links use `main` because these registry entries pin hashes, not repository revisions.
