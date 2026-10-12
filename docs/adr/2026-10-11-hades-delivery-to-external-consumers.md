# Hades Delivery to External Consumers

**Status:** Accepted
**Date:** 2026-10-11
**Deciders:** Qj Stout-Spykers
**Technical Story:** The versioning story in the [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it) requires that Hades can be obtained outside the kora frontend as a buildable package, per major, so that consumers kora assumes will exist can render the product's own components.

---

## Context

Hades lives at `frontend/src/packages/hades/` and stays there, named `@orbit/hades`. The frontend consumes it by pnpm workspace link and Next.js compiles it from TypeScript source; there is no build output, no publish step, and `package.json` is marked `private`. Its runtime dependencies are declared as peer dependencies, apart from one ordinary dependency, and installed at the workspace root. Its styling is Tailwind v4 utilities, which the consuming application compiles after importing `tokens.css` and scanning the package's component files with `@source`. Fonts are not part of the package: the font tokens reference CSS variables the application sets.

The white-labelling PRDs assume consumers outside this repository will render kora's own components against a theme, one release per major. Those consumers are specified in their own documents and are not named here, so that Hades never depends on anything that depends on it. What this ADR decides is the artefact Hades ships and the mechanism that ships it, for any consumer.

### Background

- The [versioning ADR](2026-10-11-design-system-semantic-versioning.md) decides that a Hades release is a `hades/vX.Y.Z` tag matching `package.json`, and that each release carries `manifest.json`, `CONTRACT.md` and an exported `HADES_VERSION`.
- The organisation already publishes container images to `ghcr.io`, so GitHub Packages is in use and authenticated from CI.
- The Hades PRD excludes Storybook, and any design-tooling artefact, from the Hades deliverable.

### Problem Statement

Decide what a Hades release delivers, in what form, through what channel, and how it is proven buildable without the Next.js application.

### Goals

- A consumer can install a specific Hades release by version and build a React application with it, without cloning this repository or the kora frontend.
- The deliverable carries the tokens, the manifest, the contract boundary rules, the components, and the theme module and schema the white-labelling theme ADR places in Hades. Nothing else.
- Publishing is a consequence of tagging a release, with no manual step.
- The mechanism costs this repository as little as possible, because Hades is first a workspace package and only second a published one.

### Non-Goals

- Deciding which majors continue to receive releases. Left until it matters, per the PRD.
- A compiled JavaScript distribution. Consumers that cannot compile TypeScript and Tailwind v4 do not exist yet.
- Shipping fonts, Storybook, stories, tests or documentation beyond the README and the contract.
- Extracting Hades to its own repository. The PRD rules it out.

---

## Decision Drivers

* Hades stays in this repository and keeps working by workspace link; delivery must not change how the frontend consumes it.
* This ADR requires consumers to be React applications on Tailwind v4, the same stack as the frontend, so that shipping source is sufficient and there is one build toolchain.
* Versioned resolution matters: a consumer must be able to say "the latest 1.2.x" and get it, which a URL to a tarball cannot do.
* The repository is private; the channel must be authenticated and already available to the organisation.
* Delayed decision making.

---

## Considered Options

### Option 1: Publish to the GitHub Packages npm registry on each `hades/v*` tag

**Description:** The release workflow packs the package and publishes it to `npm.pkg.github.com`. That registry accepts only packages whose scope is the owning account, so the package must be renamed from `@orbit/hades` to `@clear-route/hades`. The package ships its TypeScript source, `tokens.css`, `manifest.json`, `CONTRACT.md` and `README.md`. Consumers add the registry for the scope in their `.npmrc`, authenticate with a token that can read packages, and depend on the package by semver range.

**Pros:**
- Standard npm resolution: ranges, lockfiles, `pnpm outdated`, one line to upgrade.
- Authentication and the registry already exist for the organisation; CI publishes with its own token.
- No new repository, no mirror, no copying.
- The same tarball can be attached to a GitHub Release as a fallback.

