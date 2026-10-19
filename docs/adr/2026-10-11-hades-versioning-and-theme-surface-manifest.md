# Hades Versioning and Theme Surface Manifest

**Status:** Accepted, amended 2026-10-19
**Date:** 2026-10-11
**Deciders:** Qj Stout-Spykers
**Technical Story:** The versioning story in the [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it), required by the [White-labelling PRD](../prd/white-labelling.md), whose theme contract is bounded by the kora release.

> **Amendment, 2026-10-15.** The first version of this ADR gave Hades its own releases: a `hades/vX.Y.Z` tag, a package Semantic Version, and a separate `MAJOR.MINOR` contract version, each bumped in the pull request that made the change. That is withdrawn. Hades is versioned and released with kora: one version, the kora release tag, applies to the images, the charts and the `@runkora/hades` package. Nothing checks that a release's version reflects its Hades changes: there is no change classifier and no contract boundary rules, and the version is chosen by whoever cuts the release. The manifest and the asset formats are unchanged. The delivery ADR, the white-labelling theme ADR and both PRDs are amended to match. The file name is kept, as the PRDs cite it.


> **Amendment, 2026-10-19.** A theme references its assets by `https://` URL instead of carrying them (see the [white-labelling theme ADR](2026-10-12-white-labelling-theme.md)), so the validator cannot read their bytes. A slot's `maxBytes` is removed from the manifest, `formats` becomes the extensions a slot's URL path may end in, and the SVG content constraints no longer apply: nothing is scanned. Nothing else in SD-3 changes.
---

## Context

Hades is the kora design system: an isolated workspace package at `frontend/src/packages/hades/`, consumed by the frontend through the pnpm workspace protocol and compiled from source by Next.js. It has no releases of its own, and nothing outside the frontend depended on it until white-labelling.

White-labelling changes that. A theme is a deployment artefact that sets Hades values: colour tokens per mode, radius steps, font slots, identity slots and the product name. A theme is written against a kora release and has to be told, mechanically, whether a later release still accepts it. The first version of this ADR solved that with a second version number owned by Hades. In practice that meant two numbers to keep right in every Hades pull request, a release process of its own, and a team that releases kora frequently having to coordinate two release trains. The decision here removes the second train. Hades and kora release together, under the kora version.

### Background

- kora releases are cut from `v*` tags. The release workflow derives the image tag and chart version from the tag, builds images, and writes a `compatibility.yaml` listing the Kubernetes, Istio, SPIRE, PostgreSQL and tooling versions the release was tested with. A release has release candidates, `vX.Y.Z-rc.N`, before the final `vX.Y.Z`. At the time of this amendment kora is `0.x`: `v0.7.0` is the last final release and `v0.8.0` is in release candidates.
- The frontend image receives `ORBIT_VERSION` as a build argument and surfaces it as `NEXT_PUBLIC_ORBIT_VERSION`, which the dashboard header shows.
- `make check-versions` already enforces parity between a pinned version in `versions.hcl` and the Go directive in `go.mod`.
- The token file `tokens.css` defines the raw palette in a `:root` block, overrides it in a `[data-theme='light']` block, and maps semantic names in a Tailwind `@theme inline` block. The radius scale is five raw tokens, `--hades-raw-radius-sm` to `--hades-raw-radius-2xl`. The raw palette also holds six shadow tokens. There is no raw font token: the font families in `@theme inline` fall back through `--font-inter` and `--font-geist-mono`, names the kora app chose and sets through `next/font`.
- Identity is rendered today by one kora drawing in Hades, `KoraLogo`, the mark and wordmark as a single SVG, in the login card and the sidebar header; a static `icon.svg` under the app directory; and a title string in the root layout. `KoraMark`, the mark alone, is exported but nothing renders it.

### Problem Statement

Decide how Hades is versioned and released when it is released with kora; how a theme says which release it targets and how an instance decides whether to accept it; and what the manifest is that declares the surface a theme can set.

The Hades PRD numbers these as subsections 1 to 3 of this ADR, and the white-labelling PRD cites subsection 2 for version exposure and subsection 3 for the manifest. The sub-decisions below keep that numbering.

### Goals

