# White-labelling Theme

**Status:** Accepted
**Date:** 2026-10-12
**Deciders:** Qj Stout-Spykers
**Technical Story:** The six architectural decisions the [White-labelling PRD](../prd/white-labelling.md) defers to this ADR, numbered as that PRD numbers them so other documents can cite each one. Depends on [Design System Semantic Versioning](2026-10-11-design-system-semantic-versioning.md) for the manifest, the version and the contract boundary rules, and on [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) for how a producer obtains Hades.

---

## Context

A kora deployment serves one client organisation and should look and read like that client's product. The PRD defines the theme as a deployment artefact: it conforms to a contract defined by the Hades design system major, it is supplied with the rest of an instance's configuration, it changes only on redeploy, it may be partial, and a malformed value fails the deploy while a missing value falls back to the kora default. The PRD is written without reference to any producer of themes, and this ADR keeps that stance: nothing here depends on how a theme is authored.

### Background

The platform the theme has to fit:

- The published Helm charts are the product ([Install and Distribution Model](2026-09-04-install-and-distribution-model.md)). Chart values are the configuration surface, consumers apply them from their own GitOps repository with Helm or Argo CD, and the operator reconciles tenant lifecycle only. Charts render identically with no cluster access and produce no perpetual diff.
- Each backend service receives its per-install settings as a chart-rendered file at `/etc/orbit/config/{module}.yaml`, merged by the typed loader, with a checksum annotation that rolls the pods on change and no hot reload ([Rendered Service Config Files](2026-10-06-rendered-service-config-files.md)).
- The frontend is a standalone Next.js image. It receives configuration as environment variables from a ConfigMap, loads Inter and Geist Mono at build time through `next/font`, sets its favicon from a build-time file, and has no way to serve a file that arrives after the build. The Hades token sheet declares raw tokens on `:root` for dark and overrides them under `[data-theme='light']`, and `next-themes` sets `data-theme` before hydration.
- Kubernetes API objects are bounded by etcd at roughly one megabyte, so fonts and logos cannot travel inside a ConfigMap, a Secret or a custom resource.
- The charts require Kubernetes 1.34 and are tested to 1.36. Image volumes, which mount an OCI image directly into a pod, are beta and off by default in 1.34, beta and on by default in 1.35, where a cluster administrator can still disable the gate, and stable with the gate locked on in 1.36.
- The operator already reads image configs from registries with the cluster's pull secrets, for the `SandboxImage` resource.
- Storybook renders every top-level page as a story under the `Pages/` prefix, from the real page components with mock data.

### Problem Statement

Decide how a theme is expressed, how it and its brand assets reach a deployment and its pods, where it is validated so an invalid theme fails before an instance serves, how the frontend renders it without a flash of the kora default, how the backend learns the product name, how new hardcoded product-name literals are kept out, and whether real pages can be exposed for preview outside the app.

### Goals

- One theme per deployment, applied only by redeploy, attributable through the deployment configuration's history.
- Brand assets never forced into a git repository.
- A malformed theme fails the deploy with a message that names the fault, before any page serves. A missing value never fails.
- No flash of kora colours or identity on first paint. A theme font may swap in once.
- Nothing kora-specific in Hades, and nothing in the instance that assumes a particular producer.
- Nothing a consumer must set up beyond their deployment configuration and a registry they already use.

### Non-Goals

- Runtime theme changes, per-tenant themes, and theme authoring. All excluded by the PRD.
- Raster assets, email templates, and a general asset pipeline.
- Hot reload of configuration in backend services, ruled out by the rendered-config ADR.

---

## Decision Drivers

* The theme has the shape of install configuration: one per install, set at deploy, read at start.
* Every mechanism should be one the platform already has, or the standard Kubernetes one, before it is a new one.
* Validation must be a guarantee the chart carries, not a step a consumer remembers to add.
* One source of truth per value, or a mechanical check where two copies are unavoidable.
* Hades owns the contract and its validator; kora calls them and never duplicates a rule.
* The decisions are numbered as the PRD numbers them, so the builder's documents can cite them directly.

---

## Considered Options

Each decision below records its own options. The two overall shapes that were weighed and set aside:

