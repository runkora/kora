# Hades Delivery to External Consumers

**Status:** Accepted, amended 2026-10-15
**Date:** 2026-10-11
**Deciders:** Qj Stout-Spykers
**Technical Story:** The versioning story in the [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it) requires that Hades can be obtained outside the kora frontend as a buildable package, per release, so that consumers kora assumes will exist can render the product's own components.

> **Amendment, 2026-10-15.** Hades no longer has releases of its own. The [versioning ADR](2026-10-11-hades-versioning-and-theme-surface-manifest.md) withdrew the `hades/v*` tag, the package Semantic Version and the contract version: Hades is released with kora, under the kora tag. This ADR keeps what it delivers and how, and changes when and under which version: the jobs that were a separate `hades-release.yml` workflow are now jobs of kora's `release.yml`, and the package version is the kora release version.

---

## Context

Hades lives at `frontend/src/packages/hades/` and stays there, named `@orbit/hades`. The frontend consumes it by pnpm workspace link and Next.js compiles it from TypeScript source; there is no build output, no publish step, and `package.json` is marked `private`. Its runtime dependencies are declared as peer dependencies, apart from one ordinary dependency, and installed at the workspace root. Its styling is Tailwind v4 utilities, which the consuming application compiles after importing `tokens.css` and scanning the package's component files with `@source`. Fonts are not part of the package: the font tokens reference CSS variables the application sets.

The white-labelling PRDs assume consumers outside this repository will render kora's own components against a theme, one release per kora release. Those consumers are specified in their own documents and are not named here, so that Hades never depends on anything that depends on it. What this ADR decides is the artefact Hades ships and the mechanism that ships it, for any consumer.

### Background

- The [versioning ADR](2026-10-11-hades-versioning-and-theme-surface-manifest.md) decides that Hades is released with kora under the kora `vX.Y.Z` tag, that the tag is stamped into the package when the release is built, and that every release carries `manifest.json`, `theme.schema.json` and `defaults.json`.
- The organisation already publishes container images to `ghcr.io`, so GitHub Packages is in use and authenticated from CI.
- The Hades PRD excludes Storybook, and any design-tooling artefact, from the Hades deliverable.

### Problem Statement

Decide what a Hades release delivers, in what form, through what channel, and how it is proven buildable without the Next.js application.

### Goals

- A consumer can install a specific Hades release by version and build a React application with it, without cloning this repository or the kora frontend.
- The deliverable carries the tokens, the manifest, the components, and the theme module and schema the white-labelling theme ADR places in Hades. Nothing else.
- Publishing is a consequence of a kora release, with no manual step and no release of its own to cut.
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
* The repository is private today and is being open-sourced within weeks, at which point the package is made public. Until then the channel must be authenticated and already available to the organisation.
* Delayed decision making.

---

## Considered Options

### Option 1: Publish to the GitHub Packages npm registry on each final kora release

**Description:** The release workflow packs the package and publishes it to `npm.pkg.github.com`. That registry accepts only packages whose scope is the owning account, so the package must be renamed from `@orbit/hades` to `@runkora/hades`. The package ships its TypeScript source, `tokens.css`, `manifest.json`, `defaults.json` (the raw default of every settable token per mode: the colours a consumer passes to the validator to check contrast, and the radii, fonts and shadows a theme builder shows as the fallback for an unset field) and `README.md`. Consumers add the registry for the scope in their `.npmrc`, authenticate with a token that can read packages, and depend on the package by semver range.

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

**Description:** The consumer depends on `github:runkora/kora#v1.2.0` and points at the package subdirectory.

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

**Chosen Option:** Option 1, publish to the GitHub Packages npm registry on each final kora release, with the tarball also attached to the GitHub Release, for release candidates too, as a fallback. The package is renamed `@runkora/hades`, because the registry requires the scope to be the owning organisation.

### What ships

The published package contains exactly:

- `src/**` excluding `*.stories.tsx`, `*.test.ts`, `*.test.tsx`, `test-setup.ts`, `src/docs/` and `.storybook/`.
- `src/tokens/tokens.css` and `src/tokens/surface.ts`.
- `manifest.json` from the versioning ADR, and `theme.schema.json` from the white-labelling theme ADR.
- `README.md`, which documents the consumer requirements below.
- `package.json` named `@runkora/hades`, with `private` removed and `version` set from the release tag by the workflow, a `files` whitelist producing the list above, `publishConfig` pointing at `npm.pkg.github.com`, the `repository` field with `directory`, the existing dependencies, and `exports` extended with `./manifest.json` and `./theme.schema.json` so a consumer can read what it is told it receives.