- One version, the kora version, for everything a release ships. No version of Hades is written by hand.
- A running instance can read the version it is, and a theme records the release it was written against.
- The surface a theme can set is a checked-in manifest, generated from the token file and the slot definitions, so a theme validator and a reviewer both read it from one place.

### Non-Goals

- Delivering Hades to consumers outside this repository: the mechanism is the [Hades delivery ADR](2026-10-11-hades-delivery-to-external-consumers.md).
- The theme contract itself: schema, validation rules and packaging. That is subsection 1 of the white-labelling theme ADR, which depends on the manifest defined here.
- Which releases continue to receive fixes. The PRD leaves this until it matters.
- Versioning anything other than Hades. kora's own release versioning is unchanged.
- Checking that a release's version reflects its Hades changes. The version is chosen by whoever cuts the release.
- Any promise that a theme written for one major keeps working on the next. There is none.

---

## Decision Drivers

* Hades is released with kora, frequently, so a Hades release process separate from kora's is cost without a reader.
* A theme author needs one fact: the kora release the theme was written against.
* The surface a theme can set must be a fact that is diffable in a pull request and deliverable with a release.
* No version may be written by hand and then drift. The tag is the only place a version is written.
* The repo already has patterns for version parity checks, release tagging and generated-file drift checks; reuse them.

---

## Considered Options

The options are grouped by sub-decision. Each sub-decision records the option chosen and why.

---

## Sub-Decisions

The Hades PRD refers to these as subsections 1 to 3.

### SD-1: Version source of truth and release process

**Options considered:**

1. **The kora release tag is the only version. Hades is stamped with it when the release is built.** `v1.4.0` releases the images, the charts and `@runkora/hades@1.4.0` together. Nothing in the tree carries a Hades version that has to be kept in step.
2. **Keep an independent Hades version, released on its own tags.** The first version of this ADR. Withdrawn: two release trains, two numbers in every pull request, and a contract version that existed only to let themes survive releases nobody promised they would survive.
3. **Keep an independent contract version, but release it with kora.** Still two numbers to keep right, still bumped by hand in the pull request, with none of the benefit of independent releases.

**Chosen: Option 1.**

- The version of a release is its `vX.Y.Z` tag. Release candidates, `vX.Y.Z-rc.N`, carry the same core version as the final release they lead to.
- `frontend/src/packages/hades/package.json` carries the placeholder `0.0.0` in the tree. The release workflow sets `version` to the tag's version, without the `v`, before packing, so the published package, the tarball name and the release it ships in agree by construction. There is no `src/version.ts` and no `CONTRACT_VERSION`, and `make check-versions` has nothing to say about Hades.
- The manifest carries no version, for the same reason: it is delivered inside the release, and the release is its version.
- A Hades release is therefore a kora release. There is no `hades/v*` tag, no separate workflow trigger, and no Hades-only release to cut.
- Pre-release builds, local development and any build that is not from a release tag are development builds. They have no kora version, and the theme comparison is skipped for them.
- **The surface is settled before the first contract.** Four changes land before any release carries the white-labelling contract: radius steps as literal tokens rather than multiples of one unit; agnostic identity slot components for the logo and the favicon, each taking a value per mode, with the product name as a value (this ADR decides only their Hades-side props, and the white-labelling theme ADR decides how the effective theme feeds them); `KoraLogo` moved out of Hades into the kora default theme at app level and `KoraMark`, which nothing renders, deleted; and two font slot tokens, `--hades-raw-font-sans` and `--hades-raw-font-mono`, defaulting to generic stacks, which `--font-sans` and `--font-mono` resolve through and the kora default theme sets to its own fonts. The asset formats and product-name bounds the manifest carries are decided in SD-3, so nothing outside this ADR blocks the first release that carries a contract.

### SD-2: How kora pins the version and exposes it

**Options considered:**

1. **The instance's own kora version is its Hades version; a theme records the release it targets.** The frontend already receives the version as a build argument. The theme document carries `koraVersion`, the version it was written against.
2. **Serve the Hades version and the manifest from a read-only frontend endpoint.** Gives an in-cluster validator a runtime source. Costs a public route for data that is already published with every release; decided in the white-labelling theme ADR.
3. **A separate Hades version constant checked into the package.** The first version of this ADR. Withdrawn with SD-1.