- **A `Theme` custom resource reconciled by the operator, applied live.** The precedent for a custom resource in kora is an entity with many instances, a lifecycle independent of releases, and a controller keeping something in sync. A theme is a singleton read once at start. A live update contradicts the PRD's acceptance criterion that nothing at runtime changes the theme, it needs runtime asset transport that only the image pull at pod start provides for free, and triggering a rollout by patching the chart-owned frontend Deployment breaks the install ADR's ownership rule. Applying a custom resource also always succeeds, so "fail the deploy" has no meaning for it unless the consumer writes a custom health check.
- **Baking the theme into a per-client image.** Rejected by the PRD.

---

## Decisions

### Decision 1: The theme contract

**Options considered:**

1. **A YAML document grouped by token, in a bundle directory with its assets, with Hades owning the schema, every rule value and the validator.** JSON remains valid input as a subset of YAML.
2. **JSON only.** Equivalent in power, worse to author and review by hand, which is what the deployment repository invites.
3. **Separate value-only and value-with-assets packagings.** Two shapes to validate and document for one artefact whose assets are already optional.
4. **A kora-owned validator driven by a Hades schema.** Any rule the schema cannot express would be written twice.

**Chosen: Option 1.**

**Terms.** The PRD's glossary defines the theme as the whole artefact. This ADR calls its YAML file the document and the directory holding the document and its assets the bundle.

**The document.** `theme.yaml`, with these top-level keys and no others. An unknown top-level key fails validation.

- `contractVersion`: the contract version, `MAJOR.MINOR`, the theme was written against, as `CONTRACT_VERSION` in the package and `contractVersion` in the manifest carry it. The only required key, written as a quoted string because YAML reads an unquoted `1.0` as the number 1 and `1.10` as 1.1; the validator refuses a number. The instance accepts a theme whose contract major equals its own and refuses every other (Decision 3).
- `productName`: a single verbatim string within the manifest's product-name bounds.
- `tokens`: a map keyed by manifest token name, each holding `dark` and `light` values. Either mode may be omitted and falls through to the kora default. The radius steps are tokens like any other.
- `slots`: a map keyed by manifest slot name. Image slots hold a path per mode; font slots hold one path. Paths are relative to the bundle root.

The two font family tokens from SD-1 of the versioning ADR are settable like any token, for a theme that names a system or pre-installed family without supplying a file. A font slot sets its family token to the family name the instance assigns, so a theme that sets a font slot and the same slot's family token is malformed.

An entry under `tokens` or `slots` whose key the running manifest does not know is ignored and logged. A known key whose value does not match the manifest's format is malformed. A referenced asset that is absent from the bundle, exceeds its slot's size, or violates its format rules is a malformed value. Malformed values fail validation; nothing else does.

**The bundle.** A directory with `theme.yaml` at its root and `assets/` beside it. Assets are optional, and a theme that sets no slot has an empty or absent `assets/`. There is one packaging. How the directory is transported is Decision 2.

**Ownership.** Hades owns the theme surface manifest, a JSON Schema generated from it, every rule value including the asset formats, size limits, SVG constraints and product-name bounds, and the validator that applies them. Formats and bounds are the manifest fields decided in subsection 3 of the versioning ADR and are cited, not restated. kora calls the validator and never carries a rule of its own.

**Publication.** The schema is `theme.schema.json`, generated by the same make target as the manifest, checked in beside it with the same drift check, shipped in the package and attached to every Hades release, as the delivery ADR records. The validator ships in the package. Together they are the validation rules the PRD requires to be published per exact version.

**Document-format changes.** The document's top-level keys and their shapes are part of the contract: an unknown top-level key fails validation, so adding one breaks every older instance in the major. A change to the schema's top-level properties is classified major, and the versioning ADR's classification table carries the row.

### Decision 2: Ingestion and asset delivery

**Options considered, vehicle for the document:**

1. **Chart values.** The theme is part of the values the consumer already applies. Validated structurally at render time by the values schema, reviewed in the GitOps repository, and a change is a `helm upgrade`, which is a redeploy.
2. **A `Theme` custom resource.** See Considered Options above.
3. **A ConfigMap authored outside the chart.** Loses values as the configuration surface.

**Options considered, asset transport:**