It does not contain fonts, Storybook, stories, tests, or any build of its own. The kora frontend's own consumption is unaffected beyond the rename: the workspace link ignores `files` and `publishConfig`, and `exports` keeps its existing entries.

### Consumer requirements

Stated in the README and treated as part of the deliverable:

- React 19 and the peer dependencies as declared.
- Tailwind v4, importing `tokens.css` and scanning the package with `@source` the way the kora frontend does.
- TypeScript compilation of the package source, as the frontend does through `transpilePackages`.
- The font slot tokens `--hades-raw-font-sans` and `--hades-raw-font-mono` set by the consumer, or by the theme it renders, to font families it loads itself. Hades references them and never loads a font.
- The package version follows the kora release tag. No check makes a break to the components, props, peer ranges or tokens a major, so a consumer should not assume a caret range is safe across minors; nothing is promised below `1.0.0`. The package version is the kora release version; a consumer that renders themes decides whether a theme is compatible by the rule in the white-labelling theme ADR, comparing the theme's `koraVersion` with that version.

### Release workflow

The jobs are part of kora's `.github/workflows/release.yml`, triggered by `v*` tags, in the order below. This ADR owns all of them. Hades jobs sit beside the image and chart jobs and share the workflow's tag, concurrency group.

1. **hades-verify**: the manifest and `theme.schema.json` are verified in sync with their sources. It runs first, before any image is built or anything is published.
2. **hades-test**: the Hades type check under the package's own strict flags, and the Hades unit tests.
3. **hades-pack-and-smoke**: set `package.json` `version` from the tag, then `pnpm --filter @runkora/hades pack` into `runkora-hades-X.Y.Z.tgz`, uploaded as a workflow artefact. Then the **smoke build**: scaffold a minimal Vite plus React plus Tailwind v4 application in a temporary directory, install the tarball into it, render a page that imports `tokens.css` and the `Button` component, and run its production build. This is the proof that the package is buildable without the Next.js application, and it runs on every release, release candidates included.
4. **release assets**: kora's existing `publish` job, which creates the GitHub Release, attaches the tarball, `manifest.json`, `theme.schema.json` and `defaults.json` to it, release candidates included, so a theme builder can offer every release, read its surface and run the contrast check against that release's colour defaults and show the default of every other settable token, so each release must attach its own regenerated `defaults.json`.
5. **hades-publish**: publish the same tarball to GitHub Packages, for a final release only. A release candidate does not publish, because a published package version can be neither removed nor published again. It runs after kora's `publish` job, so a failure of the images, the charts or the release leaves no package version published. A failure of `hades-publish` itself leaves the release in place and no package, and is recovered by re-running that job on its own.

### Workflow requirements

- **Permissions.** The workflow declares `permissions: contents: read` at the top, as every workflow in this repository does. The `hades-publish` job alone adds `packages: write` for the registry; the job that attaches release assets adds `contents: write`. The Hades jobs hold nothing more than the permissions kora's existing jobs for the same steps hold.
- **Actions pinned by commit SHA**, per the repository's Scorecard convention. The actions needed are checkout, the pnpm and Node setup already used by `frontend-ci`, upload and download of the tarball artefact, and the GitHub CLI for the release, which is preinstalled on the runner.
- **Registry authentication.** The Node setup step is given `registry-url: https://npm.pkg.github.com` and `scope: @runkora`, which writes the `.npmrc` for the job; `NODE_AUTH_TOKEN` is the workflow token. The built-in token can publish to GitHub Packages for the repository that owns the workflow; no personal access token is stored.
- **Package linkage.** `package.json` gains `"repository": { "type": "git", "url": "git+https://github.com/runkora/kora.git", "directory": "frontend/src/packages/hades" }`. GitHub Packages links a package to the repository named here, which is what lets the workflow token publish it.
- **Consumer access.** A package first published with the workflow token inherits the access of the linked repository: accounts that can read the repository can read the package. A consumer in another repository of the organisation reads it with a personal access token (classic) that has `read:packages`, held as a secret in that repository, because the GitHub Packages npm registry accepts neither fine-grained personal access tokens nor GitHub App installation tokens, or, for its Actions workflows, that repository granted access in the package's own settings after the first publish, after which its built-in token suffices. The consumer's `.npmrc` carries two lines: the scope-to-registry mapping and the token reference. Which of these a given consumer uses is that consumer's decision. Making the package public with the repository widens who may read it but does not remove the token: the GitHub Packages npm registry requires one for every install, and kora accepts that cost in exchange for the registry, the scope and the publishing path that already exist. `publishConfig` carries only the registry; an `access` field does not govern visibility on GitHub Packages and is not set.
- **No provenance attestation.** npm provenance is supported only for the public npm registry, not GitHub Packages. kora signs its images with cosign; the equivalent for the Hades tarball is the checksum published in the GitHub Release, which is enough while every consumer is inside the organisation. Revisit when the package is made public, if provenance is wanted by consumers outside it.
- **No second tag namespace.** There is no `hades/v*` tag, so no filter on kora's previous-release lookup and no local tagging target. A Hades release is cut by cutting a kora release.

