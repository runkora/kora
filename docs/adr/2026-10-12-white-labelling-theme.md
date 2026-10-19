# White-labelling Theme

**Status:** Accepted, amended 2026-10-19
**Date:** 2026-10-12
**Deciders:** Qj Stout-Spykers
**Technical Story:** The six architectural decisions the [White-labelling PRD](../prd/white-labelling.md) defers to this ADR, numbered as that PRD numbers them so other documents can cite each one. Depends on [Design System Semantic Versioning](2026-10-11-design-system-semantic-versioning.md) for the manifest and the version, and on [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) for how a producer obtains Hades.

> **Amendment, 2026-10-15.** The theme no longer records a Hades contract version. Hades is released with kora under one version, so a theme records the kora release it was written against, `koraVersion`, and an instance accepts a theme of the same major. The versioning ADR carries the reasoning. The `contractVersion` key, the `contractVersion` and `hadesVersion` health fields and the contract-minor handling in this ADR are replaced accordingly; nothing else in the decisions changes.

> **Amendment, 2026-10-19.** A theme no longer carries its assets. Chart consumers are platform teams and themes are written by trusted administrators, so the bundle image, its registry, Dockerfile, digest, second copy of the document and the Kubernetes 1.36 node floor were more machinery than a logo, a favicon and a couple of webfonts justify. The document is now the whole theme: its `slots` hold `https://` URLs and the browser loads the assets directly from them. Decisions 2 to 4 are replaced accordingly, and Decisions 1 and 5 lose their references to the bundle. The cost is recorded under Consequences: an air-gapped install must host its assets internally, and asset content is not checked at deploy.

---

## Context

A kora deployment serves one client organisation and should look and read like that client's product. The PRD defines the theme as a deployment artefact: it conforms to a contract defined by the Hades design system as released with the kora major it was written for, it is supplied with the rest of an instance's configuration, it changes only on redeploy, it may be partial, and a malformed value fails the deploy while a missing value falls back to the kora default. The PRD is written without reference to any producer of themes, and this ADR keeps that stance: nothing here depends on how a theme is authored.

### Background

The platform the theme has to fit:

- The published Helm charts are the product ([Install and Distribution Model](2026-09-04-install-and-distribution-model.md)). Chart values are the configuration surface, consumers apply them from their own GitOps repository with Helm or Argo CD, and the operator reconciles tenant lifecycle only. Charts render identically with no cluster access and produce no perpetual diff.
- Each backend service receives its per-install settings as a chart-rendered file at `/etc/orbit/config/{module}.yaml`, merged by the typed loader, with a checksum annotation that rolls the pods on change and no hot reload ([Rendered Service Config Files](2026-10-06-rendered-service-config-files.md)).
- The frontend is a standalone Next.js image. It receives configuration as environment variables from a ConfigMap, loads Inter and Geist Mono at build time through `next/font`, sets its favicon from a build-time file, and has no way to serve a file that arrives after the build. The Hades token sheet declares raw tokens on `:root` for dark and overrides them under `[data-theme='light']`, and `next-themes` sets `data-theme` before hydration.
- Kubernetes API objects are bounded by etcd at roughly one megabyte, so fonts and logos cannot travel inside a ConfigMap, a Secret or a custom resource.
- Storybook renders every top-level page as a story under the `Pages/` prefix, from the real page components with mock data. It is a development tool, and nothing publishes it.

### Problem Statement

Decide how a theme is expressed, how it reaches a deployment and its pods, where its brand assets are loaded from, where it is validated so an invalid theme fails before an instance serves, how the frontend renders it without a flash of the kora default, how the backend learns the product name, how new hardcoded product-name literals are kept out, and whether kora provides real pages to third parties for previewing a theme.

### Goals

- One theme per deployment, applied only by redeploy, attributable through the deployment configuration's history.
- Brand assets never forced into a git repository.
- A malformed theme fails the deploy with a message that names the fault, before any page serves. A missing value never fails.
- No flash of kora colours or identity on first paint. A theme font may swap in once.
- Nothing kora-specific in Hades, and nothing in the instance that assumes a particular producer.
- Nothing a consumer must set up beyond their deployment configuration and somewhere to host their brand assets.

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