1. **An OCI image pulled by the kubelet.** Uses the existing image pull, pull secrets, mirrors and air-gap arrangements. Content-addressed, immutable, signable, recorded in git as a digest, promoted between environments by copying one value.
2. **A volume populated by the deploy pipeline.** Needs write access into cluster storage that differs per consumer, a ReadWriteMany storage class for more than one replica, a separate rollout trigger because a storage change is invisible to Kubernetes, and leaves what is deployed unknowable from git.
3. **Fetched at startup from a bucket or URL.** A network dependency, credentials and a trust decision in instance startup, for nothing the registry does not already give.

**Options considered, how the image reaches the pod:**

1. **An image volume.** The kubelet mounts the image read-only at a path. No copy, no base image, the bundle is pure files built from scratch.
2. **An init container copying into an `emptyDir`.** Works on 1.34, at the cost of a busybox base in every bundle and a copy container in the pod.
3. **A chart toggle between the two.** Two paths to test, and the install ADR allows a switch only where the consumer has a substitute.

**Chosen: values for the document, an OCI image for the bundle, an image volume into the pod.**

- **Values.** `theme.bundle.image` holds a digest-pinned reference to the bundle image. `theme.bundle.pullSecrets` defaults to `global.imagePullSecrets`. `theme.document` holds the full `theme.yaml` as a string, supplied from the repository file with `--set-file`, Argo CD `fileParameters` or the Helmfile equivalent, so the committed file is the single thing a consumer edits. The file must live in the repository the values come from.
- **The bundle image.** Built from scratch with `theme.yaml` at the image root, `/theme.yaml`, and the assets under `/assets/`. An image volume exposes the image's filesystem root at its mount path, so the bundle root is what the pod sees at `/etc/orbit/theme`, with no `subPath`. That layout and pullability with the configured secrets are the only producer-facing requirements. kora publishes no build tool: wrapping a directory as an image is a two-line Dockerfile every pipeline already knows, and the in-deploy validation of Decision 3 is the guarantee however the image was built. The deployment guide shows the Dockerfile. Where the directory's contents come from, a repository beside the document or an artefact store the pipeline assembles from, is the consumer's business, which is how the PRD's constraint that assets are never forced into git is honoured.
- **The mount.** The frontend pod mounts the bundle image read-only at `/etc/orbit/theme` as an image volume. The digest in the pod template rolls the pods on change; a tag that is re-pushed rolls nothing, so the reference is a digest and the values schema rejects a tag.
- **The floor.** Theming requires nodes on Kubernetes 1.36 or later with containerd 2.1 or CRI-O 1.31 or later. The chart floor stays 1.34; with no theme configured nothing renders that needs more. The prerequisites contract gains the row, and the chart does not check the version at render time: under `helm template` with no cluster Helm substitutes its own default version, so a themed values file would render differently with and without cluster access, against the install ADR, and the API server's version says nothing about the nodes. Below the floor the pod is never created or never starts, the rollout stalls on an event that names the volume, and the previous pods keep serving.
- **Rollout.** The document is rendered into the frontend's ConfigMap, and the product name extracted from it into the backend config modules of Decision 5, so the existing checksum annotations roll every pod that reads either. A theme change is therefore a values change and a rolling update, and a rollback is a values revert. The runtime-created pods receive their values from orbit-agents.
- **What the deployment repository shows.** The uncompressed document, reviewable line by line, and the digest of the bundle built from it. The consequence of one packaging stands: every theme is an image, including one that sets colours only.

### Decision 3: Validation and runtime exposure

**Options considered, where validation runs:**

1. **An init container in the frontend pod, using the frontend image with a different command.** The image already contains Node and the Hades package, so no new image is published. A failure keeps the pod from becoming ready, the rollout stalls, the old pods keep serving, and an Argo CD Application goes Degraded once the Deployment's progress deadline passes. The message is in the init container's log and the pod event. It holds however the bundle was built.
2. **A consumer-run validation step before deploy.** Nothing guarantees it runs.
3. **The operator, reading the bundle from the registry.** The validator is TypeScript and Hades-owned; a Go validator would duplicate every rule.

**Options considered, what the frontend loads:**

1. **One code path.** The validator and the loader are one Hades module. The init container runs it in check mode; the frontend runs it in load mode at startup, reading the same mount.
2. **The validator emits a resolved document for the frontend to read.** A second writable volume, a second format and a second place to drift.

**Options considered, runtime exposure of the Hades version, deferred here by subsection 2 of the versioning ADR:**

1. **Nothing beyond the startup log line.**
2. **The version on the existing health route.**
3. **The version and the manifest on a read-only endpoint.** The manifest is internal, which rules this out.