**Cons:**
- The rename touches every import of the package: 292 files across the frontend source, its ESLint rules, configs, the Makefile, the Dockerfile and the docs. It is a one-time mechanical change, but it is not additive.
- `package.json` must drop `private` and declare `files` and `publishConfig`, which slightly changes a file the frontend also depends on.
- Consumers must configure a scoped registry and a token. One-time setup per consumer.
- A source distribution requires the consumer to compile TypeScript and Tailwind v4 and to set the font variables itself. These are documented requirements, not surprises, but they are requirements.

### Option 2: Consume by git reference to a subdirectory of this repository

**Description:** The consumer depends on `github:clear-route/orbit#hades/v1.2.0` and points at the package subdirectory.

**Pros:**
- No registry, no publish step.

**Cons:**
- pnpm's git dependencies do not support a subdirectory of a monorepo without workarounds; the consumer would clone the whole kora repository on every install.
- No range resolution; every upgrade is a tag edit.
- Grants the consumer's CI read access to the entire repository rather than one package.

### Option 3: Mirror each release into a separate `hades` repository by subtree split

**Description:** The release workflow splits the package directory into its own repository and tags it there; consumers depend on that repository.

**Pros:**
- A clean, standalone repository for consumers to look at.

**Cons:**
- It is extraction by another name. The PRD keeps Hades here, and a mirror invites changes that never flow back.
- Two repositories to keep in step, with the split history as a second source of truth.
- Still no range resolution unless the mirror also publishes, at which point Option 1 was the work anyway.

### Option 4: Attach a packed tarball to a GitHub Release and consume it by URL

**Description:** `pnpm pack` in the release workflow; the tarball is attached to a GitHub Release for the tag; consumers depend on the asset URL.

**Pros:**
- No registry configuration.

**Cons:**
- No range resolution and no lockfile integrity by name; every upgrade is a URL edit.
- Private release assets still need a token, so the authentication cost is the same as Option 1 with none of the resolution benefit.

---

## Decision

**Chosen Option:** Option 1, publish to the GitHub Packages npm registry on each `hades/v*` tag, with the tarball also attached to the GitHub Release as a fallback. The package is renamed `@clear-route/hades`, because the registry requires the scope to be the owning organisation.

### What ships

The published package contains exactly:

- `src/**` excluding `*.stories.tsx`, `*.test.ts`, `*.test.tsx`, `test-setup.ts`, `src/docs/` and `.storybook/`.
- `src/tokens/tokens.css` and `src/tokens/surface.ts`.
- `manifest.json` and `CONTRACT.md` from the versioning ADR, and `theme.schema.json` from the white-labelling theme ADR.
- `README.md`, which documents the consumer requirements below.
- `package.json` named `@clear-route/hades`, with `private` removed, a `files` whitelist producing the list above, `publishConfig` pointing at `npm.pkg.github.com`, the `repository` field with `directory`, the existing dependencies, and `exports` extended with `./manifest.json`, `./CONTRACT.md` and `./theme.schema.json` so a consumer can read what it is told it receives. `src/index.ts` exports `HADES_VERSION`.

It does not contain fonts, Storybook, stories, tests, or any build of its own. The kora frontend's own consumption is unaffected beyond the rename: the workspace link ignores `files` and `publishConfig`, and `exports` keeps its existing entries.

### Consumer requirements

Stated in the README and treated as part of the deliverable:

- React 19 and the peer dependencies as declared.
- Tailwind v4, importing `tokens.css` and scanning the package with `@source` the way the kora frontend does.
- TypeScript compilation of the package source, as the frontend does through `transpilePackages`.
- The font slot tokens `--hades-raw-font-sans` and `--hades-raw-font-mono` set by the consumer, or by the theme it renders, to font families it loads itself. Hades references them and never loads a font.
- A patch-range pin, such as `~1.2.0`. The Hades version promises the theme contract; a minor may change the component API, as the versioning ADR defines.

### Release workflow

