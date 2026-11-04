# Brand Package

The product name for every string a client's people or agents read: guardrail messages, the capabilities brief, MCP tool descriptions, egress denial messages and sandbox image descriptions. See [White-labelling theme](../../../../docs/adr/2026-10-12-white-labelling-theme.md), Decision 5.

## Configuration

The package loads the `internal/platform/brand` config module: `config/base.yaml` (default `productName: kora`), the `{APP_ENV}.yaml` overlay if one exists, and the file the chart renders at `/etc/kora/config/internal/platform/brand.yaml`, decoded strictly. The chart extracts the name from the theme document, so the backend and the frontend render the same name.

Session-manager pods are created by kora-agents, not rendered by the chart, so they carry no rendered file. kora-agents sets `KORA_PRODUCT_NAME` on each pod it builds, and `Load` reads it when set. That read lives in this package only.

`Load` rejects an empty name and a name with control characters. Every other bound on the name is the theme contract's and is checked by the frontend's init container, not here.

## Usage

```go
b, err := brand.Load()           // at service startup
...
proxy := egressgw.NewProxyHandler(broker, transport, b, logger)
```

A component declares the one-method interface it calls, beside itself and unexported, and `brand.Brand` satisfies it:

```go
type productNamer interface{ ProductName() string }
```

Tests construct a brand with `brand.MustNew("Acme")` and assert the name appears, which proves the injection rather than the default.

## Lint

The `noProductNameLiteral` ruleguard rule (`rules/kora_rules.go`) flags a new `kora` or `orbit` prose literal in the speaking packages, so a string that should read the brand cannot be hardcoded again. Identifier-shaped literals (`orbit.io`, `kora-base`) pass.