**Chosen: in-deploy validation, one code path, same contract major only, the versions on the health route.**

- **Compatibility.** A theme passes when the major of its `contractVersion` equals the major of the instance's `CONTRACT_VERSION`. A newer contract minor's tokens are unknown to an older instance and are ignored and logged, per Decision 1; an older theme on a newer instance leaves the new tokens at default. A theme of any other contract major fails validation. The Hades package version plays no part: a Hades major that leaves the contract alone invalidates no theme.
- **Bundle matches document.** The init container also receives the chart-supplied document, mounted as a file from the frontend's ConfigMap, and fails validation unless the two parse to the same document, because the chart-supplied copy has passed through templating and a ConfigMap, which can change line endings and trailing whitespace. A committed document whose bundle was not rebuilt, or the reverse, fails the deploy with a message that says so.
- **The module is pure.** It takes the document and a function that reads an asset by bundle path, and returns the validated theme and the report. It never touches the filesystem, so the delivery ADR's smoke build compiles it and Decision 6's decorator runs it in a browser. The init container entry point and the frontend's server module read `/etc/orbit/theme` and hand the bytes over.
- **What validation reports.** Besides faults, the validator reports every ignored entry and every identity value the theme leaves to fall through to the kora default, as the PRD requires. The init container prints the report; the instance logs it at startup.
- **Health.** `GET /api/health` gains `hadesVersion` and `contractVersion` fields. No manifest endpoint.
- **Nothing validates in Go.** The backend reads a value the chart rendered from a document the frontend's init container has accepted, and never calls the validator.

### Decision 4: Startup loading, first HTML, assets and identity slots

**Options considered:**

1. **Load once at process start; inline the overrides in the document head; declare theme fonts with `font-display: swap` and preload links; serve assets from a route under a versioned prefix; render identity slots through agnostic Hades components fed by the effective theme.**
2. **Load per request.** Filesystem reads on the request path for a value fixed for the life of the process.
3. **A linked stylesheet for the overrides.** One extra render-blocking request per page and an ordering dependency on the token sheet.
4. **An unversioned asset route with short caching.** Stale logos after a redeploy for the cache lifetime, and fonts that cannot be cached long.
5. **Choosing the logo variant on the client after hydration.** A flash on every load.

**Chosen: Option 1.**

- **Loading.** A server-only module reads `/etc/orbit/theme` on first import, runs the Hades loader, computes everything derived, and holds the result for the life of the process. A bundle that fails to load stops the process, which cannot happen after the init container passed and is the right outcome if it was bypassed.
- **First HTML.** Hades provides a generator that turns a validated document into one CSS string: an `html:root:not([data-theme='light'])` block for dark and an `html[data-theme='light']` block for light, carrying only the tokens the theme sets, plus an `@font-face` rule per theme font with the font slot tokens set to the family names the instance assigns. Both selectors are more specific than the token sheet's `:root` and `[data-theme='light']`, so order against Next's stylesheet links does not matter, and the dark block is scoped so a token set in dark only falls through to the kora light default in light. The root layout renders the string in a `<style>` element in the head, and the kora fonts stay bundled as the fallback. The generator takes an asset resolver that maps a bundle path to a URL, so the same function serves the app and Decision 6's preview.
- **Fonts.** `font-display: swap`, with a preload link per theme font in the head so the brand font is almost always resolved before first paint. When it is not, text paints in the kora font and swaps once; that single transition is accepted over blocking text on a font fetch.
- **Budget.** The PRD's first-paint budget is set here as no additional blocking request and at most 16 KiB added to the HTML document. The inline style for a theme that sets every token and the preload links fit inside it, and both are checked in a build, not measured in a cluster.
- **Assets.** A route handler at `/theme/<hash>/assets/<path>` serves files from the mount with `Cache-Control: public, max-age=31536000, immutable`, a `Content-Type` fixed from the file extension against the manifest's formats, never sniffed, `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`, so an SVG opened by direct navigation cannot run script even if the validator's no-script rule were bypassed. The session proxy exempts the `/theme/` prefix, because the login page needs the fonts and logos before any session exists. The hash is computed by the loader from the bundle's contents, so the URL changes on every bundle change and no cache can serve a stale brand asset, and the loader stays independent of whatever mounted the bundle. The handler serves only the paths the loaded theme references.
- **Identity slots.** Hades gains agnostic slot components for the logo, the favicon and the product name. They take the values and sources they render and know nothing of kora. The logo component renders both mode variants as `<img>` elements with one hidden by CSS keyed on `data-theme`, so the user's theme choice is followed with no flash and no client logic, and the SVG stays out of the DOM. The app feeds them from the effective theme, which is the kora default overlaid by the loaded theme. The kora logo becomes an SVG asset of the kora default theme at app level, and the three call sites in the login card and the sidebar use the slot component. The favicon is set through the root layout's metadata with a `media` query for `prefers-color-scheme` when both modes are supplied, because browsers choose favicons by system scheme, not by the page's `data-theme`. The favicon therefore follows the system colour scheme, not the in-app toggle: a user who overrides the system scheme in the app sees the logo and product name in their chosen mode and the tab icon in the system's. That is accepted over client logic that rewrites the icon link after hydration, which would reintroduce a flash and a first paint on the system scheme anyway.

