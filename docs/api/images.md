---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Image API

Image models generate new images from text prompts and edit one or more
reference images. The endpoints use the OpenAI image API shapes: generation
requests are JSON, while edit requests use `multipart/form-data`.

Health, model metadata and the error format are shared by all modalities; see
the [API overview](/api/).

</div>
<div class="api-example">

<div class="api-label">Endpoints</div>

```text
POST /v1/images/generations
POST /v1/images/edits
```

<div class="api-label">Models and quick start</div>

[Browse image models](/registry/?type=image)

```bash
self serve qwen-image-2.1:7b
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Hints

Image responses contain base64-encoded bytes in `data[].b64_json`. Use `jq` to
select a result, then pipe it to `base64 --decode`. The output filename should
use the same extension as `output_format`.

</div>
<div class="api-example">

<div class="api-label">Generate and save with curl</div>

```bash
curl http://localhost:8080/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{
    "prompt": "A red fox in a snowy forest",
    "size": "1024x1024",
    "n": 1,
    "output_format": "png"
  }' \
  | jq -r '.data[0].b64_json' \
  | base64 --decode > output.png
```

<div class="api-label">Save the JSON response first</div>

```bash
curl http://localhost:8080/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"A red fox in a snowy forest","output_format":"jpeg"}' \
  -o response.json

jq -r '.data[0].b64_json' response.json | base64 --decode > output.jpeg
```

<div class="api-label">Decode multiple results</div>

```bash
jq -r '.data[].b64_json' response.json \
  | awk '{print > ("image-" NR ".b64")}'
for file in image-*.b64; do
  base64 --decode "$file" > "${file%.b64}.jpeg"
