# Design System Semantic Versioning

**Status:** Accepted, amended 2026-10-15
**Date:** 2026-10-11
**Deciders:** Qj Stout-Spykers
**Technical Story:** The versioning story in the [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it), required by the [White-labelling PRD](../prd/white-labelling.md), whose theme contract is bounded by the kora release.

> **Amendment, 2026-10-15.** The first version of this ADR gave Hades its own releases: a `hades/vX.Y.Z` tag, a package Semantic Version, and a separate `MAJOR.MINOR` contract version, each bumped in the pull request that made the change. That is withdrawn. Hades is versioned and released with kora: one version, the kora release tag, applies to the images, the charts and the `@runkora/hades` package. The classifier stays, because the discipline it enforces still matters, but it now answers one question at one point: does this kora release carry a Hades break, and is the release version high enough to say so. The manifest, the contract boundary rules and the asset formats are unchanged. The delivery ADR, the white-labelling theme ADR and both PRDs are amended to match. The file name is kept, as the PRDs cite it.

---

## Context

Hades is the kora design system: an isolated workspace package at `frontend/src/packages/hades/`, consumed by the frontend through the pnpm workspace protocol and compiled from source by Next.js. It has no releases of its own, and nothing outside the frontend depended on it until white-labelling.

White-labelling changes that. A theme is a deployment artefact that sets Hades values: colour tokens per mode, radius steps, font slots, identity slots and the product name. A theme is written against a kora release and has to be told, mechanically, whether a later release still accepts it. The first version of this ADR solved that with a second version number owned by Hades. In practice that meant two numbers to keep right in every Hades pull request, a release process of its own, and a team that releases kora frequently having to coordinate two release trains. The decision here removes the second train. Hades and kora release together, under the kora version, and a break in Hades is a break in that release.

### Background

- kora releases are cut from `v*` tags. The release workflow derives the image tag and chart version from the tag, builds images, and writes a `compatibility.yaml` listing the Kubernetes, Istio, SPIRE, PostgreSQL and tooling versions the release was tested with. A release has release candidates, `vX.Y.Z-rc.N`, before the final `vX.Y.Z`. At the time of this amendment kora is `0.x`: `v0.7.0` is the last final release and `v0.8.0` is in release candidates.
- The frontend image receives `ORBIT_VERSION` as a build argument and surfaces it as `NEXT_PUBLIC_ORBIT_VERSION`, which the dashboard header shows.
- `make check-versions` already enforces parity between a pinned version in `versions.hcl` and the Go directive in `go.mod`.
- The token file `tokens.css` defines the raw palette in a `:root` block, overrides it in a `[data-theme='light']` block, and maps semantic names in a Tailwind `@theme inline` block. The radius steps are multiples of one `--hades-radius-unit`. The raw palette also holds six shadow tokens. There is no raw font token: the font families in `@theme inline` fall back through `--font-inter` and `--font-geist-mono`, names the kora app chose and sets through `next/font`.
- Identity is rendered today by one kora drawing in Hades, `KoraLogo`, the mark and wordmark as a single SVG, in the login card and the sidebar header; a static `icon.svg` under the app directory; and a title string in the root layout. `KoraMark`, the mark alone, is exported but nothing renders it.

### Problem Statement

Decide how Hades is versioned and released when it is released with kora; how a theme says which release it targets and how an instance decides whether to accept it; how a change to Hades is classified against Semantic Versioning and the contract boundary rules; and where the classifier runs, what it blocks, and how it explains itself.

The Hades PRD numbers these as subsections 1 to 4 of this ADR, and the white-labelling PRD cites subsection 2 for version exposure and subsection 3 for the manifest. The sub-decisions below keep that numbering.

### Goals

- One version, the kora version, for everything a release ships. No version of Hades is written by hand.
- A running instance can read the version it is, and a theme records the release it was written against.
- Classification is decided on the manifest against the numbered contract boundary rules, is deterministic where it can be, and cites the rule it applied.
- A kora release that contains a Hades break cannot be cut under a version that does not say so.
- When the classifier blocks, its message tells a developer or an agent what was found, why it blocks, and exactly how to resolve it.