One workflow, `.github/workflows/hades-release.yml`, triggered by `hades/v*` tags. The versioning ADR owns its first jobs; this ADR owns the rest. Jobs, in order, each depending on the previous:

1. **verify**: the tag equals `package.json` `version`, is not a `0.x` version, `version.ts` and `manifest.json` agree with it, and the manifest is in sync with its sources.
2. **test**: the Hades unit tests.
3. **pack-and-smoke**: `pnpm --filter @clear-route/hades pack` into `clear-route-hades-X.Y.Z.tgz`, uploaded as a workflow artefact. Then the **smoke build**: scaffold a minimal Vite plus React plus Tailwind v4 application in a temporary directory, install the tarball into it, render a page that imports `tokens.css`, the `Button` component and `HADES_VERSION`, and run its production build. This is the proof that the package is buildable without the Next.js application, and it runs on every release.
4. **publish**: publish the same tarball to GitHub Packages.
5. **release**: create a GitHub Release named `hades/vX.Y.Z` with the tarball, `manifest.json`, `CONTRACT.md` and `theme.schema.json` attached, marked as not the repository's latest release, so kora's own releases keep that position.
6. **cleanup**, run only if any job above failed: delete the tag, so the version can be re-cut once the cause is fixed. A failure leaves nothing published and is reported on the commit.

### Workflow requirements

- **Permissions.** The workflow declares `permissions: contents: read` at the top, as every workflow in this repository does. The `publish` job alone adds `packages: write`; the `release` and `cleanup` jobs each add `contents: write`. No job holds both.
- **Actions pinned by commit SHA**, per the repository's Scorecard convention. The actions needed are checkout, the pnpm and Node setup already used by `frontend-ci`, upload and download of the tarball artefact, and the GitHub CLI for the release, which is preinstalled on the runner.
- **Registry authentication.** The Node setup step is given `registry-url: https://npm.pkg.github.com` and `scope: @clear-route`, which writes the `.npmrc` for the job; `NODE_AUTH_TOKEN` is the workflow token. The built-in token can publish to GitHub Packages for the repository that owns the workflow; no personal access token is stored.
- **Package linkage.** `package.json` gains `"repository": { "type": "git", "url": "https://github.com/clear-route/orbit.git", "directory": "frontend/src/packages/hades" }`. GitHub Packages links a package to the repository named here, which is what lets the workflow token publish it.
- **Consumer access.** A package first published with the workflow token inherits the access of the linked repository: accounts that can read the repository can read the package. A consumer in another repository of the organisation reads it with a token that has `read:packages`, either a fine-grained personal access token or a GitHub App token held as a secret in that repository, or, for its Actions workflows, that repository granted access in the package's own settings after the first publish, after which its built-in token suffices. The consumer's `.npmrc` carries two lines: the scope-to-registry mapping and the token reference. Which of these a given consumer uses is that consumer's decision. `publishConfig` carries only the registry; an `access` field does not govern visibility on GitHub Packages and is not set.
- **No provenance attestation.** npm provenance is supported only for the public npm registry, not GitHub Packages. kora signs its images with cosign; the equivalent for the Hades tarball is the checksum published in the GitHub Release, which is enough for consumers inside the organisation. Revisit if a consumer outside it ever appears.
- **Tag isolation.** kora's `release.yml` triggers on `v*`, which does not match `hades/v*`, and this workflow triggers only on `hades/v*`. One shared surface remains: kora's release step finds the previous release with an unfiltered `gh release list`, which would return a Hades release once one exists. That lookup is changed to consider only tags starting with `v`.
- **Local tagging.** `make hades-release VERSION=X.Y.Z` verifies the working tree is on `main`, clean and up to date, that `package.json` already carries `X.Y.Z`, then creates and pushes the tag. It is a convenience; the workflow re-verifies everything.

### Rationale

Option 1 is the only option that gives consumers semver resolution, and it is the one whose infrastructure already exists. Shipping source rather than a build keeps one toolchain, matches how the frontend consumes the package today, and defers the compiled distribution until a consumer that needs it appears. The smoke build in the release workflow turns "buildable without the Next.js app" from a claim into a check that runs every release.