**Chosen: Option 1.**

- **Pin.** The Hades in a kora image is the Hades in the tree at the release commit. No range, no second declaration. `compatibility.yaml` is unchanged: the Hades version is the release version, so it needs no line.
- **A theme targets a release.** A theme records `koraVersion`, a full `MAJOR.MINOR.PATCH` version, as a quoted string. It is the only mandatory key. The white-labelling theme ADR decides how it is validated and gives the acceptance rule.
- **No promise across majors.** A theme is accepted by an instance of the same major and is not promised to be accepted by any other. Within a major it is accepted, because the validator checks a theme against the instance's own manifest and names the fault. Which release gets a major is decided by whoever cuts it, and nothing checks that a change that breaks themes is in one. While kora is below `1.0.0`, every release has major `0`, so nothing is rejected on version and nothing is promised either.
- **Expose.** The manifest is a file in the package, imported in-process by the frontend. The instance logs its version at startup, alongside the theme it loaded, as the white-labelling PRD requires. Every kora release carries the manifest, `theme.schema.json` and `defaults.json` as release assets (the delivery ADR), so a theme builder can be offered every release and fetch the surface of any one of them without a running instance.
- **Runtime exposure over HTTP** is decided in the white-labelling theme ADR. This ADR guarantees its input: the instance knows its own version, and the manifest is in the package.

### SD-3: The manifest

**Options considered:**

1. **A checked-in manifest generated from the token file and the slot definitions.**
2. **No manifest; the surface is whatever the token file happens to say.** Rejected: the validation rules need a declared surface, and a reviewer cannot see a surface change in a diff.
3. **Derive the manifest at build time rather than checking it in.** Rejected: the manifest has to be diffable in a PR and deliverable to consumers without a build.

**Chosen: Option 1.**

**The surface declaration.** `frontend/src/packages/hades/src/tokens/surface.ts` is the declared source of everything about the surface that `tokens.css` does not say: for every settable token its `kind` and `format`; the slot definitions; and the product-name bounds. It is hand-written and reviewed like any source file. The generator reads token names from `tokens.css` and everything else from this file, and fails if a raw token in one is missing from the other.

**The inclusion rule.** Every `--hades-raw-*` custom property declared in the `:root` block of `tokens.css` is a settable token, in both modes, by rule. Presence in the `[data-theme='light']` block is a light default value, not a statement of addressability; a token with no light override is still settable in light. That makes the surface identical to the raw palette as it exists: colours, the status, provider, strategy and chart colours, the overlay tokens and the six shadow tokens. The radius steps and the two font slot tokens from SD-1 are raw tokens too once those changes land. Semantic names in `@theme inline` are never settable: Hades derives them from raw tokens.

**The manifest.** `frontend/src/packages/hades/manifest.json`, checked in, generated by `make hades-manifest` from `tokens.css` and `surface.ts`, verified in sync by `make check` the way rendered diagrams are, and delivered with every release. It carries no version: the release it ships in is its version. It contains:

- `modes`: `["dark", "light"]`.
- `tokens`: one entry per settable token, with `name` (the custom property), `kind` (`color`, `shadow`, `length`, `font`), `format` (the accepted value syntax: hex or OKLCH for `color`, a CSS shadow list for `shadow`, a CSS length for `length`, a font family name for `font`), and `modes`, which is both modes for every token, and `group` and `label` (below).
- `groups`: a flat list of `{ id, label, order }`, one per group a token or a semantic name can belong to, in display order. Groups do not nest. `branding` is the group of brand colours. Which tokens a consumer derives from them (its "shades") is the consumer's own configuration, keyed by token name, because status, provider and chart colours are settable but not derived.
- `mapping`: for each semantic name in `@theme inline`, how it resolves. A direct `var()` reference records the raw token. An expression such as `color-mix()` or `calc()` records every raw token it references and is marked derived. A literal records no token. A theme never sets a semantic name, but the mapping is what existing components read through, so a change to it is a change to what those components read. Each entry also carries `group` and `label`.
- `contrast`: the text pairs a theme must keep, sorted by `id`: `{ id, text, on, min, severity }`. `text` and each `on` entry are non-derived colour semantic names, one `id` per text colour with `on` listing the backgrounds it sits on, and `min` is a WCAG 2.x contrast ratio between 1 and 21. `severity` is `error` (the theme fails validation) or `warn` (the validator reports it and the theme still loads). Text only: borders and other non-text pairs are not declared. A derived name such as `--color-accent-subtle` has no single colour and cannot be named. The manifest carries no colour values: the validator resolves each name through `mapping` to its raw token and takes the value from the theme or, for a token the theme leaves alone, from the app's defaults, which the caller supplies and which Hades ships as `defaults.json`, generated from `tokens.css` and outside the manifest.
- `slots`: one entry per asset slot with `name`, `kind`, `formats` and `modes`.
- `productName`: the bounds on the product name.