### Non-Goals

- Delivering Hades to consumers outside this repository: the mechanism is the [Hades delivery ADR](2026-10-11-hades-delivery-to-external-consumers.md).
- The theme contract itself: schema, validation rules and packaging. That is subsection 1 of the white-labelling theme ADR, which depends on the manifest defined here.
- Which releases continue to receive fixes. The PRD leaves this until it matters.
- Versioning anything other than Hades. kora's own release versioning is unchanged, except that a Hades break now constrains it.
- Any promise that a theme written for one major keeps working on the next. There is none.

---

## Decision Drivers

* Hades is released with kora, frequently, so a Hades release process separate from kora's is cost without a reader.
* A theme author needs one fact: the kora release the theme was written against.
* The package follows Semantic Versioning, so a consumer of the published package can trust a caret range. That is only true if a break is a major, so the classifier must hold the line at release time.
* Enforcement must not be bypassable at the point that ships, because a misclassified change silently breaks every deployed theme and every consumer of the package.
* No version may be written by hand and then drift. The tag is the only place a version is written.
* The repo already has patterns for version parity checks, release tagging and generated-file drift checks; reuse them.

---

## Considered Options

The options are grouped by sub-decision. Each sub-decision records the option chosen and why.

---

## Sub-Decisions

The Hades PRD refers to these as subsections 1 to 4.

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
- **No promise across majors.** A theme is accepted by an instance of the same major and is not promised to be accepted by any other. Within a major it is accepted, because a Hades break is a major (SD-3). While kora is below `1.0.0`, every release has major `0`, so nothing is rejected on version and nothing is promised either: the classifier runs and reports, and a break is not yet blocked (SD-4).
- **Expose.** The manifest is a file in the package, imported in-process by the frontend. The instance logs its version at startup, alongside the theme it loaded, as the white-labelling PRD requires. Every kora release carries the manifest, `theme.schema.json` and `CONTRACT.md` as release assets (the delivery ADR), so a theme builder can be offered every release and fetch the surface of any one of them without a running instance.
- **Runtime exposure over HTTP** is decided in the white-labelling theme ADR. This ADR guarantees its input: the instance knows its own version, and the manifest is in the package.

### SD-3: Classification rules and the manifest

**Options considered:**

1. **A checked-in manifest generated from the token file and the slot definitions, with classification as a deterministic diff against the last final release plus one code check.**
2. **Classify by reviewing the diff with no manifest.** Rejected: the status quo for every design system that has ever shipped a silent breaking change, and the validation rules need the manifest anyway.
3. **Derive the manifest at build time rather than checking it in.** Rejected: the manifest has to be diffable in a PR, deliverable to consumers, and readable by the classifier without a build.

**Chosen: Option 1.**

**The surface declaration.** `frontend/src/packages/hades/src/tokens/surface.ts` is the declared source of everything about the surface that `tokens.css` does not say: for every settable token its `kind` and `format`; the slot definitions; and the product-name bounds. It is hand-written and reviewed like any source file. The generator reads token names from `tokens.css` and everything else from this file, and fails if a raw token in one is missing from the other.

**The inclusion rule.** Every `--hades-raw-*` custom property declared in the `:root` block of `tokens.css` is a settable token, in both modes, by rule. Presence in the `[data-theme='light']` block is a light default value, not a statement of addressability; a token with no light override is still settable in light. That makes the surface identical to the raw palette as it exists: colours, the status, provider, strategy and chart colours, the overlay tokens and the six shadow tokens. The radius steps and the two font slot tokens from SD-1 are raw tokens too once those changes land. Semantic names in `@theme inline` are never settable: Hades derives them from raw tokens.

**The manifest.** `frontend/src/packages/hades/manifest.json`, checked in, generated by `make hades-manifest` from `tokens.css` and `surface.ts`, verified in sync by `make check` the way rendered diagrams are, and delivered with every release. It carries no version: the release it ships in is its version. It contains:

- `modes`: `["dark", "light"]`.
- `tokens`: one entry per settable token, with `name` (the custom property), `kind` (`color`, `shadow`, `length`, `font`), `format` (the accepted value syntax: hex or OKLCH for `color`, a CSS shadow list for `shadow`, a CSS length for `length`, a font family name for `font`), and `modes`, which is both modes for every token.
- `mapping`: for each semantic name in `@theme inline`, how it resolves. A direct `var()` reference records the raw token. An expression such as `color-mix()` or `calc()` records every raw token it references and is marked derived. A literal records no token. A theme never sets a semantic name, but the mapping is what existing components read through, so a change to it is a change to what those components read.
- `slots`: one entry per asset slot with `name`, `kind`, `formats`, `maxBytes` and `modes`.
- `productName`: the bounds on the product name.

The generator is the only thing that writes the manifest; a hand edit fails the sync check. The white-labelling validation rules are generated from it, per release. The manifest is internal: its readers are the classifier, the validation rule generator, and any theme producer kora specifies. Its shape is bespoke, documented alongside the package, and targets no interchange format.

**Asset formats and product-name bounds.** These are manifest fields, so no release can carry the contract without them, and they are decided here. The white-labelling theme ADR's contract subsection cites them rather than deciding them again.

| Slot | Kind | Formats | Maximum size | Modes |
|---|---|---|---|---|
| `logo` | `image` | SVG only. No scripts, no event attributes, no external references; the validator rejects any of them. | 256 KiB | both |
| `favicon` | `image` | SVG only. A raster fallback is not provided until a target browser needs one. | 64 KiB | both |
| `font.sans`, `font.mono` | `font` | WOFF2 only, one file per slot, variable or single weight. The instance declares the font face and assigns the family name the font slot token resolves to. | 2 MiB each | not per mode |

Product name: one string, 1 to 40 characters after trimming surrounding whitespace, consisting of Unicode letters, digits, spaces, and the punctuation `.`, `-`, `&` and `'`; no control characters; none of `<`, `>` or `"`. Single verbatim string, as the white-labelling PRD requires.

**The contract boundary rules.** The five numbered rules from the Hades PRD, reproduced here because classification cites them by number:

1. Hades defines the shape of every value a theme can set. That shape is the theme surface manifest: colour and shadow tokens per mode, radius steps, font slots, identity slots (the logo and the favicon, each per mode) and the product name with its bounds.
2. Who reads a value is not the contract's concern. The kora frontend and the kora backend both read the product name, and neither reading is part of the contract.
3. Hades contains no kora identity. The kora default is a theme like any other, owned by the app. The kora logo is an asset of that theme, not a Hades component. Hades provides the agnostic components that render whatever a theme supplies for a slot.
4. A kora change that adds a place where a value appears, or changes a page or a template, does not change the contract. A kora change that needs a new value changes the manifest, which is a Hades change and is classified as one.
5. kora default values are outside the contract. A change to a default value alone is a patch.

The same list is `frontend/src/packages/hades/CONTRACT.md`, delivered with the package. `make check` diffs that file against the list in the Hades PRD, so the three copies cannot drift.

**What a version promises.** Hades follows Semantic Versioning, and its version is the kora version. A major is a break: a theme written for an earlier release can fail validation or change meaning, or a consumer of the package can fail to compile or render. A minor is an addition: the surface or the package gained something. A patch is a fix or an internal change. A consumer of the published package may use a caret range, and a theme author may rely on the rule in SD-2. These promises begin at `1.0.0`; below it, nothing is promised.

**Classification.** Decided on the manifest, per mode, by comparing the manifest, the schema, the component API and the peer ranges at the release commit with those at the last final release, not at a pull request base and not at the previous release candidate, so a series of changes between releases cannot each look like a patch while together they are a major. Each observation yields one level.