---

## Consequences

### Positive

- A consumer installs Hades by version and upgrades by range, like any dependency.
- Each release is proven to build outside kora before it is published.
- The deliverable is exactly the contract surface plus the components, which is what the white-labelling PRDs need and nothing a client should not see.
- No second repository.

### Negative

- The rename to `@clear-route/hades` is a one-time change across 292 files. It is mechanical, but it lands as its own PR before the first release.
- `package.json` is no longer `private`, so a mistaken `pnpm publish` from a developer machine is possible. Mitigated by `publishConfig` naming the registry, and by the registry requiring a token with write scope that developers do not hold.
- A consumer on a caret range can be broken by a minor, because the version promises the theme contract and a minor may change the component API. The README says to pin a patch range.
- Consumers carry the Tailwind v4 and TypeScript requirements. Acceptable while every consumer is a React application on the same stack.
- The smoke build adds a minute or two to each Hades release.

### Neutral

- The GitHub Release for a Hades tag sits beside kora's own releases in the same repository, distinguished by the `hades/` prefix.
- Consumers see one release per published version; which majors continue to be published is undecided until it matters.

---

## Implementation

- Rename `@orbit/hades` to `@clear-route/hades` everywhere: the package name, the frontend's dependency, every import under `frontend/src`, the ESLint rules under `frontend/eslint`, `next.config.ts`, the Vitest configs, the Makefile's `--filter` arguments, the Dockerfile, and the docs. The ESLint rule names, Storybook titles and make target names keep the word Hades and do not change.
- `frontend/src/packages/hades/package.json`: remove `private`; add `files`, `publishConfig` (`registry` only), the `repository` field with `directory`, and the three new `exports` entries.
- `frontend/src/packages/hades/src/index.ts`: export `HADES_VERSION`.
- `frontend/src/packages/hades/README.md`: consumer requirements, the patch-range pin, and the two `.npmrc` lines a consumer needs.
- `.github/workflows/hades-release.yml`: the six jobs above, with the permissions and pinning stated under Workflow requirements. Jobs 1 and 2 are the versioning ADR's.
- `.github/workflows/release.yml`: the previous-release lookup filtered to tags starting with `v`.
- `.github/scripts/hades-smoke-build.sh`: the Vite scaffold, install and build, taking the tarball path as its argument.
- `Makefile`: `hades-release`.
- Organisation settings: nothing. Package access for consumer repositories is configured per package after the first publish, by whoever owns the consumer.

### Migration Path

None for kora. The first published version is `1.0.0`, per the versioning ADR. Nothing is published pre-1.0.

### Timeline

With the `1.0.0` release.

---

## Validation

### Success Metrics

- A `hades/v1.0.0` tag results in a package at `npm.pkg.github.com` and a GitHub Release with the four assets, with no manual step.
- The smoke build passes on every release, and fails the release if the package cannot build outside kora.
- A consumer in another repository installs `@clear-route/hades@~1.0.0` with only an `.npmrc` entry and a read token.
- The kora frontend builds and its tests pass after the rename, with no change to how it consumes the package.

### Monitoring

- The GitHub Packages version list is the delivery history.
- The release workflow's smoke build is the standing check on consumer buildability.

---

## Related Decisions

- [design-system-semantic-versioning](2026-10-11-design-system-semantic-versioning.md) - the release this ADR delivers, and the manifest and contract it ships
- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) - the package structure, peer dependencies and Tailwind integration the deliverable relies on

---

## References

- [Hades Design System PRD, versioning story](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [White-labelling PRD](../prd/white-labelling.md)
- [GitHub Packages npm registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)

---

## Notes

The package is renamed `@clear-route/hades` because the GitHub Packages registry requires a package's scope to be the owning organisation. Nothing else about the package's identity changes: the ESLint rules, Storybook titles and make targets keep the name Hades.

**Last Updated:** 2026-10-11