**Contrast pairs.** Declared next to the groups in `surface.ts` as `CONTRAST`. The generator rejects an unknown, non-colour or derived name, an empty or repeated id, an empty or repeated `on`, a text listed in its own `on`, a `min` outside 1 to 21 and a severity that is not `error` or `warn`. The initial pairs are body text, muted, subtle and accent text on the four surfaces and the accent foreground on the accent, at 4.5, and each status colour on the four surfaces, at 3. Every pair is `warn` for now, so a low-contrast theme is reported but never fails a deployment; raising a pair to `error` later is a one-line change in `surface.ts`. The validator measures with the WCAG 2.x relative-luminance ratio, ignores alpha, and checks a pair only when the theme sets at least one side of it, so a theme is held to the colours it changes and not to kora's own.

**Groups and labels.** `group` (a `groups` id) and `label` (a short display name) say how a consumer presents a token or semantic name, such as a theme preview builder that lists the manifest without keeping its own token-to-group table. They are declared next to each token and semantic name in `surface.ts`, once, and are metadata: a theme does not carry them, so they are outside the contract. Each token has exactly one primary group. The generator fails when a token or semantic name declares no group, names a group that is not declared, or when a group has no members, which is what keeps a consumer's table from going stale when a token is added. Group ids are what a consumer hard-codes; labels and order are display and may change freely.

The generator is the only thing that writes the manifest; a hand edit fails the sync check. The white-labelling validation rules are generated from it, per release. The manifest is internal: its readers are the validation rule generator and any theme producer kora specifies. Its shape is bespoke, documented alongside the package, and targets no interchange format.

**Asset formats and product-name bounds.** A slot's formats are the extensions its URL path may end in. These are manifest fields, so no release can carry the contract without them, and they are decided here. The white-labelling theme ADR's contract subsection cites them rather than deciding them again.

| Slot | Kind | Formats | Modes |
|---|---|---|---|
| `logo` | `image` | `.svg` only. | both |
| `favicon` | `image` | `.svg` only. A raster fallback is not provided until a target browser needs one. | both |
| `font.sans`, `font.mono` | `font` | `.woff2` only, one file per slot, variable or single weight. The instance declares the font face and assigns the family name the font slot token resolves to. | not per mode |

Product name: one string, 1 to 40 characters after trimming surrounding whitespace, consisting of Unicode letters, digits, spaces, and the punctuation `.`, `-`, `&` and `'`; no control characters; none of `<`, `>` or `"`. Single verbatim string, as the white-labelling PRD requires.

---

## Decision

**Chosen Option:** SD-1 Option 1, SD-2 Option 1, SD-3 Option 1.

Hades is versioned and released with kora under the kora tag, stamped into the package when the release is built, with no version of its own in the tree. A theme records the kora release it was written against, and an instance accepts a theme of the same major. The surface a theme can set is a checked-in manifest generated from the token file and the slot definitions. Nothing checks that a release's version reflects its Hades changes.

### Rationale

The manifest is the hinge. Once the surface a theme can set is a checked-in JSON file, a surface change is a diff in a pull request, the white-labelling validator has something to generate rules from, and the delivery ADR has something to ship.

One version is chosen because kora releases frequently and Hades is not consumed independently of it: the only consumers of a Hades version are kora instances, theme authors targeting an instance, and package consumers who follow kora releases. Two numbers served an independence that nobody has asked for, and cost a hand-written bump, a release process and a conceptual model in every Hades pull request.

