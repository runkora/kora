# Trigger schemas

Webhook triggers take their event types and fields from a per-provider schema embedded in `backend/internal/triggers/schemas/`. See the [trigger payload templating ADR](../../docs/adr/2026-09-28-trigger-payload-templating.md) for the format.

## GitHub

`github.mjs` converts the webhook schemas in GitHub's OpenAPI description (`github/rest-api-description`, pinned by commit in `SOURCE_COMMIT`) into `schemas/github.json`.

To pick up new GitHub events or fields, move `SOURCE_COMMIT` to a newer commit and run:

```bash
node tools/trigger-schemas/github.mjs
```

A local copy of `api.github.com.json` can be passed as the first argument instead of downloading it. The script needs Node 18 or later and has no dependencies.

## Alertmanager

`schemas/alertmanager.json` is written by hand from Alertmanager's webhook payload format.
