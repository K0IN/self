# Examples

These examples are kept with the documentation so they are published with the site and remain easy to browse.

## Local settings

Copy [settings.yml](examples/settings.yml) to `~/.ai-server/settings.yml` and adjust the values for your machine:

```bash
mkdir -p ~/.ai-server
cp docs/examples/settings.yml ~/.ai-server/settings.yml
self settings kev:4b
```

The settings file is optional. It layers machine-specific engine values over the registry. See [Local settings](/self/settings) for the complete precedence order and Docker mount instructions.

## Direct API examples

The full request and response examples are in the [API reference](/self/api), including curl, Python, JavaScript, text decisions, and vision requests.

A minimal request looks like this:

```bash
curl http://localhost:8080/v1/systemone \
  -H 'Content-Type: application/json' \
  -d '{
    "state": "I was charged twice.",
    "questions": {
      "department": {
        "type": "choice",
        "instructions": "Which department should handle this?",
        "criteria": {
          "billing": "Payments and refunds",
          "technical": "Technical problems"
        }
      }
    }
  }'
```