No enforcement of the version is the choice because the validator already checks a theme against the instance's own manifest, so a theme a release no longer fits fails at validation with the fault named. A guard that decides what each release must be called costs a classifier, its rules and a release gate, for a failure the validator reports anyway.

---

## Consequences

### Positive

- One version everywhere: image, chart, package, theme target and the release a theme builder selects. No second number to understand.
- No Hades-only bump in a pull request, no `version.ts`, no parity check, no second release train.
- A theme records one fact, the release it targets, and a builder can offer every release's surface from its assets.

### Negative

- Nothing checks that a release's version reflects its Hades changes, so a change that breaks themes can ship in a minor or a patch. The validator catches it per theme, at deploy, but the same-major rule in SD-2 does not.
- A theme is only promised to work within a major. A theme author must reissue a theme on each major.
- The manifest generator parses `tokens.css` for token names and the semantic mapping, and reads kinds, formats, slots and bounds from `surface.ts`. A raw token present in one file and absent from the other fails the sync check, so the two cannot drift, but every new token is declared twice.
- The four changes before the first contract, in particular moving `KoraLogo` out of Hades, are real frontend work that this ADR schedules ahead of any contract.
- The slots are only the identity the product renders today, the logo and the favicon. A mark-only or wordmark-only slot is added when a surface needs one.

### Neutral

- There are no `hades/` tags. kora's `v*` tags, including release candidates, are the only tags.
- `compatibility.yaml` does not change.

---

## Implementation

### Package changes

- `frontend/src/packages/hades/package.json`: `version` is `0.0.0` in the tree and is set from the release tag by the release workflow.
- `frontend/src/packages/hades/src/tokens/surface.ts`: the declared kinds, formats, slots and product-name bounds.
- `frontend/src/packages/hades/manifest.json`: generated by `make hades-manifest` from `tokens.css` and `surface.ts`, without a version; `make check` fails on drift.
- `frontend/src/packages/hades/scripts/manifest.ts`: the generator.
- No `version.ts`; `HADES_VERSION` and `CONTRACT_VERSION` are not exported.
- **Groups.** The manifest gains `groups` and a `group` and `label` on every token and mapping entry, as SD-3 specifies.
- **Contrast.** The manifest gains `contrast`. Contrast is non-blocking: a low-contrast theme is reported and still loads.

### Frontend changes

- Startup log line with the instance's version, alongside the theme log the white-labelling PRD requires.

### Repository changes

- `Makefile`: `hades-manifest`; `check` includes the manifest sync check. `check-versions` no longer covers Hades.
- `.github/workflows/frontend-ci.yml`: runs `make hades-check` in an existing job, so a stale manifest fails the pull request. Actions pinned by commit SHA, as every workflow in this repository is.
- `.github/workflows/hades-release.yml`: removed. Its jobs move into `release.yml` under the delivery ADR.

### Migration Path

The theme document key is `koraVersion` from the start; Hades has not been released, so this is not a document-format change against any baseline.

### Timeline

Before the white-labelling theme ADR's contract subsection is finalised, because that subsection generates its validation rules from the manifest shape and cites the asset formats and product-name bounds decided here.

---

## Validation

### Success Metrics

- `make check` fails when `manifest.json` is stale.
- The published package's version equals the release tag's version.
- The instance's startup log names its version.

### Monitoring

- The kora tag list is the release history.

---

## Related Decisions

- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) - the design system this versions; amended to point here
- [hades-delivery-to-external-consumers](2026-10-11-hades-delivery-to-external-consumers.md) - ships Hades in every kora release to consumers outside the frontend
- [white-labelling-theme](2026-10-12-white-labelling-theme.md) - the `koraVersion` key and the acceptance rule
- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md) - the file-drift pattern reused by the manifest sync check

---

## References

- [Hades Design System PRD, versioning story](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [White-labelling PRD](../prd/white-labelling.md)
- [Semantic Versioning 2.0.0](https://semver.org/)

---

## Notes

The PRDs cite this ADR by subsection number. SD-1 to SD-3 above are those subsections, in order. The ADR was first titled Design System Semantic Versioning and was renamed when Hades stopped having a version of its own.

**Last Updated:** 2026-10-19