| Observation | Level | Breaks themes | Rule cited |
|---|---|---|---|
| A token or slot present at the last release is absent, renamed, or has a different `kind`, `format` or `modes` | major | yes | boundary rule 1 |
| A mode is added or removed | major | yes | boundary rule 1 |
| `productName` bounds tighten, or a slot's formats or maximum size shrink | major | yes | boundary rule 1 |
| A top-level property of the theme document's schema, `theme.schema.json` from the white-labelling theme ADR, is added, removed or changes shape | major | yes | boundary rule 1; an unknown top-level key fails validation on every older instance |
| A token or slot is added, and a component that existed at the last release reads it, directly or through a semantic name whose mapping now resolves to it | major | yes | boundary rule 4, plus the token test in the PRD |
| A semantic name present at the last release is remapped to a different raw token | major | yes | boundary rule 4 |
| A token or slot is added, and only components new since the last release read it | minor | no | boundary rule 4 |
| `productName` bounds loosen, or a slot's formats or maximum size grow | minor | no | boundary rule 1 |
| A semantic name present at the last release is removed | major | no | Semantic Versioning: no theme sets a semantic name, but a consumer's styles may read it |
| An export removed or renamed, a prop removed, a prop made required, a peer dependency added, or a peer dependency range narrowed | major | no | Semantic Versioning |
| A semantic name added over existing tokens, an export added, an optional prop added, a peer dependency range widened, or a peer dependency removed | minor | no | Semantic Versioning |
| No manifest change and no API change, and sources changed: markup or style changes, a default value change, a change to `scripts/`, build or test configuration | patch | no | boundary rule 5 |
| Only `README.md`, `CONTRACT.md`, `src/docs/`, stories or tests changed | none | no | no rule engaged |

The result is the highest level of any observation. "Breaks themes" is reported alongside it because it is the question a theme author asks, and the answer decides whether the break is one the white-labelling contract cares about; it does not change what the release owes. A Hades break of either kind needs the major.

The manifest rows are a diff of two JSON files, and the document row a diff of a third, the top level of the generated `theme.schema.json`. The direct-read row also needs a source search: the last final release is used only to list which components existed then; for each added token, the classifier searches those components' sources at the release commit for a reference to it, so a reference introduced into an existing component is caught. The remaining rows need the list of changed files and, for the component API rows, a diff of the package's exported names, their props and whether each prop is optional, which the classifier takes from the TypeScript declarations of `src/index.ts` at both commits. A peer dependency range is classified by comparing the two ranges: a new range that accepts every version the old one did is widened, and any other is narrowed.

Two cases no script decides, both review questions: a token that keeps its name, kind, format and mapping but changes what it means, such as an accent token repurposed as a status colour; and a prop whose type narrows, or an exported function whose signature tightens, which the classifier cannot see because it reads member names and optionality, not types. Both are a major when they apply. The pull request template for Hades changes asks them, and the release report prints them.

**What a release owes.** Let the last final release be `L` and the release being cut be `R`, compared by core version, ignoring any `-rc.N`. The classification of the change from `L` to `R` sets the least `R` may be:

| Classification | `R` must be at least |
|---|---|
| major | `L` with the leading non-zero part raised: the next major from `1.0.0` on |
| minor | `L` with the minor raised, or a higher bump |
| patch or none | any version above `L` |

Before `1.0.0` there is no leading-non-zero rule to hold: the classifier computes and reports what the release owes, and enforces nothing. From the first final release at or above `1.0.0`, a release whose version is below what its classification requires is refused. Before the first final release there is no `L`, and the classifier reports "no baseline".

### SD-4: Where the classifier runs and how it reports

Every classification in SD-3 but two is a deterministic function of the manifest diff, the semantic mapping and a source search, so the classifier is a script, not an agent. The two non-mechanical cases are review questions.

**Options considered:**

1. **At release, blocking; on every pull request, advisory.** The classifier runs as the first job of the kora release workflow against the last final release and blocks the release when its version is too low. It also runs on pull requests that touch Hades and writes what the change would owe to the job summary, without failing.
2. **On every pull request, blocking.** The first version of this ADR. Withdrawn: it forced a version bump into every Hades pull request to satisfy a number nobody reads until release.
3. **Pre-commit or pre-push.** Bypassable with `--no-verify`, run on intermediate states that never ship, and a developer's clone may lack the tags the comparison needs.

**Chosen: Option 1.**

