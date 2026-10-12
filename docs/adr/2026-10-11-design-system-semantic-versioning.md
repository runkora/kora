# Design System Semantic Versioning

**Status:** Accepted
**Date:** 2026-10-11
**Deciders:** Qj Stout-Spykers
**Technical Story:** The versioning story in the [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it), required by the [White-labelling PRD](../prd/white-labelling.md), whose theme contract is the Hades major.

---

## Context

Hades is the kora design system: an isolated workspace package at `frontend/src/packages/hades/`, consumed by the frontend through the pnpm workspace protocol and compiled from source by Next.js. It carries a `version` field of `0.1.0` that nothing reads. There are no Hades releases, no tags, and no rule for deciding whether a change to it is compatible with anything, because until now nothing outside the frontend depended on it.

White-labelling changes that. A theme is a deployment artefact that sets Hades values: colour tokens per mode, radius steps, font slots, identity slots and the product name. The white-labelling PRD defines the theme contract as the Hades major, never the kora version, so that a theme authored for a major stays valid on every kora release that pins that major and only a deliberate Hades major release can break it. For that to hold, Hades needs a version with a defined meaning, a rule that decides what each change does to the version, a machine-readable description of the surface a theme can set, and a way for a kora instance to know which version it pins.

### Background

- kora releases are cut from `v*` tags. The release workflow derives the image tag and chart version from the tag, builds images, and writes a `compatibility.yaml` listing the Kubernetes, Istio, SPIRE, PostgreSQL and tooling versions the release was tested with.
- The frontend image receives `ORBIT_VERSION` as a build argument and surfaces it as `NEXT_PUBLIC_ORBIT_VERSION`, which the dashboard header shows.
- `make check-versions` already enforces parity between a pinned version in `versions.hcl` and the Go directive in `go.mod`. The same pattern, one source and a check that every copy agrees, is reused here.
- The token file `tokens.css` defines the raw palette in a `:root` block, overrides it in a `[data-theme='light']` block, and maps semantic names in a Tailwind `@theme inline` block. The radius steps are multiples of one `--hades-radius-unit`. The raw palette also holds six shadow tokens. There is no raw font token: the font families in `@theme inline` fall back through `--font-inter` and `--font-geist-mono`, names the kora app chose and sets through `next/font`.
- Identity is rendered today by one kora drawing in Hades, `KoraLogo`, the mark and wordmark as a single SVG, in the login card and the sidebar header; a static `icon.svg` under the app directory; and a title string in the root layout. `KoraMark`, the mark alone, is exported but nothing renders it.

### Problem Statement

Decide where the Hades version lives and how a release is cut; how a kora release pins the version and exposes it to the running instance; how a change to Hades is classified as patch, minor or major against the contract boundary rules; and where the classifier runs and how it reports.

The Hades PRD numbers these as subsections 1 to 4 of this ADR, and the white-labelling PRD cites subsection 2 for version exposure and subsection 3 for the manifest. The sub-decisions below keep that numbering.

### Goals

- One source of truth for the Hades version, with a release process that cannot produce a version that disagrees with the package.
- A kora release records the Hades version it pins, and a running instance can read that version and the manifest for it.
- Classification is decided on the manifest against the numbered contract boundary rules, is deterministic where it can be, and cites the rule it applied.
- A contract-breaking change cannot ship as a minor unnoticed.

### Non-Goals

- Delivering Hades to consumers outside this repository. That is the [Hades delivery ADR](2026-10-11-hades-delivery-to-external-consumers.md).
- The theme contract itself: schema, validation rules and packaging. That is subsection 1 of the white-labelling theme ADR, which depends on the manifest defined here.
- Runtime exposure of the version and manifest over HTTP. That follows from theme ingestion and validation placement, decisions 2 and 3 of the white-labelling theme ADR.
- Which majors continue to receive releases. The PRD leaves this until it matters.
- Versioning anything other than Hades. kora's own release versioning is unchanged.

---

## Decision Drivers