done
rm image-*.b64
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/images/generations`

Generates one or more images from a text prompt. Requests must use
`Content-Type: application/json`, and unknown JSON fields are rejected. The
server loads one image model at startup; `model` is accepted for client
compatibility and is not used to select a different model.

#### Request body

<ApiField name="model" type="string" optional>

Accepted for client compatibility and ignored. The model is selected by the
`self serve` command.

</ApiField>

<ApiField name="prompt" type="string" required>

Text describing the image to generate. It must not be empty and may contain at
most 4,096 characters.

</ApiField>

<ApiField name="negative_prompt" type="string" optional>

Text describing content or visual properties to avoid.

</ApiField>

<ApiField name="n" type="integer" optional>

Number of images to generate. Defaults to `1`; values from `1` through `8` are
accepted.

</ApiField>

<ApiField name="size" type="string" optional>

Image dimensions in `WIDTHxHEIGHT` form. Each dimension must be from `64`
through `4096`, and the total area must not exceed 16 megapixels. Defaults to
`1024x1024`.

</ApiField>

<ApiField name="output_format" type="string" optional>

Encoded output format: `png`, `jpeg`, or `webp`. Defaults to `png`.

</ApiField>

<ApiField name="output_compression" type="integer" optional>

Output compression setting passed to the image backend. The effective range
depends on the selected output format and backend.

</ApiField>

<ApiField name="seed" type="integer" optional>

Seed passed to the image backend when supported. Reusing a seed can make
results more reproducible, but does not guarantee identical output across
different backends or settings.

</ApiField>

<ApiField name="sd_cpp_extra_args" type="object" optional>

Backend-specific stable-diffusion.cpp options. The supported keys depend on
the loaded image model. For example, sampling options can be supplied under
`sample_params`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen-image-2.1:7b",
    "prompt": "A red fox in a snowy forest",
    "negative_prompt": "blurry, low quality",
    "n": 1,
    "size": "1024x1024",
    "output_format": "png",
    "seed": 42
  }'
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/images/generations",
    json={
        "model": "qwen-image-2.1:7b",
        "prompt": "A red fox in a snowy forest",
        "negative_prompt": "blurry, low quality",
        "n": 1,
        "size": "1024x1024",
        "output_format": "png",
        "seed": 42,
    },
    timeout=900,
)
response.raise_for_status()
print(response.json())
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/images/generations', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    model: 'qwen-image-2.1:7b',
    prompt: 'A red fox in a snowy forest',
    negative_prompt: 'blurry, low quality',
    n: 1,
    size: '1024x1024',
    output_format: 'png',
    seed: 42
  })
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

<div class="api-label">Example response</div>

```json
{
  "created": 1760000000,
  "output_format": "png",
  "data": [
    {"b64_json": "iVBORw0KGgoAAAANSUhEUg..."}
  ]
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Generation response

<ApiField name="created" type="integer">

Unix timestamp in seconds for the response.

</ApiField>

<ApiField name="output_format" type="string">

Format of the encoded images in `data`.

</ApiField>

<ApiField name="data" type="array">

One object for each generated image, in request order.

</ApiField>

<ApiField name="data[].b64_json" type="string">

Base64-encoded image bytes. Decode this value using the response
`output_format`; the API does not return hosted image URLs.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Response shape</div>

```json
{
  "created": 1760000000,
  "output_format": "jpeg",
  "data": [
    {"b64_json": "..."},
    {"b64_json": "..."}
  ]
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/images/edits`

Edits one or more reference images according to a text prompt. Requests must
use `multipart/form-data`. Reference images may be uploaded files or supplied
as HTTPS URLs or image data URIs in form fields.

#### Multipart fields

<ApiField name="prompt" type="string" required>

Text describing the requested edit. It must not be empty and may contain at
most 4,096 characters.

</ApiField>

<ApiField name="image[]" type="file[]" required>

One or more JPEG, PNG, or WebP reference images. Repeat the field for multiple
images; at most eight reference images are accepted. `image` and `file` are
also accepted as single-image compatibility aliases.

</ApiField>

<ApiField name="image_url[]" type="string" optional>

HTTPS image URLs or JPEG, PNG, and WebP data URIs. Repeat the field for
multiple references; `image_url` is also accepted as a single-image alias.

</ApiField>

<ApiField name="mask" type="file" optional>

JPEG, PNG, or WebP mask image used by the backend to guide the edit.

</ApiField>

<ApiField name="n" type="integer" optional>

Number of edited images to generate. Defaults to `1`; values from `1` through
`8` are accepted.

</ApiField>

<ApiField name="size" type="string" optional>

Output dimensions in `WIDTHxHEIGHT` form. Each dimension must be from `64`
through `4096`, and the total area must not exceed 16 megapixels. When omitted,
the backend uses its default dimensions.

</ApiField>

<ApiField name="output_format" type="string" optional>

Encoded output format: `png` or `jpeg`. Defaults to `png`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/images/edits \
  -F 'prompt=Turn the reference into a watercolor illustration' \
  -F 'image[]=@reference.png' \
  -F 'n=1' \
  -F 'size=1024x1024' \
  -F 'output_format=png'
```

```python [Python]
import requests

with open("reference.png", "rb") as image_file:
    response = requests.post(
        "http://localhost:8080/v1/images/edits",
        data={
            "prompt": "Turn the reference into a watercolor illustration",
            "n": "1",
            "size": "1024x1024",
            "output_format": "png",
        },
        files={"image[]": ("reference.png", image_file, "image/png")},
        timeout=900,
    )
response.raise_for_status()
print(response.json())
```

```js [JavaScript]
const form = new FormData()
form.append('prompt', 'Turn the reference into a watercolor illustration')
form.append('image[]', new Blob([await Bun.file('reference.png').arrayBuffer()], {
  type: 'image/png'
}), 'reference.png')
form.append('n', '1')
form.append('size', '1024x1024')
form.append('output_format', 'png')

const response = await fetch('http://localhost:8080/v1/images/edits', {
  method: 'POST',
  body: form
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

<div class="api-label">Example response</div>

```json
{
  "created": 1760000000,
  "output_format": "png",
  "data": [
    {"b64_json": "iVBORw0KGgoAAAANSUhEUg..."}
  ]
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Image inputs and limits

Uploaded images and fetched URL/data-URI images are decoded and normalized in
memory before inference. Supported formats are JPEG, PNG, and WebP. HTTPS URLs
are allowed; plain HTTP and private or loopback addresses require the matching
server options.

The request body is limited to 32 MiB. Reference input is limited to eight
images, each image is limited to 20 MiB compressed and 40 megapixels decoded,
and no dimension may exceed 12,000 pixels. Generated dimensions are limited to
16 megapixels and 4,096 pixels per side.

</div>
<div class="api-example">

<div class="api-label">URL reference example</div>

```bash
curl http://localhost:8080/v1/images/edits \
  -F 'prompt=Apply the reference style to the scene' \
  -F 'image_url[]=https://example.org/reference.png'
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Image errors

Besides the [shared errors](/api/#errors), image requests return errors for
invalid formats, dimensions, missing references, oversized inputs, failed URL
fetches, unsupported model capabilities, and a full request queue. The error
response always uses the shared `error.type` and `error.message` fields.

</div>
<div class="api-example">

<div class="api-label">Error response</div>

```json
{
  "error": {
    "type": "invalid_request",
    "message": "size must use WIDTHxHEIGHT"
  }
}
```

</div>
</div>