- **The classifier** is a script under `frontend/src/packages/hades/scripts/`, invoked by `make hades-classify`, which takes the baseline and the version being cut. It prints the classification, what the release owes, the versions at the baseline and at the release, and one line per finding citing the boundary rule or Semantic Versioning and the token, slot or export. It exits non-zero when the release version is below what is owed and enforcement applies.
- **At release.** The first job of `release.yml`, `hades-verify`, runs it on the tagged commit, with full history and tags, before any image is built or anything is published. The release does not proceed if it fails, and a job deletes the tag, because nothing has been built or published yet, so the version can be re-cut.
- **On pull requests.** A job in `frontend-ci`, gated on a `hades` filter on `frontend/src/packages/hades/**`, runs the same script against the last final release and the next version it would be, writes the report to the job summary, and always passes. Its purpose is to tell a developer or an agent, early, that a change is a break and that the next release will have to say so. It holds `contents: read` and posts nothing.
- **When it blocks, it says why and how to fix it.** The message is a fixed template. It states, in order: the verdict in one sentence (this release is `R`, the last final release is `L`, the change is a major, so `R` must be at least `N`); each finding, with the rule, what changed and where; the two ways out, which are to re-cut the release as `N` or higher, or to revert the breaking change and re-cut; and the commands to do either, including the exact tag to delete and the make target that reproduces the check locally. It names the two review questions and says they are not decided by the script. A reader with no context on Hades can act on it without opening the ADR.

---

## Decision

**Chosen Option:** SD-1 Option 1, SD-2 Option 1, SD-3 Option 1, SD-4 Option 1.

Hades is versioned and released with kora under the kora tag, stamped into the package when the release is built, with no version of its own in the tree. A theme records the kora release it was written against, and an instance accepts a theme of the same major. A change to Hades is classified deterministically on the manifest against the contract boundary rules, and the classifier runs at release, where it blocks a release whose version is lower than its Hades changes require from `1.0.0` on, and on pull requests, where it only informs.

### Rationale

The manifest is the hinge. Once the surface a theme can set is a checked-in JSON file, versioning stops being a judgement about intent and becomes a diff, the classifier has something mechanical to cite, the white-labelling validator has something to generate rules from, and the delivery ADR has something to ship.

One version is chosen because kora releases frequently and Hades is not consumed independently of it: the only consumers of a Hades version are kora instances, theme authors targeting an instance, and package consumers who follow kora releases. Two numbers served an independence that nobody has asked for, and cost a hand-written bump, a release process and a conceptual model in every Hades pull request. A frequent release cadence also makes a major bump for a Hades break cheap, which is what lets the break be the major instead of a separate number.

Comparing against the last final release, not the pull request base and not the previous release candidate, is what keeps the check honest. A break can arrive in any of fifty pull requests and the release is still one comparison between two snapshots.

Blocking at release rather than at every pull request is chosen because the version is only a fact at release. A pull request has no version to compare, a bump required before the release is cut is a guess, and the guard that matters sits in front of the irreversible step. The advisory pull request report keeps the early warning without the guess.

---

## Consequences

### Positive

- One version everywhere: image, chart, package, theme target and the release a theme builder selects. No second number to understand.
- No Hades-only bump in a pull request, no `version.ts`, no parity check, no second release train.
- The release is blocked, in front of the irreversible publish, when it contains a Hades break and is not a major.
- A theme records one fact, the release it targets, and a builder can offer every release's surface from its assets.
- The package follows Semantic Versioning, because the classifier holds the line on the one level, major, that consumers rely on.

### Negative

- A Hades break forces a kora major. That is intended and accepted by the deciders, given frequent releases, but it couples a design-system change to the product's version line.
- Below `1.0.0` nothing is blocked, so the protection starts only when kora reaches `1.0.0`. Until then the classifier reports what it would have required.
- A theme is only promised to work within a major. A theme author must reissue a theme on each major.
- The classifier runs at release, so a break is learned in the pull request report if the author reads it, and otherwise at tag time. Cutting a release is where it surfaces.
- The manifest generator parses `tokens.css` for token names and the semantic mapping, and reads kinds, formats, slots and bounds from `surface.ts`. A raw token present in one file and absent from the other fails the sync check, so the two cannot drift, but every new token is declared twice.
- The "newly read by a pre-existing component" check is a source search, which can produce a false major when a token name appears in a comment or a string. A false major is the safe direction, and the report shows the match for review.
- The four changes before the first contract, in particular moving `KoraLogo` out of Hades, are real frontend work that this ADR schedules ahead of any contract.
- The slots are only the identity the product renders today, the logo and the favicon. A mark-only or wordmark-only slot is added when a surface needs one, which the classifier records as a minor.