* The contract is the Hades major, never the kora version, and a kora change that does not change Hades must not move the Hades version.
* Delayed decision making: decide what white-labelling needs now, and no more.
* Enforcement must be unbypassable at the merge gate, because a misclassified change silently breaks every deployed theme.
* Hades stays in this repository, consumed by workspace link. The version must work with that, not require publishing to be meaningful.
* The repo already has patterns for version parity checks, release tagging and generated-file drift checks; reuse them.

---

## Considered Options

The options are grouped by sub-decision. Each sub-decision records the option chosen and why.

---

## Sub-Decisions

The Hades PRD refers to these as subsections 1 to 4.

### SD-1: Version source of truth, release process and first major

**Options considered:**

1. **`package.json` is the source of truth; a release is a tag `hades/vX.Y.Z` that matches it.** The `version` field in `frontend/src/packages/hades/package.json` is the only place the version is written by hand. A release is a tag in the `hades/` namespace pushed to a commit on `main` where the field equals the tag. The namespace keeps Hades tags out of the kora `v*` release workflow.
2. **Derive the Hades version from the kora release tag.** Every kora release implies a Hades release of the same version. Rejected outright: it makes the contract the kora version, which the PRD forbids, and it bumps Hades for changes that never touched it.
3. **A changelog-driven release tool (changesets or release-please) decides the version.** Standard in the npm ecosystem, but it adds a tool and a changeset file to every Hades PR, and it decides the bump from prose, not from the manifest. The classification in SD-3 is a better source for the bump than a hand-written changeset.

**Chosen: Option 1.**

