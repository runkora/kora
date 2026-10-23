---
name: architecture-diagrams
description: >-
  Author, render, and verify kora architecture diagrams. Excalidraw sources under
  docs/diagrams/ render to committed SVGs via tools/diagrams (make diagrams / make check);
  sequences are inline Mermaid. Covers the full loop: generate the scene, render, self-verify
  the PNG at natural size, adversarially review it against the code and the already-approved
  diagrams, then wire it into the docs. Use when asked to create, update, or review an
  architecture diagram, a system/context/topology diagram, or a docs/architecture page.
---

# Architecture diagrams

Structural diagrams are **Excalidraw sources** (`docs/diagrams/*.excalidraw`) rendered to
committed sibling **SVGs**. Sequences are **inline Mermaid** in the docs (GitHub renders
Mermaid natively, no pipeline). The pipeline in `tools/diagrams/` already exists. Do not
rebuild it.

## The loop

1. **Survey against code first.** Every element and arrow must trace to a code-verified
   fact. ADRs and PRDs show how the architecture evolved; the code is the source of truth.
   `docs/architecture.md` is stale, not an input. State each claim, then confirm it in the
   code before drawing it.
2. **Generate the scene.** Copy `assets/scene-template.mjs` into your scratchpad, rename,
   and build the diagram with the helpers. Author the source with a generator script during
   drafting; it makes iteration fast.
3. **Render.** Write the source to `docs/diagrams/<name>.excalidraw`, then `make diagrams`.
   That stamps the source sha256 into the SVG.
4. **Self-verify at natural size.** `node tools/diagrams/preview.mjs docs/diagrams/<name>.excalidraw <scratch>/<name>.png`,
   then read the PNG. Check for overlapping labels, arrows crossing chips, text overflow,
   cramped seams. Iterate on the generator and re-render until it is clean **before** showing
   the user. Do not use `qlmanage`; it distorts wide SVGs.
5. **Adversarial review.** Critique the diagram on two axes, ranked, most serious first:
   - **Accuracy.** Does every arrow and label match the code? Do not overstate. Verify, do
     not assert.
   - **Consistency with approved diagrams.** Naming, palette, grouping, and terminology must
     match `system-context`, `agent-pod-anatomy`, and the sequence pages, so a new diagram
     reads as a zoom of the set, not a separate story.
6. **Show the user from the repo path.** Send `docs/diagrams/<name>.svg` (a repo file).
   Files under `/tmp` do not render in chat.
7. **Wire it in.** Embed the SVG in the relevant `docs/architecture/<topic>.md` page and
   link that page from the root `ARCHITECTURE.md`.
8. **Stage, do not commit.** Leave the changes staged for review. Commit only when asked.

## Canonical source rule

Once a `.excalidraw` is committed or hand-edited by a human, it is canonical. **Never
regenerate it from a generator script** (that clobbers hand edits). Edit the `.excalidraw`
in place, or in Excalidraw (open at excalidraw.com, save over the file), then `make diagrams`
re-exports only the SVG. Generators are for the first draft.

## Conventions

- **Title and labels only.** No subtitles, taglines, value claims, or "detail in X"
  pointers. The structure speaks for itself. See memory `orbit-diagrams-no-commentary`.
- **No middot (`·`).** Use commas or colons.
- **Palette** (in the template): blue = control plane / kora-system, green = data plane /
  agent, grey = external / substrate, `EDGE` grey = flow arrows. `roughness: 0` and
  `fontFamily: 2` for the clean look.
- **Prose** on the docs pages follows the voice rules: lead with the conclusion, no
  rhetorical-question openers, no "This <noun>" back-references, no stapled summary clauses,
  no em dashes. See memories `orbit-no-this-x-backreference`, `orbit-no-em-dashes-in-product-copy`.
- **Flow labels** need room. Keep gaps between zones wide enough that labels do not overlap
  borders; prefer short labels ("provisions runs", "agent egress").

## Commands

```bash
make diagrams                                   # render every source -> stamped SVG
make check                                       # sha-compares sources vs SVGs (no browser)
node tools/diagrams/preview.mjs <src|svg> <png>  # PNG at natural size, for self-review
```

Setup once: `cd tools/diagrams && npm install && npm exec playwright install chromium`.

## Files

- `assets/scene-template.mjs` — generator skeleton: palette, primitives (`rect`, `ctext`,
  `ltext`, `arrow`), composites (`chip`, `box`, `zone`). Copy to scratch to start.
- `tools/diagrams/README.md` — pipeline reference (editing, how it works, setup).
