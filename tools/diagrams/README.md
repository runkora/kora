# Architecture diagram pipeline

Excalidraw is the source of truth for the architecture diagrams. Each
`docs/diagrams/*.excalidraw` renders to a sibling `*.svg` that is
committed and embedded in the docs.

## Editing

Open the `.excalidraw` file at [excalidraw.com](https://excalidraw.com) (File →
Open), edit, save back over the same file, then regenerate:

```bash
make diagrams
```

## How it works

- `make diagrams` (`export.mjs`) bundles Excalidraw's `exportToSvg` and renders
  each source in headless Chromium, stamping the source sha256 into the SVG.
- `make check` runs `check.mjs`: a pure sha compare that fails when an SVG is
  missing or stale. No browser is needed for the check, only for regeneration.

## Setup (once)

```bash
cd tools/diagrams && npm install && npm exec playwright install chromium
```