- **A `Theme` custom resource reconciled by the operator, applied live.** The precedent for a custom resource in kora is an entity with many instances, a lifecycle independent of releases, and a controller keeping something in sync. A theme is a singleton read once at start. A live update contradicts the PRD's acceptance criterion that nothing at runtime changes the theme, it needs a runtime path for the document to reach the instance, and triggering a rollout by patching the chart-owned frontend Deployment breaks the install ADR's ownership rule. Applying a custom resource also always succeeds, so "fail the deploy" has no meaning for it unless the consumer writes a custom health check.
- **Baking the theme into a per-client image.** Rejected by the PRD.

---

## Decisions

### Decision 1: The theme contract

**Options considered:**

1. **A YAML document grouped by token, with assets referenced by URL, with Hades owning the schema, every rule value and the validator.** JSON remains valid input as a subset of YAML.
2. **JSON only.** Equivalent in power, worse to author and review by hand, which is what the deployment repository invites.
3. **Separate value-only and value-with-assets packagings.** Two shapes to validate and document for one artefact whose slots are already optional.
4. **A kora-owned validator driven by a Hades schema.** Any rule the schema cannot express would be written twice.

**Chosen: Option 1.**

**Terms.** The PRD's glossary defines the theme as the whole artefact. This ADR calls its YAML file the document.

**The document.** `theme.yaml`, with these top-level keys and no others. An unknown top-level key fails validation.

- `koraVersion`: the kora release, `MAJOR.MINOR.PATCH`, the theme was written against. The only required key, written as a quoted string; the validator refuses a number, which is what YAML makes of an unquoted `1.10`. Every kora release carries the manifest and schema for its own version as release assets, so a producer selects a release and gets the surface that release accepts. The instance accepts a theme whose major equals its own and refuses every other (Decision 3). A theme is not promised to keep working across a major.
- `productName`: a single verbatim string within the manifest's product-name bounds.
- `tokens`: a map keyed by manifest token name, each holding `dark` and `light` values. Either mode may be omitted and falls through to the kora default. The radius steps are tokens like any other.
- `slots`: a map keyed by manifest slot name. Image slots hold an `https://` URL per mode; font slots hold one `https://` URL.

The two font family tokens from SD-1 of the versioning ADR are settable like any token, for a theme that names a system or pre-installed family without supplying a file. A font slot sets its family token to the family name the instance assigns, so a theme that sets a font slot and the same slot's family token is malformed.

An entry under `tokens` or `slots` whose key the running manifest does not know is ignored and logged. A known key whose value does not match the manifest's format is malformed. A slot value that is not an `https://` URL, carries credentials, or whose path does not end in one of its slot's formats is a malformed value. Malformed values fail validation; nothing else does. The validator never fetches a URL, so it does not check that the asset exists, how large it is or what it contains.

**Assets.** A theme that sets no slot references no assets. The assets are files the theme's author hosts anywhere the browser can reach over HTTPS, and the document points at them. There is one shape. How the document is transported is Decision 2, and what the browser does with the URLs is Decision 4.

**Ownership.** Hades owns the theme surface manifest, a JSON Schema generated from it, every rule value including the slot URL formats and product-name bounds, and the validator that applies them. Formats and bounds are the manifest fields decided in subsection 3 of the versioning ADR and are cited, not restated. kora calls the validator and never carries a rule of its own. Themes are authored by trusted instance administrators, and the validator cannot see what a slot URL serves, so the versioning ADR's size limits and SVG constraints no longer apply: nothing is scanned. What contains an SVG is that it is rendered as an `<img>`, which never runs script. Accepting themes from untrusted authors would need content checks and a new decision.

**Publication.** The schema is `theme.schema.json`, generated by the same make target as the manifest, checked in beside it with the same drift check, shipped in the package and attached to every Hades release, as the delivery ADR records. The validator ships in the package. Together they are the validation rules the PRD requires to be published per exact version.

**Document-format changes.** The document's top-level keys and their shapes are part of the contract: an unknown top-level key fails validation, so adding one breaks every older instance in the major.

### Decision 2: Ingestion and asset delivery

**Options considered, vehicle for the document:**

1. **Chart values.** The theme is part of the values the consumer already applies. Validated structurally at render time by the values schema, reviewed in the GitOps repository, and a change is a `helm upgrade`, which is a redeploy.
2. **A `Theme` custom resource.** See Considered Options above.
3. **A ConfigMap authored outside the chart.** Loses values as the configuration surface.

**Options considered, asset transport:**