- `frontend/src/packages/hades/package.json` `version` is the source of truth. Every other copy of the version (the exported constant in SD-2, the manifest's own version field in SD-3, the tag) must equal it, and `make check-versions` is extended to fail when they do not.
- A release is `git tag hades/vX.Y.Z` on a `main` commit where the field equals `X.Y.Z`. A workflow triggered by `hades/v*` tags verifies the equality, runs the Hades tests, and hands over to the delivery workflow. A tag that does not match the field fails and is deleted.
- Version bumps travel in the PR that makes the change, as SD-3 and SD-4 enforce, so `main` always carries the version its contents warrant and a release is only a tag.
- Pre-1.0 releases carry no contract. Nothing is promised about `0.x`.
- **The first major, `1.0.0`, is cut from the current token set once four changes have landed:** radius steps as literal tokens rather than multiples of one unit; agnostic identity slot components for the logo and the favicon, each taking a value per mode, with the product name as a value, where this ADR decides only their Hades-side props and the white-labelling theme ADR decides how the effective theme feeds them; `KoraLogo` moved out of Hades into the kora default theme at app level and `KoraMark`, which nothing renders, deleted; and two font slot tokens, `--hades-raw-font-sans` and `--hades-raw-font-mono`, defaulting to generic stacks, which `--font-sans` and `--font-mono` resolve through and the kora default theme sets to its own fonts. The asset formats and product-name bounds the manifest carries are decided in SD-3, so nothing outside this ADR blocks `1.0.0`. `1.0.0` is the baseline every later classification compares against.

### SD-2: How kora pins the version and exposes it

**Options considered:**

1. **Implicit pin through the workspace link, recorded at release, exposed as an exported constant with the manifest inside the package.** The frontend consumes Hades by `workspace:*`, so the version in the tree at a kora release is the version that release pins. The release workflow records it in `compatibility.yaml`. Hades exports `HADES_VERSION` from a checked-in `src/version.ts`, and the manifest is a file in the package, so both are present in-process and in the frontend image.
2. **Pin explicitly in `frontend/package.json` with a version range against a published package.** Only possible once Hades is published, which the delivery ADR does for external consumers, not for kora. It would also change how the frontend builds today for no gain: the tree already pins exactly one version.
3. **Stamp the Hades version into the image as a build argument, like `ORBIT_VERSION`.** Workable, but it introduces a second source that can drift from `package.json`, and it leaves the manifest without a carrier.
4. **Also serve the version and manifest from a read-only frontend endpoint.** Gives an in-cluster validator a runtime source without reaching into the image. Costs a public route for data that is already in the package and published per version. Whether it is needed depends on how a theme is ingested into a deployment and where it is validated, which the white-labelling theme ADR decides; see the last bullet under the chosen option.

**Chosen: Option 1.**

- **Pin.** Implicit through the monorepo. The Hades version in the tree at the commit a kora release is cut from is the version that release pins. No range, no second declaration.
- **Record.** The kora release workflow adds `hades: "X.Y.Z"` to `compatibility.yaml`, read from `package.json` at the release commit. A kora release therefore states its Hades major in the same file that states its Kubernetes and Istio support.
- **Export.** `frontend/src/packages/hades/src/version.ts` exports `HADES_VERSION` as a string constant. It is checked in rather than generated at build, so source consumers and the workspace link see the same value, and `make check-versions` fails if it disagrees with `package.json`.
- **Expose.** The version and the manifest from SD-3 are both files in the package and exported from it, so the frontend reads them in-process by import. The standalone image bundles what is imported, so neither sits at a path in the image. The instance logs the Hades version at startup alongside the theme it loaded, as the white-labelling PRD requires. The delivery ADR publishes the manifest with every release, so anything that knows a version can obtain the manifest without a running instance.
- **Runtime exposure over HTTP is decided in the white-labelling theme ADR.** Whether a running instance also serves the version, or the version and the manifest, depends on how a theme is ingested into a deployment and where it is validated, which are decisions 2 and 3 of that ADR. It chooses among: nothing beyond the startup log; the version only, on an existing health or version surface; or the version and the manifest on a read-only endpoint, so an in-cluster validator has a runtime source. This ADR guarantees the inputs for whichever it picks.

### SD-3: Classification rules and the manifest

**Options considered:**

1. **A checked-in manifest generated from the token file and the slot definitions, with classification as a deterministic diff against the last release plus one code check.** The manifest is the surface; classification compares manifests and inspects component code only for the one rule that needs it.
2. **Classify by reviewing the diff with no manifest.** The reviewer, human or agent, reads the change and applies the rules. Rejected: it is the status quo for every design system that has ever shipped a silent breaking change, and the PRD requires the manifest anyway for the validation rules.
3. **Derive the manifest at build time rather than checking it in.** Avoids drift by construction, but the manifest has to be diffable in a PR, deliverable to consumers, and readable by the classifier without a build. Checked-in with a drift check is the pattern the repo already uses for rendered diagrams and docs.

**Chosen: Option 1.**

**The surface declaration.** `frontend/src/packages/hades/src/tokens/surface.ts` is the declared source of everything about the surface that `tokens.css` does not say: for every settable token its `kind` and `format`; the slot definitions; and the product-name bounds. It is hand-written and reviewed like any source file. The generator reads token names from `tokens.css` and everything else from this file, and fails if a raw token in one is missing from the other.

**The inclusion rule.** Every `--hades-raw-*` custom property declared in the `:root` block of `tokens.css` is a settable token, in both modes, by rule. Presence in the `[data-theme='light']` block is a light default value, not a statement of addressability; a token with no light override is still settable in light. That makes the surface identical to the raw palette as it exists: colours, the status, provider, strategy and chart colours, the overlay tokens and the six shadow tokens. The radius steps and the two font slot tokens from SD-1 are raw tokens too once those changes land. Semantic names in `@theme inline` are never settable: Hades derives them from raw tokens.

**The manifest.** `frontend/src/packages/hades/manifest.json`, checked in, generated by `make hades-manifest` from `tokens.css` and `surface.ts`, verified in sync by `make check` the way rendered diagrams are, and delivered with the package. It contains:

- `version`: equal to `package.json`.
- `modes`: `["dark", "light"]`.
- `tokens`: one entry per settable token, with `name` (the custom property), `kind` (`color`, `shadow`, `length`, `font`), `format` (the accepted value syntax: hex or OKLCH for `color`, a CSS shadow list for `shadow`, a CSS length for `length`, a font family name for `font`), and `modes`, which is both modes for every token.
- `mapping`: for each semantic name in `@theme inline`, how it resolves. A direct `var()` reference records the raw token. An expression such as `color-mix()` or `calc()` records every raw token it references and is marked derived. A literal records no token. A theme never sets a semantic name, but the mapping is what existing components read through, so a change to it is a change to what those components read.
- `slots`: one entry per asset slot with `name`, `kind`, `formats`, `maxBytes` and `modes`.
- `productName`: the bounds on the product name.

The generator is the only thing that writes the manifest; a hand edit fails the sync check. The white-labelling validation rules are generated from it, per version. The manifest is internal: its readers are the classifier, the validation rule generator, and any theme producer kora specifies. Its shape is bespoke, documented alongside the package, and targets no interchange format.

**Asset formats and product-name bounds.** These are manifest fields, so `1.0.0` cannot exist without them, and they are decided here. The white-labelling theme ADR's contract subsection cites them rather than deciding them again.

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

**What the version promises.** The Hades version promises the theme contract. A major obliges every deployed theme to be reissued, so major is reserved for a contract break and nothing else moves it. Minor means the contract gained surface, or the component API changed in a way a consumer could notice: an export removed or renamed, a prop removed, a peer dependency range narrowed. Patch means neither. A consumer that depends on the component API pins a patch range such as `~1.2.0`; the contract alone is safe across the whole major.

**Classification.** Decided on the manifest, per mode, by comparing the manifest at the head commit with the manifest at the last `hades/v*` tag, not the PR base, so a series of PRs between releases cannot each look like a patch while together they are a major.

| Observation | Classification | Rule cited |
|---|---|---|
| A token or slot present at the last release is absent, renamed, or has a different `kind`, `format` or `modes` | major | boundary rule 1 |
| A mode is added or removed | major | boundary rule 1 |
| `productName` bounds tighten, or a slot's formats or maximum size shrink | major | boundary rule 1 |
| A top-level property of the theme document's schema, `theme.schema.json` from the white-labelling theme ADR, is added, removed or changes shape | major | boundary rule 1; an unknown top-level key fails validation on every older instance |
| A token or slot is added, and a component that existed at the last release reads it, directly or through a semantic name whose mapping now resolves to it | major | boundary rule 4, plus the token test in the PRD |
| A semantic name present at the last release is remapped to a different raw token | major | boundary rule 4 |
| A token or slot is added, and only components new since the last release read it | minor | boundary rule 4 |
| `productName` bounds loosen, or a slot's formats or maximum size grow | minor | boundary rule 1 |
| No manifest change, and the component API changed: an export removed or renamed, a prop removed, a peer dependency range narrowed in `package.json` | minor | what the version promises, above |
| No manifest change, and sources changed without changing the component API: markup or style changes, a new export, a default value change, a change to `scripts/`, build or test configuration | patch | boundary rule 5 |
| Only `README.md`, `CONTRACT.md`, `src/docs/`, stories or tests changed | none | no rule engaged |

The manifest rows are a diff of two JSON files, and the document row a diff of a third, the top level of the generated `theme.schema.json`. The direct-read row also needs a source search: for each added token, the classifier searches the component sources that existed at the last release tag for a reference to it. The remaining rows need the list of changed files and, for the component API row, a diff of the package's exported names and props, which the classifier takes from the TypeScript declarations of `src/index.ts` at both commits.

One case no script decides: a token that keeps its name, kind, format and mapping but changes what it means, such as an accent token repurposed as a status colour. That is a review question, and the PR template for Hades changes asks it.

**Enforcement.** The classification sets a floor for the version bump. A PR whose classification is patch or above must raise `package.json` from the last released version by at least the classified step. A bump above the floor is allowed; a bump below it fails CI. A classification of none owes no bump. Before the first `hades/v*` tag exists there is no release to compare against: the classifier compares against the PR base commit, labels its report "no baseline", and does not enforce the floor, because nothing is contract yet. No `0.x` tag is ever cut, and the release workflow refuses one.

### SD-4: Where the classifier runs and how it reports

Every classification in SD-3 but one is a deterministic function of the manifest diff, the semantic mapping and a source search, so the classifier is a script, not an agent. The one non-mechanical case, a token changing meaning without changing name or format, is a review question.

**Options considered:**

1. **Pull request.** A CI job runs the deterministic classifier when Hades files change, posts the result as a PR comment, and fails the check if the bump is below the floor. The same script runs locally through a make target.
2. **Pre-commit.** The classifier runs in the existing pre-commit hook. The hook already runs the full `make check`, taking several minutes; commits are routinely rebased and squashed, so the classification would run on intermediate states that never ship; and a hook is bypassable with `--no-verify`.
3. **Pre-push.** Later than pre-commit and less noisy, but still local, still bypassable, and the comparison against the last release tag needs the tag, which a developer's clone may not have fetched.

**Chosen: Option 1, pull request.**

- **The deterministic classifier** is a script under `frontend/src/packages/hades/scripts/`, invoked by `make hades-classify`. It prints the classification, the floor, the current bump, and one line per finding citing the boundary rule and the token or slot. It exits non-zero when the bump is below the floor.
- **CI** runs it as a job in `frontend-ci`. The change-detection job in `ci.yml` gains a `hades` filter on `frontend/src/packages/hades/**`, passed into the reusable workflow as a new input that gates the job. The job checks out with full history and tags, because the comparison point is the last `hades/v*` tag, which a shallow checkout does not have. It holds `pull-requests: write` to post the comment and skips the comment on pushes to `main`, where there is no PR. It is wired into the `frontend-gate` job, which is what makes it required. This is the enforcement; it cannot be bypassed.
- **Reporting.** The CI job posts or updates one PR comment with the classification table: classification, each finding with its boundary rule and the token or slot it concerns, the version at the last release, the version at head, and whether the floor is met. The check status carries the same verdict. Locally a developer runs `make hades-classify` before opening the PR to learn the bump they owe. Review confirms the classification and answers the meaning-change question; it does not replace the classifier.

---

## Decision

**Chosen Option:** SD-1 Option 1, SD-2 Option 1, SD-3 Option 1, SD-4 Option 1.

Hades is semantically versioned from its `package.json`, released by `hades/v*` tags, pinned implicitly by the monorepo and recorded in each kora release's `compatibility.yaml`, exported as a constant and carried with its manifest inside the package and the frontend image, classified deterministically on the manifest against the contract boundary rules by a script that cites the rule it applied, with the version bump enforced at pull request.

### Rationale

The manifest is the hinge. Once the surface a theme can set is a checked-in JSON file, versioning stops being a judgement about intent and becomes a diff, the classifier has something mechanical to cite, the white-labelling validator has something to generate rules from, and the delivery ADR has something to ship. Every option that skipped the manifest left one of those three without a source.

Comparing against the last release rather than the PR base is what makes the floor honest. Comparing against the base would let three patch-looking PRs ship a removed token between releases.

Pull request enforcement is chosen over hooks because it is the only point that is both unbypassable and run on the state that ships. The comment the job posts makes the result understandable without a second tool.

---

## Consequences

### Positive

- A theme's compatibility is decided by one number, the Hades major, and that number has a mechanical definition.
- Each kora release states its Hades version next to its other compatibility facts.
- A removed or renamed token cannot reach `main` without a major bump, whatever the PR description says.
- The same manifest serves versioning, validation and delivery.

### Negative

- Every PR that changes Hades sources owes a version bump in the same PR. This is deliberate: it keeps `main` releasable at any commit. Documentation, story and test changes inside the package are exempt.
- The manifest generator parses `tokens.css` for token names and the semantic mapping, and reads kinds, formats, slots and bounds from `surface.ts`. A raw token present in one file and absent from the other fails the sync check, so the two cannot drift, but every new token is declared twice.
- The "newly read by a pre-existing component" check is a source search, which can produce a false major when a token name appears in a comment or a string. A false major is the safe direction, and the comment shows the match for review.
- The four changes before `1.0.0`, in particular moving `KoraLogo` out of Hades, are real frontend work that this ADR schedules ahead of any contract.
- The slots are only the identity the product renders today, the logo and the favicon. A mark-only or wordmark-only slot is added when a surface needs one, which the classifier records as a minor.
- A component removal or a prop removal is a minor, not a major, so a consumer on a caret range can be broken by a minor. The README tells consumers to pin a patch range; the trade is deliberate, because a major forces every client theme to be reissued.

### Neutral

- Hades tags live beside kora tags in one repository, distinguished by the `hades/` prefix.
- `compatibility.yaml` gains one line.
- Pre-1.0, the classifier runs and reports but does not fail CI.

---

## Implementation

### Package changes

- `frontend/src/packages/hades/src/version.ts`: `export const HADES_VERSION = '0.1.0' as const;` kept equal to `package.json` by `make check-versions`.
- `frontend/src/packages/hades/src/tokens/surface.ts`: the declared kinds, formats, slots and product-name bounds.
- `frontend/src/packages/hades/manifest.json`: generated by `make hades-manifest` from `tokens.css` and `surface.ts`; `make check` fails on drift.
- `frontend/src/packages/hades/CONTRACT.md`: the five contract boundary rules; `make check` diffs it against the Hades PRD.
- `frontend/src/packages/hades/scripts/manifest.ts` and `classify.ts`: the generator and the classifier, run under the package's existing Vitest and TypeScript toolchain.
- `src/index.ts` exports `HADES_VERSION`.
- The four pre-1.0 changes from SD-1, each a normal Hades PR with its own classification.

### Frontend changes

- Startup log line with the Hades version, alongside the theme log the white-labelling PRD requires.

### Repository changes

- `Makefile`: `hades-manifest`, `hades-classify`; `check-versions` extended; `check` includes the manifest sync check and the `CONTRACT.md` diff.
- `.github/workflows/ci.yml`: a `hades` output on the change-detection job, passed to `frontend-ci` as an input; the classification job added to `frontend-gate`.
- `.github/workflows/frontend-ci.yml`: the classification job, gated on the new input, checking out with `fetch-depth: 0` and tags, holding `pull-requests: write`, skipping the comment on `main`. Actions pinned by commit SHA, as every workflow in this repository is.
- `.github/workflows/hades-release.yml`: triggered by `hades/v*` tags. Its first jobs verify the tag equals `package.json`, refuse a `0.x` tag, run the Hades tests and the manifest sync check; its later jobs are the delivery ADR's. kora's own `release.yml` filters on `v*` tags, which does not match `hades/v*`, so a Hades tag never triggers a kora release.
- `.github/scripts/write-compatibility.sh`: adds the `hades` line.
- `.github/pull_request_template.md` or a Hades-specific template: the meaning-change review question.

### Migration Path

Hades stays at `0.x`, untagged, until the four SD-1 changes land. During that period the classifier compares against each PR's base, reports "no baseline" and does not enforce, so the team sees the classifications it would have produced before any of them count. `1.0.0` is tagged once the four changes are in and the manifest for them is reviewed. From then on the floor is enforced.

### Timeline

Before the white-labelling theme ADR's contract subsection is written, because that subsection generates its validation rules from the manifest shape decided here and cites the asset formats and product-name bounds decided here.

---

## Validation

### Success Metrics

- `make check-versions` fails when `package.json`, `version.ts` and `manifest.json` disagree.
- `make check` fails when `manifest.json` is stale.
- A PR that removes a raw token without a major bump fails CI, and the check names the token and boundary rule 1.
- A PR that removes a component export without a minor bump fails CI, and the check names the export.
- A kora release's `compatibility.yaml` names the Hades version in the tree at the release commit.
- The local instance's startup log names the same Hades version as the package in the image.
- A PR that only edits the Hades README classifies as none and passes CI without a bump.

### Monitoring

- The classification comment on every Hades PR is the running record of what each change did to the contract.
- The Hades tag list is the release history; the gap between the last tag and `main`'s `package.json` is the pending release.

---

## Related Decisions

- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) - the design system this versions; amended to point here
- [hades-delivery-to-external-consumers](2026-10-11-hades-delivery-to-external-consumers.md) - ships each release decided here to consumers outside the frontend
- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md) - the version parity check pattern reused by `check-versions`

---

## References

- [Hades Design System PRD, versioning story](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [White-labelling PRD](../prd/white-labelling.md)
- [Semantic Versioning 2.0.0](https://semver.org/)

---

## Notes

The PRDs cite this ADR by subsection number. SD-1 to SD-4 above are those subsections, in order.

**Last Updated:** 2026-10-11