### Decision 5: Product name

**Options considered, how the backend learns the name:**

1. **The chart extracts it from the document and renders it into the backend config modules.** `theme.document` is parsed at render time with `fromYaml`, and `productName` lands in a shared config module included by every service that produces user-facing text, read through the typed loader like any other setting. Decision 3's bundle-matches-document check guarantees the backend and the frontend render the same name. The chart fails the render when the document is not valid YAML, and when `productName` is missing or outside the manifest's bounds, so the theme's one value that reaches backend workloads is checked before any workload rolls. The product-name bounds are not restated in the chart: `make hades-manifest` also copies them from the manifest into a rule file inside the chart, `files/hades-product-name.json`, because a chart can read only its own files, and `make hades-check` fails when the copy drifts. The chart applies it with `fail` and renders the name trimmed, as the validator does. The rest of the theme is validated by Decision 3's init container; this is the one rule that must run earlier, because it is the one value the backend consumes.
2. **The operator extracts it from the bundle in the registry and writes an operator-owned ConfigMap.** One more reconcile path, a layer pull in the operator, and either a hot read of configuration in the services, which the rendered-config ADR rules out, or a race between the operator's write and the pods' start.
3. **Mount the bundle in every speaking pod.** Carries the whole theme into pods that need one string.
4. **A chart value typed separately from the document.** Two sources to keep in step by hand.

**Options considered, which strings change:**

1. **Everything a client's people or their agents read.** UI copy, the page title, the consent screen, guardrail messages, the capabilities brief that becomes the agent's system prompt, MCP tool descriptions and the MCP server name the runners register, the egress gateway's denial messages, and the sandbox base image description. Identifiers such as the `orbit.io` API group and image names, log lines, Kubernetes messages and operator preflight output keep the kora name.
2. **UI copy only.** A guardrail message that says "kora paused this run" inside a white-labelled product is the leak the PRD exists to prevent.

**Options considered, the hardcoded-string check:**

1. **A lint rule on each side, in the tier that enforces everything else there.** Both rules search the whole words `kora` and `orbit`, case-insensitively, not adjacent to `-`, `_`, `.` or `/`, so identifier-shaped literals such as `orbit.io` and `kora-base` pass and prose fails. An ESLint rule in the `local/` set applies that to string and JSX text literals under the app and components trees, with mock-data paths excluded until they go. A ruleguard rule applies it to string literals, including struct tags, where the MCP tool descriptions live, skips arguments to logger calls, and is scoped to the speaking packages, `internal/agents`, `internal/orbitmcp`, `internal/egressgw` and `internal/platform/sandboximages`, through the path-scoped exclusions in `.golangci.yml`, where any residual exception lives.
2. **One script scanning both trees.** The frontend loses the editor-time enforcement every other rule there has.
3. **Review only.** Ruled out by the PRD.

**Chosen: chart extraction, everything a client's people or agents read, a lint rule on each side.**