1. **URLs in the document, loaded by the browser.** The assets live wherever the author hosts them. Nothing in the cluster stores, mounts or serves them, and the deployment repository shows exactly which files the theme uses.
2. **An OCI image mounted as an image volume.** Content-addressed and immutable, using the existing registry arrangements, and the instance can validate the bytes. It needs a registry, a Dockerfile, a digest the schema pins, a second copy of the document inside the image held equal to the first, pull secrets and nodes on Kubernetes 1.36 with a recent container runtime. For a logo, a favicon and a couple of webfonts, set by trusted administrators on platform-operated charts, that is disproportionate.
3. **A volume populated by the deploy pipeline.** Needs write access into cluster storage that differs per consumer, a ReadWriteMany storage class for more than one replica, a separate rollout trigger because a storage change is invisible to Kubernetes, and leaves what is deployed unknowable from git.
4. **Fetched by the instance at startup.** A network dependency and a trust decision in instance startup, for files the browser can fetch itself.

**Chosen: values for the document, URLs for the assets.**

- **Values.** `theme.document` holds the full `theme.yaml` as a string, supplied from the repository file with `--set-file`, Argo CD `fileParameters` or the Helmfile equivalent, so the committed file is the single thing a consumer edits. The file must live in the repository the values come from. It is the only theme value.
- **Assets.** The document's `slots` hold `https://` URLs for the logo, the favicon and the fonts. The author hosts the files, and the PRD's constraint that assets are never forced into git is honoured because the repository holds only their addresses.
- **The mount.** The chart renders the document into the frontend's ConfigMap, mounted as `/etc/orbit/theme/theme.yaml`. Both the init container of Decision 3 and the frontend read that file.
- **No node prerequisite.** The chart floor stays 1.34 with or without a theme, and nothing renders that needs more.
- **Rollout.** The product name extracted from the document into the backend config modules of Decision 5, and the document in the ConfigMap, carry checksum annotations that roll every pod that reads either. A theme change is therefore a values change and a rolling update, and a rollback is a values revert. The runtime-created pods receive their values from orbit-agents.
- **What the deployment repository shows.** The uncompressed document, reviewable line by line, with every asset address in it.
- **Accepted trade-offs.** An air-gapped install cannot use custom assets unless it hosts them internally. Asset content is not checked at deploy. Platform teams operate these charts and trust their theme authors, so both are accepted.

### Decision 3: Validation and runtime exposure

**Options considered, where validation runs:**

1. **An init container in the frontend pod, using the frontend image with a different command.** The image already contains Node and the Hades package, so no new image is published. A failure keeps the pod from becoming ready, the rollout stalls, the old pods keep serving, and an Argo CD Application goes Degraded once the Deployment's progress deadline passes. The message is in the init container's log and the pod event.
2. **A consumer-run validation step before deploy.** Nothing guarantees it runs.
3. **The operator, validating the document.** The validator is TypeScript and Hades-owned; a Go validator would duplicate every rule.

**Options considered, what the frontend loads:**

1. **One code path.** The validator and the loader are one Hades module. The init container runs it in check mode; the frontend runs it in load mode at startup, reading the same file.
2. **The validator emits a resolved document for the frontend to read.** A second writable volume, a second format and a second place to drift.

**Options considered, runtime exposure of the Hades version, deferred here by subsection 2 of the versioning ADR:**

1. **Nothing beyond the startup log line.**
2. **The version on the existing health route.**
3. **The version and the manifest on a read-only endpoint.** The manifest is internal, which rules this out.

**Chosen: in-deploy validation, one code path, same major only, the version on the health route.**

- **Compatibility.** A theme passes when the major of its `koraVersion` equals the major of the instance's own version, the kora release it is running. A newer release's tokens are unknown to an older instance and are ignored and logged, per Decision 1; an older theme on a newer instance leaves the new tokens at default. A theme of any other major fails validation. Within a major the instance validates the theme against its own manifest, so a theme the release no longer fits fails with the fault named; nothing checks that a Hades break is released as a major (the versioning ADR). Below `1.0.0` every release has major `0`, so nothing is rejected on version and nothing is promised. A build that is not from a release tag is a development build with no kora version: the theme's `koraVersion` is checked for shape, the comparison is skipped, and the log and the report say so.
- **What is validated.** The document, as mounted from the frontend's ConfigMap, and nothing else. The init container does not fetch the slot URLs: a deploy never depends on an asset host being reachable, and an asset that is missing or unreachable shows as a broken image or the fallback font, which nothing at deploy catches.
- **The module is pure.** It takes the document and returns the validated theme and the report. It touches neither the filesystem nor the network, so the delivery ADR's smoke build compiles it and Decision 6's decorator runs it in a browser. The init container entry point and the frontend's server module read `/etc/orbit/theme/theme.yaml` and hand the text over.
- **What validation reports.** Besides faults, the validator reports every ignored entry and every identity value the theme leaves to fall through to the kora default, as the PRD requires. The init container prints the report; the instance logs it at startup.
- **Health.** `GET /api/health` reports the instance's version. No manifest endpoint.
- **Nothing validates in Go.** The backend reads a value the chart rendered from a document the frontend's init container has accepted, and never calls the validator.