### Rationale

Option 1 is the only option that gives consumers semver resolution, and it is the one whose infrastructure already exists. Shipping source rather than a build keeps one toolchain, matches how the frontend consumes the package today, and defers the compiled distribution until a consumer that needs it appears. The smoke build in the release workflow turns "buildable without the Next.js app" from a claim into a check that runs every release. Releasing with kora, rather than on its own tags, removes a second release train whose only purpose was independence nobody asked for.

---

## Consequences

### Positive

- A consumer installs Hades by version and upgrades by range, like any dependency.
- Each release is proven to build outside kora before it is published.
- The deliverable is exactly the contract surface plus the components, which is what the white-labelling PRDs need and nothing a client should not see.
- No second repository.

### Negative

- The rename to `@runkora/hades` is a one-time change across 292 files. It is mechanical, but it lands as its own PR before the first release.
- `package.json` is no longer `private`, so a mistaken `pnpm publish` from a developer machine is possible. Mitigated by `publishConfig` naming the registry, and by the registry requiring a token with write scope that developers do not hold.
- A consumer that wants only a Hades fix receives a kora version number that moves for reasons unrelated to Hades. Accepted: the version is one number, and kora releases frequently.
- Consumers carry the Tailwind v4 and TypeScript requirements. Acceptable while every consumer is a React application on the same stack.
- The smoke build adds a minute or two to each Hades release.

### Neutral

- Hades assets are attached to kora's own GitHub Release; there is no separate Hades release.
- Consumers see one release per published version; which majors continue to be published is undecided until it matters.

---

## Implementation

- Rename `@orbit/hades` to `@runkora/hades` everywhere: the package name, the frontend's dependency, every import under `frontend/src`, the ESLint rules under `frontend/eslint`, `next.config.ts`, the Vitest configs, the Makefile's `--filter` arguments, the Dockerfile, and the docs. The ESLint rule names, Storybook titles and make target names keep the word Hades and do not change.
- `frontend/src/packages/hades/package.json`: remove `private`; add `files`, `publishConfig` (`registry` only), the `repository` field with `directory`, and the two new `exports` entries. `version` stays `0.0.0` in the tree.
- `frontend/src/packages/hades/README.md`: consumer requirements, how a theme's `koraVersion` is checked, and the two `.npmrc` lines a consumer needs.
- `.github/workflows/release.yml`: the jobs above, with the permissions and pinning stated under Workflow requirements.
- `.github/workflows/hades-release.yml`: removed.
- `.github/scripts/hades-smoke-build.sh`: the Vite scaffold, install and build, taking the tarball path as its argument.
- `Makefile`: no `hades-release` target.
- Organisation settings: nothing. Package access for consumer repositories is configured per package after the first publish, by whoever owns the consumer.

### Migration Path

None for kora. The first published version is the first final kora release that contains this mechanism, which is `0.x`; nothing is promised below `1.0.0`. Release candidates are packed and smoke-built but never published.

### Timeline

With the first final kora release after this ADR lands.

---

## Validation

### Success Metrics

- A final kora release `vX.Y.Z` results in `@runkora/hades@X.Y.Z` at `npm.pkg.github.com` and the three assets on kora's GitHub Release, with no manual step; a release candidate attaches the assets and publishes no package.
- The smoke build passes on every release, and fails the release if the package cannot build outside kora.
- A consumer in another repository installs `@runkora/hades` at a released version with only an `.npmrc` entry and a read token.
- The kora frontend builds and its tests pass after the rename, with no change to how it consumes the package.

### Monitoring

- The GitHub Packages version list is the delivery history.
- The release workflow's smoke build is the standing check on consumer buildability.

---

## Related Decisions

- [hades-versioning-and-theme-surface-manifest](2026-10-11-hades-versioning-and-theme-surface-manifest.md) - the release this ADR delivers, and the manifest it ships
- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) - the package structure, peer dependencies and Tailwind integration the deliverable relies on

---

## References

- [Hades Design System PRD, versioning story](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [White-labelling PRD](../prd/white-labelling.md)
- [GitHub Packages npm registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)

---

## Notes

The package is renamed `@runkora/hades` because the GitHub Packages registry requires a package's scope to be the owning organisation. Nothing else about the package's identity changes: the ESLint rules, Storybook titles and make targets keep the name Hades.

**Last Updated:** 2026-10-15