- The shared module is `internal/platform/brand` with one field, `productName`, defaulting to `kora`. Each speaking service includes it in its rendered config, and the checksum annotation rolls the service on change. orbit-agents passes the value to the session pods it creates through the typed env producer it already uses.
- The frontend exposes the name through a server-side accessor on the loaded theme for metadata and server components, and a provider from the root layout for client components. There is no build-time environment variable, because the value is runtime.
- The backend strings that change are enumerated above, which answers the PRD's first open question.
- Nothing orders the backend pods after the frontend's init container, which is why the chart checks `productName` itself: a value outside the manifest's bounds, including one with control characters or markup that would reach the agent's system prompt and MCP descriptions, fails `helm template` and `helm upgrade` before any pod changes, so the PRD's guarantee that an invalid theme never reaches a running instance holds for backend text as well. The chart applies the rule from the manifest, so Go never carries one, and the chart's copy is regenerated whenever the manifest changes.

### Decision 6: Template exposure

**Options considered:**

1. **Publish the built Storybook as a release asset.** The static build of every story under `Pages/` is zipped and attached to each kora GitHub Release, the same way Hades ships as a release and with nothing hosted by kora. It is a preview aid for whoever produces a theme, not a client deliverable, which is what the PRD's non-goal excludes. A preview decorator accepts a theme over `postMessage` and injects the same CSS Decision 4's generator produces. The pages stay in the app, which contract boundary rule 4 allows, because a page change is never a contract change.
2. **Publish the page components as a package.** The pages are client components tied to Next's app router, its navigation mocks and the Tier 2 domain components; decoupling every page from Next and keeping it decoupled on every change is a permanent tax.
3. **A running instance in a preview mode.** Needs a runtime theme override, which the PRD rules out, plus authentication and data.
4. **Conclude not viable.** The P1 goes unmet.

**Chosen: Option 1.**

- **The asset.** `kora-storybook-vX.Y.Z.zip`, built on every kora release tag from `build-storybook`, named with the kora version because the pages are kora's. Inside it, `preview.json` records the kora version, the Hades version and contract version the release pins, and the embedding protocol version, so a downloaded archive is self-describing.
- **Discovery.** A consumer that needs the latest pages for contract major N walks kora releases newest first, reads the pinned contract version each publishes in its compatibility statement, and takes the first whose contract major matches. No new mapping exists. When kora moves to the next contract major, the last release on the previous major is the final preview for that major, which is correct: themes for that major target the instances running those kora versions.
- **The protocol.** The consumer sends the iframe a message carrying the theme document and a map from bundle asset paths to URLs the consumer serves. The decorator validates the document with the Hades validator at the pinned version, builds the CSS through the generator with the supplied resolver, and injects it. The message shape carries its own version, independent of kora's and Hades', bumped only when the shape changes. The protocol is documented with the asset, because kora does not know its consumers.
- **Which pages.** Everything under the `Pages/` Storybook prefix. The ADR names the prefix rather than a list, so a page added to Storybook is exposed by being there.

---

## Decision

**Chosen Option:** Decision 1 Option 1; Decision 2 values, OCI image, image volume; Decision 3 in-deploy validation, one code path, same contract major only, versions on health; Decision 4 Option 1; Decision 5 chart extraction, everything a client's people or agents read, a lint rule on each side; Decision 6 Option 1.

### Rationale

The theme is install configuration, so it travels as values, and its assets are too large for any API object, so they travel as an image, which is the one large-blob path Kubernetes provides for free. Validation inside the deploy makes "a malformed theme fails the deploy" a property of the chart rather than of a consumer's diligence, and placing it in an init container that uses the frontend image means no new artefact and the exact Hades version the instance runs. Passing the committed document into the chart lets the chart hand the product name to the backend through the path every service already walks, and the equality check in the init container turns the only way the two copies can diverge, a stale bundle, into a failed deploy with a message. Image volumes remove a copy step and a base image at the cost of a prerequisite that applies only to deployments that use a theme, recorded in the prerequisites contract. Exposing pages as a published Storybook build costs a release step and a decorator, keeps the pages where they live, and lets a consumer render a theme on the real product without kora hosting anything.

---

## Consequences

### Positive

- A theme change is a values change, reviewed in the deployment repository, rolled out by the standard rolling update and rolled back by a revert.
- An invalid theme never serves a page: the frontend rollout stalls on the init container and the previous pods keep serving.
- One Hades module validates, loads and generates CSS, used by the init container, the instance and the preview. No rule exists twice.
- No operator change, no custom resource, no new published tool.
- The frontend's contract is a directory at a fixed path, so the vehicle that mounts it could change without touching the loader.

### Negative