### Decision 4: Startup loading, first HTML, assets and identity slots

**Options considered:**

1. **Load once at process start; inline the overrides in the document head; declare theme fonts with `font-display: swap` and preload links; load assets from the URLs in the document; render identity slots through agnostic Hades components fed by the effective theme.**
2. **Load per request.** Filesystem reads on the request path for a value fixed for the life of the process.
3. **A linked stylesheet for the overrides.** One extra render-blocking request per page and an ordering dependency on the token sheet.
4. **Choosing the logo variant on the client after hydration.** A flash on every load.

**Chosen: Option 1.**

- **Loading.** A server-only module reads `/etc/orbit/theme/theme.yaml` on first import, runs the Hades loader, computes everything derived, and holds the result for the life of the process. A document that fails to load stops the process, which cannot happen after the init container passed and is the right outcome if it was bypassed.
- **First HTML.** Hades provides a generator that turns a validated document into one CSS string: an `html:root:not([data-theme='light'])` block for dark and an `html[data-theme='light']` block for light, carrying only the tokens the theme sets, plus an `@font-face` rule per theme font with the font slot tokens set to the family names the instance assigns. Both selectors are more specific than the token sheet's `:root` and `[data-theme='light']`, so order against Next's stylesheet links does not matter, and the dark block is scoped so a token set in dark only falls through to the kora light default in light. The root layout renders the string in a `<style>` element in the head, and the kora fonts stay bundled as the fallback. The font URLs are used as the document gives them, so the same function serves the app and Decision 6's preview.
- **Fonts.** `font-display: swap`, with a preload link per theme font in the head, marked `crossorigin` because a font fetch is always a CORS request, so the brand font is almost always resolved before first paint. The font host must therefore send `Access-Control-Allow-Origin`; a font without it does not load and the kora font stays. When it is not, text paints in the kora font and swaps once; that single transition is accepted over blocking text on a font fetch.
- **Budget.** The PRD's first-paint budget is set here as no additional blocking request and at most 16 KiB added to the HTML document. The inline style for a theme that sets every token and the preload links fit inside it, and both are checked in a build, not measured in a cluster.
- **Assets.** The instance serves none. The browser loads the logo, the favicon and the fonts straight from the document's URLs, so there is no asset route, no content hash and no `/theme/` exemption in the session proxy, and the login page needs no session to show them. Logos and favicons render as `<img>`, which never runs script, even for an SVG. Caching belongs to the host: a file changed at an unchanged URL is served stale from browser and CDN caches, so a theme change that replaces a file should change its URL. A rolling update has no asset mismatch between old and new pods, because the URLs travel in the document. The instance sets no Content-Security-Policy today, so there is no `img-src` or `font-src` to extend; an app-wide policy is a separate decision, and when it lands it must allow the hosts of the loaded theme's slot URLs.
- **Identity slots.** Hades gains agnostic slot components for the logo, the favicon and the product name. They take the values and sources they render and know nothing of kora. The logo component renders both mode variants as `<img>` elements with one hidden by CSS keyed on `data-theme`, so the user's theme choice is followed with no flash and no client logic, and the SVG stays out of the DOM. The app feeds them from the effective theme, which is the kora default overlaid by the loaded theme. The kora logo becomes an SVG asset of the kora default theme at app level, and the three call sites in the login card and the sidebar use the slot component. The favicon is set through the root layout's metadata with a `media` query for `prefers-color-scheme` when both modes are supplied, because browsers choose favicons by system scheme, not by the page's `data-theme`. The favicon therefore follows the system colour scheme, not the in-app toggle: a user who overrides the system scheme in the app sees the logo and product name in their chosen mode and the tab icon in the system's. That is accepted over client logic that rewrites the icon link after hydration, which would reintroduce a flash and a first paint on the system scheme anyway.