### Neutral

- There are no `hades/` tags. kora's `v*` tags, including release candidates, are the only tags.
- `compatibility.yaml` does not change.
- Pre-releases of a version are compared against the last final release, so `v1.4.0-rc.1` and `v1.4.0` owe the same.

---

## Implementation

### Package changes

- `frontend/src/packages/hades/package.json`: `version` is `0.0.0` in the tree and is set from the release tag by the release workflow.
- `frontend/src/packages/hades/src/tokens/surface.ts`: the declared kinds, formats, slots and product-name bounds.
- `frontend/src/packages/hades/manifest.json`: generated by `make hades-manifest` from `tokens.css` and `surface.ts`, without a version; `make check` fails on drift.
- `frontend/src/packages/hades/CONTRACT.md`: the five contract boundary rules; `make check` diffs it against the Hades PRD.
- `frontend/src/packages/hades/scripts/manifest.ts` and `classify.ts`: the generator and the classifier. The classifier takes `--base <ref>` and `--release <version>`, returns one level, derives what the release owes, and prints the template in SD-4 when it blocks.
- No `version.ts`; `HADES_VERSION` and `CONTRACT_VERSION` are not exported.

### Frontend changes

- Startup log line with the instance's version, alongside the theme log the white-labelling PRD requires.

### Repository changes

- `Makefile`: `hades-manifest`, `hades-classify`; `check` includes the manifest sync check and the `CONTRACT.md` diff. `check-versions` no longer covers Hades.
- `.github/workflows/ci.yml`: a `hades` output on the change-detection job, passed to `frontend-ci` as an input.
- `.github/workflows/frontend-ci.yml`: the advisory classification job, gated on the new input, with full history and tags, read-only (`contents: read`), always passing. It is not part of `frontend-gate`. Actions pinned by commit SHA, as every workflow in this repository is.
- `.github/workflows/release.yml`: the `hades-verify` job, first, which resolves the last final release, runs the classifier and blocks as SD-4 describes. The delivery ADR's jobs follow it.
- `.github/workflows/hades-release.yml`: removed. Its jobs move into `release.yml` under the delivery ADR.
- `.github/pull_request_template.md` or a Hades-specific template: the two review questions.

### Migration Path

kora is `0.x`, so nothing is blocked. The classifier runs on pull requests and at release, and its report states what the release would owe from `1.0.0`. The theme document key is `koraVersion` from the start; Hades has not been released, so this is not a document-format change against any baseline. Once kora reaches `1.0.0` the first final release at or above it is the baseline, and enforcement starts with the release after it.

### Timeline

Before the white-labelling theme ADR's contract subsection is finalised, because that subsection generates its validation rules from the manifest shape and cites the asset formats and product-name bounds decided here.

---

## Validation

### Success Metrics

- `make check` fails when `manifest.json` is stale.
- A release at or above `1.0.0` whose changes remove a raw token, and whose version is not a major, fails `hades-verify`, and the message names the token, boundary rule 1, `L`, `R` and the required version.
- A release that removes a component export without a major fails the same way, and the message says no theme is affected.
- A release whose Hades changes are an added export and nothing else, at a minor, passes.
- A release candidate and its final release owe the same.
- A pull request that only edits the Hades README classifies as none and passes.
- The published package's version equals the release tag's version.
- The instance's startup log names its version.

### Monitoring

- The report in each Hades pull request's job summary shows what the change would owe; the report in `hades-verify` is the record of what a release did to the contract.
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

The PRDs cite this ADR by subsection number. SD-1 to SD-4 above are those subsections, in order.

**Last Updated:** 2026-10-15
