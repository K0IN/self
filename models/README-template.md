# <Lab>/<Model Name> <Parameter Count>

- **License:** [<License name>](<Original license URL>)
- **Release date:** <YYYY-MM-DD>
- **Modalities:** Input: <text, image, audio, ...>; Output: <text, image, audio, ...>
- **Original model:** [<Lab>/<Repository>](https://huggingface.co/<Lab>/<Repository>)

## Description

<Short description paraphrased from the original model card. Describe what the
model does, not the quantization repository or our engine.>

## Run

```bash
self serve <model-id>:<parameter-count>
```

Here is a link to the API docs: [<Modality> API](/api/<modality>).

<Choose the API matching the model's served task: decision -> /api/decision,
text -> /api/text, embedding -> /api/embeddings, image -> /api/images,
audio generation -> /api/audio, speech recognition -> /api/stt.
Include multiple links if the model serves multiple API tasks.>

### Example Request

With the server running, open another terminal and send a request:

```bash
curl http://localhost:8080/<modality-endpoint> \
	-H 'Content-Type: application/json' \
	-d '<minimal valid JSON request for this model>'
```

<Replace the placeholders with a working, model-specific example from the
linked API docs. Use multipart form fields instead of JSON when the endpoint
requires them. Explain how to save the output if it contains image or audio
data rather than readable JSON.>

## Engine Parameters

<Only include settings supported by this model's adapter. Omit this section
when there are no configurable engine settings. Distinguish startup settings
from per-request API fields.>

| Parameter | Default | What it controls |
| --- | --- | --- |
| `<setting>` | `<effective registry default>` | <Description and supported values or range> |

Override startup settings with `--set <parameter>=<value>`.

## Quantizations and Files

Default variant: `<default-quant>`.

<Repeat the subsection for every registered variant, including non-GGUF
formats. Include every required file: weights, encoders, projectors, VAEs,
and reference voices. Sizes and full SHA-256 hashes come from the registry.
Use each file's own repository and pinned revision for its origin link.>

### <quant> <(default), if applicable>

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `<filename>` | <model / text-encoder / mmproj / vae / voice> | <GiB or MiB> (<exact bytes> bytes) | `<full 64-character SHA-256>` | [<owner>/<repo>](https://huggingface.co/<owner>/<repo>/blob/<revision>/<filename>) |

Total download: <sum of required file sizes>.

## Notes

<Optional: essential model-specific limitations, license restrictions, or
usage requirements. Omit this section if there are none.>