- Theming requires Kubernetes 1.36 nodes and a recent container runtime, ahead of the chart's own floor, and the chart does not check it; a cluster below it finds out at rollout.
- A product name outside bounds is served in backend text until the values change is reverted, because the backend reads the chart-rendered name and nothing orders it after the init container.
- Every theme is an image, so a colours-only theme still needs a registry push.
- The document lives in two places, the repository file and the bundle, held equal by the init container rather than by construction.
- Validation feedback arrives at rollout rather than in a pull request check, unless a consumer runs the validator in their own pipeline.

### Neutral

- The kora logo and mark leave Hades for the app's default theme, and three call sites change to the slot component.
- A Storybook archive is attached to every kora release.
- The versioning ADR's deferred question on runtime exposure is answered: the version on the health route, no manifest endpoint.

---

## Implementation

### Hades

- `src/theme/` module: the validator, the loader, the CSS generator with its asset resolver, and the SVG, size and product-name checks driven by manifest data, all pure. `theme.schema.json` generated from the manifest and checked in beside it; the delivery ADR ships it in the package and attaches it to each release.
- The agnostic slot components for logo, favicon and product name, and the move of `KoraLogo` to the app, are the pre-1.0 changes SD-1 of the versioning ADR schedules; this ADR adds nothing to them.

### Frontend

- The server-only theme module, the inline style and preload links in the root layout, the `/theme/<hash>/assets/<path>` route handler, metadata icons from the theme, the health field, the brand provider, and the `local/no-product-name-literal` ESLint rule.
- A `validate-theme` entry point in the image for the init container.
- The Storybook decorator and `preview.json`, and the `build-storybook` step in the release workflow with the archive attached to the release.

### Backend

- `internal/platform/brand` config module and the `noProductNameLiteral` ruleguard rule.
- The strings enumerated in Decision 5 read the configured name. orbit-agents passes it to session pods.

### Chart

- `theme.bundle.image`, `theme.bundle.pullSecrets`, `theme.document`, with schema entries. The image volume, the init container, the document rendered into the frontend ConfigMap and mounted as a file, the brand module in each speaking service's config, and the render-time parse check on the document.
- The deployment guide: the theming prerequisite row, the bundle layout, the reference Dockerfile, and the values example. The compatibility statement gains the prerequisite.

### Migration Path

None. No deployment carries a theme today, and an install without `theme.bundle.image` renders exactly as before.

### Timeline

The implementation lands as separate pull requests, each stacked on the previous, in this order: the versioning ADR's pre-1.0 changes and classifier, the delivery ADR's rename and release workflow, then the Hades, frontend, backend and chart changes above. `hades/v1.0.0` is cut before the first theme, because a document records a Hades version whose major the instance must match.

---

## Validation

### Success Metrics

- A deployment with a theme shows no kora identity on any page in either mode, and no kora name in any string a client's people or agents read.
- A malformed theme stalls the frontend rollout with a message naming the fault; a stale bundle does the same; the previous pods keep serving throughout.
- A theme that sets colours only, with no assets, deploys and renders.
- The CI rules fail on a new product-name literal in either tree.
- The Storybook archive, embedded with a theme sent over the protocol, renders every `Pages/` story under that theme.

### Monitoring

- The instance logs its own Hades version, the version the theme records, the theme hash, every ignored token and every identity value left to fall through, at startup.
- The init container's log is the diagnostic for a failed rollout.

---

## Related Decisions

- [Design System Semantic Versioning](2026-10-11-design-system-semantic-versioning.md) - the manifest, the version and the contract boundary rules this ADR builds on; its deferred runtime-exposure question is answered in Decision 3.
- [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) - how a producer obtains the Hades version a theme targets.
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md) - charts as the product, GitOps cleanliness, the operator's scope and the prerequisites contract.
- [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) - the path the product name takes into the backend.
- [Hades Frontend Design System](2026-03-25-terra-frontend-design-system.md) - the token sheet the overrides target.

---

## References

- [White-labelling PRD](../prd/white-labelling.md)
- [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [Kubernetes image volumes](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
- [Helm `--set-file`](https://helm.sh/docs/helm/helm_upgrade/) and [Argo CD Helm `fileParameters`](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/)

---

## Notes

The decision numbers match the list in the PRD's Solution Overview, so a document in another repository cites "decision 6 of the white-labelling theme ADR" and finds it here.

**Last Updated:** 2026-10-12
