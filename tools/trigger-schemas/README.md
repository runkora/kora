# Trigger schemas

Webhook triggers take their event types and fields from a per-provider schema embedded in `backend/internal/triggers/schemas/`. See the [trigger payload templating ADR](../../docs/adr/2026-09-28-trigger-payload-templating.md) for the format.

## GitHub

`github.mjs` converts the webhook schemas in GitHub's OpenAPI description (`github/rest-api-description`) into `schemas/github.json`. The committed schema is generated from commit `0029c0d4a6d330559abe0e45b22263bbe1c6b1a7`; the repository stopped tagging releases in 2022.

To regenerate, download the description at the pinned commit and convert it:

```bash
COMMIT=0029c0d4a6d330559abe0e45b22263bbe1c6b1a7
curl -fsSL -o /tmp/api.github.com.json \
  "https://raw.githubusercontent.com/github/rest-api-description/$COMMIT/descriptions/api.github.com/api.github.com.json"
node tools/trigger-schemas/github.mjs /tmp/api.github.com.json
```

To pick up new GitHub events or fields, move the commit here and in the command. The script needs Node 18 or later and has no dependencies.

## Alertmanager

`schemas/alertmanager.json` is written by hand from Alertmanager's webhook payload format.