### Decision 5: Product name

**Options considered, how the backend learns the name:**

1. **The chart extracts it from the document and renders it into the backend config modules.** `theme.document` is parsed at render time with `fromYaml`, and `productName` lands in a shared config module included by every service that produces user-facing text, read through the typed loader like any other setting. The backend and the frontend read the same `theme.document`, so they render the same name. The chart fails the render when the document is not valid YAML, and when `productName` is blank, set to nothing or outside the manifest's bounds, while a document that omits it keeps the default, so the theme's one value that reaches backend workloads is checked before any workload rolls. The product-name bounds are not restated in the chart: `make hades-manifest` also copies them from the manifest into a rule file inside the chart, `files/hades-product-name.json`, because a chart can read only its own files, and `make hades-check` fails when the copy drifts. The chart applies it with `fail` and renders the name trimmed, as the validator does. The rest of the theme is validated by Decision 3's init container; this is the one rule that must run earlier, because it is the one value the backend consumes.
2. **The operator extracts it from the document and writes an operator-owned ConfigMap.** One more reconcile path in the operator, and either a hot read of configuration in the services, which the rendered-config ADR rules out, or a race between the operator's write and the pods' start.
3. **Mount the document in every speaking pod.** Carries the whole theme into pods that need one string.
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

1. **Publish the built Storybook as a release asset.** Considered and rejected for now. The static build of every story under `Pages/` would be attached to each kora GitHub Release, with a preview decorator that accepts a theme over `postMessage`. It is too complex at this stage for the one purpose of previewing templates: it adds a release step, a decorator, a versioned message protocol and a published artefact that kora would have to keep secure and supported, and it would lower the security rating ahead of release. It remains the most likely route if the need returns, because the pages could stay in the app, which the theme contract allows, since a page change is never a change to what a theme can set. The option is open to revisiting.
2. **Publish the page components as a package.** The pages are client components tied to Next's app router, its navigation mocks and the Tier 2 domain components; decoupling every page from Next and keeping it decoupled on every change is a permanent tax.
3. **A running instance in a preview mode.** Needs a runtime theme override, which the PRD rules out, plus authentication and data.
4. **Conclude not viable at this stage.** The P1 goes unmet, and kora provides no preview templates to third parties.

**Chosen: Option 4.**

- kora does not publish, package or host page templates for third parties. A producer of themes previews them on their own compositions of Hades components, against the contract.
- The decision is revisited if a consumer needs real-page previews badly enough to justify the cost. Option 1 is the starting point, and it would amend this decision rather than supersede it.
- Nothing in the theme contract, the Hades package or the instance depends on the outcome, so reversing it later changes no contract.

---

## Decision

**Chosen Option:** Decision 1 Option 1; Decision 2 values for the document, URLs for the assets; Decision 3 in-deploy validation, one code path, same major only, version on health; Decision 4 Option 1; Decision 5 chart extraction, everything a client's people or agents read, a lint rule on each side; Decision 6 not viable at this stage.

### Rationale

The theme is install configuration, so it travels as values, and a theme written by a trusted administrator for a platform-operated chart needs nothing heavier than the addresses of its assets, so they are URLs the browser loads directly. Validation inside the deploy makes "a malformed theme fails the deploy" a property of the chart rather than of a consumer's diligence, and placing it in an init container that uses the frontend image means no new artefact and the exact Hades version the instance runs. Passing the committed document into the chart lets the chart hand the product name to the backend through the path every service already walks, and because the document is the only copy, nothing can diverge. The price is that the instance cannot see the assets, so it checks their addresses and not their content, and an air-gapped install hosts them itself. Page templates are not provided to third parties, because a published Storybook would add a release step, a protocol and a published artefact for the sole purpose of theme previews, at a cost to the security rating before release; the option stays open to revisit.

---

## Consequences

### Positive

- A theme change is a values change, reviewed in the deployment repository, rolled out by the standard rolling update and rolled back by a revert.
- An invalid theme never serves a page: the frontend rollout stalls on the init container and the previous pods keep serving.
- One Hades module validates, loads and generates CSS, used by the init container and the instance. No rule exists twice.
- No operator change, no custom resource, no new published tool, and no registry, image build, digest or node prerequisite for a theme.
- The document is the only copy of the theme, so no second copy has to be held equal to it.
- The frontend's contract is a directory at a fixed path, so the vehicle that mounts it could change without touching the loader.

### Negative

- A product name outside bounds is served in backend text until the values change is reverted, because the backend reads the chart-rendered name and nothing orders it after the init container.
- Validation feedback arrives at rollout rather than in a pull request check, unless a consumer runs the validator in their own pipeline.
- An air-gapped install cannot use custom assets unless it hosts them on a host its users' browsers can reach.
- Asset content is not checked at deploy: the validator checks each URL's form, not that it resolves, how large the file is or what it contains. A missing logo shows as a broken image and a font without CORS headers does not load.
- The browser of every visitor, including an unauthenticated one on the login page, contacts the asset host.
- A file replaced at an unchanged URL is served stale from caches until they expire.
- Theme producers get no kora-supplied preview of the real pages, so they preview on their own compositions of Hades components and see the pages only once deployed.

### Neutral

- The kora logo and mark leave Hades for the app's default theme, and three call sites change to the slot component.
- The versioning ADR's deferred question on runtime exposure is answered: the version on the health route, no manifest endpoint.
- No Content-Security-Policy is introduced. When an app-wide policy is decided, the hosts of the slot URLs go into its `img-src` and `font-src`.

---

## Implementation

### Hades

- `src/theme/` module: the validator, the loader, the CSS generator, and the slot URL and product-name checks driven by manifest data, all pure. `theme.schema.json` generated from the manifest and checked in beside it; the delivery ADR ships it in the package and attaches it to each release.
- The agnostic slot components for logo, favicon and product name, and the move of `KoraLogo` to the app, are the pre-contract changes SD-1 of the versioning ADR schedules; this ADR adds nothing to them.

### Frontend

- The server-only theme module, the inline style and preload links in the root layout, metadata icons from the theme, the health field, the brand provider, and the `local/no-product-name-literal` ESLint rule.
- A `validate-theme` entry point in the image for the init container.

### Backend

- `internal/platform/brand` config module and the `noProductNameLiteral` ruleguard rule.
- The strings enumerated in Decision 5 read the configured name. orbit-agents passes it to session pods.

### Chart

- `theme.document`, with its schema entry. The init container, the document rendered into the frontend ConfigMap and mounted as a file, the brand module in each speaking service's config, and the render-time parse check on the document.
- The deployment guide: the document, its slot URLs, the CORS requirement for fonts and the values example.

### Migration Path

None. No deployment carries a theme today, and an install without `theme.document` renders exactly as before.

### Timeline

The implementation lands as separate pull requests, each stacked on the previous, in this order: the versioning ADR's pre-contract changes and manifest, the delivery ADR's rename and release jobs, then the Hades, frontend, backend and chart changes above. No Hades release is cut first: a theme records a kora release, and the instance compares majors against its own.

---

## Validation

### Success Metrics

- A deployment with a theme shows no kora identity on any page in either mode, and no kora name in any string a client's people or agents read.
- A malformed theme stalls the frontend rollout with a message naming the fault, and the previous pods keep serving throughout.
- A theme that sets colours only, with no slots, deploys and renders.
- The CI rules fail on a new product-name literal in either tree.

### Monitoring

- The instance logs its own version, the `koraVersion` the theme records, the theme hash, every ignored token and every identity value left to fall through, at startup.
- The init container's log is the diagnostic for a failed rollout.

---

## Related Decisions

- [Design System Semantic Versioning](2026-10-11-design-system-semantic-versioning.md) - the manifest and the version this ADR builds on; its deferred runtime-exposure question is answered in Decision 3.
- [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) - how a producer obtains the Hades release a theme targets.
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md) - charts as the product, GitOps cleanliness and the operator's scope.
- [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) - the path the product name takes into the backend.
- [Hades Frontend Design System](2026-03-25-terra-frontend-design-system.md) - the token sheet the overrides target.

---

## References

- [White-labelling PRD](../prd/white-labelling.md)
- [Hades Design System PRD](../prd/design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it)
- [Helm `--set-file`](https://helm.sh/docs/helm/helm_upgrade/) and [Argo CD Helm `fileParameters`](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/)

---

## Notes

The decision numbers match the list in the PRD's Solution Overview, so a document in another repository cites "decision 6 of the white-labelling theme ADR" and finds it here.

**Last Updated:** 2026-10-19
