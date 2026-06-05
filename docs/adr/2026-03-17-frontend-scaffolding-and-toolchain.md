# Frontend Scaffolding and Toolchain

**Status:** Accepted
**Date:** 2026-03-17
**Deciders:** Engineering Team
**Technical Story:** Establishing the package manager, version enforcement strategy, Next.js routing model, code quality tooling, source directory structure, and BFF security boundary for the Orbit operator dashboard frontend introduced in [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) and [language-and-framework-selection](2026-02-16-language-and-framework-selection.md).

---

## Context

[monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) and [language-and-framework-selection](2026-02-16-language-and-framework-selection.md) established that the Orbit operator dashboard will be built with Next.js, React, and TypeScript, hosted in a `frontend/` subdirectory of the monorepo. Those decisions deliberately deferred lower-level toolchain questions:

- Which package manager manages frontend dependencies?
- How do we prevent version drift between developer machines and CI?
- Which Next.js routing model — Pages Router or App Router — should the frontend use?
- How are code formatting and linting enforced across the frontend?
- How should `frontend/src/` be structured to make the server/client boundary visible and enforceable?
- How is the Next.js BFF layer secured so that server-side credentials cannot leak into client bundles?

These are not cosmetic choices. The package manager affects lockfile stability, disk usage in CI, and monorepo performance. Version enforcement directly affects the reproducibility of builds. The source directory structure determines whether the BFF boundary is visible at a glance or requires developer knowledge to infer. The BFF boundary affects the security posture of the control plane's operator interface.

### Background

Orbit is deployed on an internal platform (not Vercel or another managed Next.js host). The frontend is a single Next.js application that serves as both the operator dashboard UI and the Backend-for-Frontend (BFF) layer that translates browser session cookies into API key calls against the Go control plane. This means the Next.js server process has access to credentials that must never reach the browser.

[monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) established that the monorepo uses separate dependency management for each component: `go.mod` for the Go backend, `package.json` for the frontend. There is no root-level `package.json`. This means the chosen package manager is scoped entirely to `frontend/` — it does not need to support a polyglot monorepo root, which simplifies the version enforcement problem significantly. If a workspace-protocol dependency (e.g. `workspace:*` for an internal design system package) is introduced in future, a `pnpm-workspace.yaml` at the repo root would be required and this claim would need revision; that change is expected to be covered by the design system ADR.

### Problem Statement

We need to decide:

1. **Package manager**: Which tool manages `frontend/` dependencies, and how is it enforced?
2. **Version pinning**: How do we guarantee that all developers and CI use the exact same package manager version?
3. **Next.js router strategy**: Pages Router or App Router?
4. **Code quality tooling**: How is code formatting and linting enforced?
5. **BFF boundary enforcement**: How do we prevent server-side modules (credentials, API clients) from being imported into client components?
6. **Source directory structure**: How should `frontend/src/` be laid out to make server/client zone membership structurally visible?

### Goals

- Reproducible installs across developer machines and CI — identical lockfile behaviour regardless of who runs the install
- Exact package manager version pinned in one location, automatically propagated
- A hard, statically-enforced boundary between server-only code and client-side bundles
- Source directory layout that makes security zone membership visible in import paths without requiring tooling
- Minimal onboarding friction — a new engineer should need only Node.js pre-installed

### Non-Goals

- Choosing a frontend test runner (deferred — no tests yet)
- Defining the CI/CD pipeline (separate concern)
- Selecting a CSS framework, component library, or design system — covered by the design system work (separate ADR)
- Supporting multiple frontend applications

---

## Decision Drivers

* **Reproducibility**: Lockfile and package manager version must be deterministic across environments
* **Security**: Server-side credentials (API keys, config) must be statically prevented from reaching client bundles
* **Monorepo compatibility**: Package manager must work cleanly alongside the Go backend in a single repository without polluting root-level files; scoped to `frontend/` only per [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md)'s separate dependency management model
* **Onboarding simplicity**: Node is the only prerequisite; the package manager installs itself
* **Developer experience**: Fast installs, clear error messages when wrong tooling is used, consistent code formatting enforced automatically
* **Long-term stability**: Toolchain choices should be mature enough to survive years of Next.js upgrades
* **Forward compatibility**: Router model must align with Next.js's active development direction, not a maintenance-mode branch

---

## Considered Options

### Decision 1: Package Manager

#### Option 1A: npm

**Description:** The default Node.js package manager, no installation step required.

**Pros:**
- Zero setup — ships with Node.js
- Universally familiar
- Lockfile (`package-lock.json`) is the industry default

**Cons:**
- Slower installs than pnpm or Bun due to non-content-addressable cache
- No hard-link deduplication — each project gets a full copy of every dependency
- Phantom dependencies: packages can import transitive dependencies not listed in `package.json`, hiding real dependency relationships
- No native enforcement mechanism to prevent yarn/pnpm being used accidentally
- In a monorepo context, workspace support is weaker than pnpm's

#### Option 1B: Yarn (Berry / v4)

**Description:** Yarn's modern "Berry" release with Plug'n'Play (PnP) or `node_modules` mode.

**Pros:**
- Fast installs, good workspace support
- Zero-installs mode (check lockfile into git, skip install step)
- Plugin system for extensibility

**Cons:**
- PnP mode has known compatibility issues with some Next.js internals and native add-ons
- `node_modules` mode loses most of Yarn Berry's advantages
- Two Yarn versions (Classic v1 / Berry v4) cause persistent confusion — `yarn` on a developer machine may be v1 while the project uses v4
- Weaker adoption trend in the Node ecosystem compared to pnpm
- Corepack support exists but is less battle-tested than for pnpm

#### Option 1C: pnpm

**Description:** A content-addressable package manager that hard-links packages from a global store, producing a strict `node_modules` layout that mirrors `package.json` declarations exactly.

**Pros:**
- Strict `node_modules`: phantom dependencies are impossible — packages can only import what they declare
- Content-addressable global store with hard links: multiple projects share the same bytes on disk
- Fast installs (second only to Bun for Node-based managers)
- First-class Corepack support — version pinning via the `packageManager` field in `package.json` works reliably
- Excellent monorepo workspace support
- Human-readable `pnpm-lock.yaml` (YAML, diffable)
- Widely adopted; well-supported by Next.js and the broader ecosystem

**Cons:**
- Not pre-installed on developer machines — requires Corepack activation or explicit install
- Strict layout occasionally causes friction with poorly-written packages that rely on phantom dependencies (increasingly rare)

#### Option 1D: Bun

**Description:** A JavaScript runtime and package manager written in Zig, designed to replace both Node.js and npm.

**Pros:**
- Fastest installs of any option — typically 5–10× faster than npm
- Unified toolchain: runtime, package manager, bundler, and test runner in one binary
- Lockfile is compact

**Cons:**
- Binary lockfile (`bun.lockb`) — not human-readable, makes PR reviews of lockfile changes opaque
- Node.js API compatibility is high but not complete; some Next.js internals and native add-ons have edge cases
- No Corepack support — version pinning requires a different mechanism (`BUNTAG` env var, Dockerfile pinning, or wrapper scripts)
- Younger ecosystem maturity; fewer resources for debugging subtle issues
- Switching the runtime (not just the package manager) is a larger architectural change with more surface area for unexpected behaviour

---

### Decision 2: Version Enforcement Strategy

#### Option 2A: `packageManager` field + Corepack only

**Description:** Pin the exact version in `package.json`'s `packageManager` field (including sha512 hash). Rely on Corepack to enforce it.

**Pros:**
- Single source of truth for the version
- Corepack auto-downloads the correct version — no manual install step after `corepack enable`
- Corepack rejects mismatched versions with a clear error

**Cons:**
- Does not block `npm install` or `yarn install` if Corepack is not active
- Corepack is labelled "experimental" in Node.js (though it ships in all recent LTS releases and is stable in practice)

#### Option 2B: `only-allow` preinstall guard only

**Description:** Add `"preinstall": "npx only-allow pnpm"` to `package.json`. Any `npm install` or `yarn` invocation fails at the preinstall hook.

**Pros:**
- Simple, no Node.js features required
- Catches wrong package manager regardless of Corepack state

**Cons:**
- Does not enforce the pnpm _version_ — a developer on pnpm 9.x passes the guard but may produce a different lockfile
- `npx only-allow` requires a network round-trip on first run if not cached

#### Option 2C: `packageManager` field + Corepack + `only-allow` (two-layer enforcement)

**Description:** Both mechanisms active simultaneously.

- `packageManager` field: pins exact version + sha512; Corepack auto-downloads correct pnpm version
- `preinstall` hook: rejects npm/yarn invocations before any packages are installed
- `PNPM_VERSION` in `Makefile`: single variable that drives `corepack install -g pnpm@$(PNPM_VERSION)` in the `frontend-install` target, keeping the Makefile version in sync

**Pros:**
- Layered defence: Corepack catches wrong version, `only-allow` catches wrong manager
- New engineers running `make setup` get the correct pnpm installed automatically via Corepack
- One place (`PNPM_VERSION` in `Makefile`, mirrored by `packageManager` in `package.json`) to update when upgrading pnpm

**Cons:**
- Two places to update when bumping pnpm version (`Makefile` variable and `package.json` field + hash)
- Slightly more moving parts than either mechanism alone

---

### Decision 3: BFF Boundary Enforcement

#### Option 3A: Convention only

**Description:** Rely on code review and naming conventions to prevent server-side modules from being imported into client components.

**Pros:**
- Zero implementation cost

**Cons:**
- Fails silently — a misplaced import is only caught at review time or at runtime
- Next.js will bundle server-side code into the client if not explicitly prevented, exposing credentials

#### Option 3B: `server-only` package guard

**Description:** Import `server-only` at the top of every server-side module. Next.js's bundler will throw a build-time error if a module marked `server-only` is ever imported into a client component.

**Pros:**
- Build-time enforcement — the error is caught before any code ships
- Explicit: the intention is documented in the file itself
- Zero runtime cost
- Works correctly with Next.js App Router's module graph

**Cons:**
- Requires discipline: developers must add the import to new server-side files
- Does not prevent server-side logic from being duplicated in client code (only prevents imports)

#### Option 3C: Separate package/service for BFF logic

**Description:** Extract the BFF layer into a separate Node.js service, removing the possibility of shared imports entirely.

**Pros:**
- Hard architectural boundary — impossible to accidentally import across services

**Cons:**
- Significantly more infrastructure (separate process, deployment, service discovery)
- Negates the simplicity benefit of Next.js's unified server+client model
- Premature for the current project scale

### Decision 4: Source Directory Structure

The layout of `frontend/src/` determines whether the BFF boundary is visible at a glance or requires developer knowledge to infer.

#### Option 4A: Flat `src/` with no enforced convention

**Description:** Components, server logic, shared types, and route handlers all live at the same directory level, differentiated only by file suffix or developer discipline.

**Pros:**
- Minimal scaffolding to set up

**Cons:**
- No visual or structural signal about which files are server-safe vs client-safe
- Import paths give no indication of whether crossing the BFF boundary is occurring
- Difficult to enforce linting rules based on directory membership

#### Option 4B: Separate top-level `src/components/` and `src/hooks/`

**Description:** All reusable UI components live in `src/components/`, hooks in `src/hooks/`, server logic in `src/server/`, and pages in `src/app/`.

**Pros:**
- Familiar to teams coming from Create React App or older Next.js patterns
- Clear separation between component library and page logic

**Cons:**
- Components are decoupled from the pages that use them, making dead-code detection harder
- Does not encode the server/client boundary structurally — `src/components/` and `src/server/` are at the same level with no enforced relationship between them
- Encourages over-abstraction of one-off components into a shared library before that reuse is warranted

#### Option 4C: Three-zone layout with co-located page components

**Description:** `src/` is divided into three structurally distinct zones with clear ownership rules:

```
src/
├── server/        # Trusted kernel — server-only modules (credentials, API client)
│   ├── client.ts  #   → must carry `import "server-only"`
│   └── config.ts  #   → must carry `import "server-only"`
├── app/           # Next.js App Router root
│   ├── api/       #   → BFF route handlers (imports from src/server/, never imported by client)
│   │   └── */route.ts
│   ├── _components/ # Co-located UI components (underscore = not a route segment)
│   ├── layout.tsx
│   ├── page.tsx
│   └── globals.css
└── shared/        # Safe zone — type-only, no runtime code, importable from anywhere
    └── api.ts
```

**Pros:**
- The directory name encodes the security zone: `server/` = never in browser, `shared/` = safe anywhere, `app/` = rendered UI
- An import of `@/server/*` inside a page or component is visually wrong before any tooling runs
- Co-located `_components/` (Next.js underscore convention) keeps page-specific UI adjacent to its page without polluting the router
- The `shared/` zone makes the type contract between client and server explicit and inspectable
- Directly lintable: the zone boundaries map cleanly onto ESLint glob patterns, making zone violations enforceable as build-time errors rather than code-review concerns

**Cons:**
- `_components/` for truly shared components (used by multiple pages) requires a separate home — `src/components/` would be added when that need arises
- Requires onboarding explanation; developers familiar with `src/components/` will look there first

---

### Decision 5: Next.js Router Strategy

[language-and-framework-selection](2026-02-16-language-and-framework-selection.md) selected Next.js as the frontend framework but did not evaluate the routing model. Next.js has two distinct routing systems: the original Pages Router and the App Router introduced in Next.js 13 (stable from v13.4, the default from v14+).

#### Option 5A: Pages Router

**Description:** The original Next.js file-based routing model. Pages live under `src/pages/`; BFF endpoints live under `src/pages/api/`.

**Pros:**
- Mature and battle-tested — extensive documentation, community resources, and library compatibility
- Familiar to developers with Next.js experience prior to v13
- Simple mental model: every file in `pages/` is a route

**Cons:**
- All components are client components by default — server-side data fetching requires separate `getServerSideProps` / `getStaticProps` functions rather than being co-located with the component
- No React Server Component support — misses the security and performance benefits of server-default rendering
- BFF endpoints live under `pages/api/` alongside UI pages, mixing routing concerns in a single directory tree
- Pages Router is in maintenance mode — Next.js's active development is entirely on App Router; new features will not be backported

#### Option 5B: App Router

**Description:** The modern Next.js routing model. Pages and layouts under `src/app/` are React Server Components by default; client components opt in with `"use client"`. BFF endpoints are Route Handlers and live under `src/app/api/`.

**Pros:**
- Server Components by default: credential access and data fetching happen server-side unless a component explicitly opts into client rendering — the framework default is the secure default
- Co-located data fetching: fetch calls live inside the component, not in separate props functions, reducing the surface area for accidental client exposure
- Native alignment with the `server-only` guard (Decision 2) and the three-zone directory layout (Decision 4) — the component model itself reinforces the server/client split
- Route Handlers under `app/api/` integrate naturally with the three-zone directory layout (Decision 4)
- Streaming, Suspense, and partial rendering built in
- Actively developed — new Next.js features ship to App Router first; Pages Router receives maintenance only

**Cons:**
- Steeper learning curve for developers coming from the Pages Router model
- Some older libraries lag in App Router support (improving rapidly as the ecosystem converges)
- `"use client"` placement requires deliberate thought about component tree boundaries

---

### Decision 6: Code Quality Tooling

[language-and-framework-selection](2026-02-16-language-and-framework-selection.md) mentioned "Use ESLint and Prettier for code quality" as an implementation note without formal evaluation. This section provides that evaluation.

#### Option 6A: ESLint only

**Description:** Use `eslint` with `eslint-config-next` for linting. No dedicated formatter — formatting is left to editor defaults.

**Pros:**
- Minimal dependencies; `eslint-config-next` is maintained by the Next.js team and covers React- and Next.js-specific rules out of the box

**Cons:**
- Formatting style is not enforced — pull requests accumulate noise when different editors produce different whitespace, quote style, or trailing-comma conventions
- No single source of truth for formatting decisions; disagreements become a recurring cost in code review

#### Option 6B: ESLint + Prettier

**Description:** ESLint handles code quality and correctness rules; Prettier handles all formatting. `eslint-config-prettier` disables any ESLint rules that conflict with Prettier's output.

**Pros:**
- Formatting is deterministic and automated — style debates are eliminated from code review
- Prettier supports TypeScript, JSON, CSS, and Markdown, covering the entire `frontend/` directory with one formatter
- Industry-standard pairing for TypeScript/Next.js projects; AI assistants are well-trained on this combination (consistent with [language-and-framework-selection](2026-02-16-language-and-framework-selection.md)'s reasoning about toolchain familiarity and training corpus)
- `eslint-config-prettier` is a well-maintained bridge that prevents the two tools from conflicting

**Cons:**
- Two tools to configure and maintain
- Requires `prettier` and `eslint-config-prettier` as additional dev dependencies

#### Option 6C: Biome

**Description:** A single Rust-based tool that replaces both ESLint and Prettier, providing linting and formatting in one binary.

**Pros:**
- Extremely fast (Rust-based); single tool instead of two
- Growing Next.js and TypeScript support

**Cons:**
- Does not yet provide a full replacement for `eslint-config-next` — Next.js-specific lint rules are missing
- Smaller community and documentation than ESLint + Prettier
- Would diverge from [language-and-framework-selection](2026-02-16-language-and-framework-selection.md)'s stated guidance without a strong migration rationale

---

## Decision

**Chosen Options:**
- **Package Manager**: Option 1C — pnpm
- **Version Enforcement**: Option 2C — `packageManager` field + Corepack + `only-allow` (two-layer)
- **BFF Boundary**: Option 3B — `server-only` package guard
- **Source Directory Structure**: Option 4C — Three-zone layout with co-located page components
- **Next.js Router**: Option 5B — App Router
- **Code Quality Tooling**: Option 6B — ESLint + Prettier

### Rationale

**pnpm over npm**: npm's phantom dependency model is a footgun at scale. pnpm's strict `node_modules` layout means dependency relationships are explicit and auditable — important for a security-focused control plane where supply chain clarity matters. The install speed and disk efficiency are secondary benefits.

**pnpm over Yarn Berry**: Yarn Berry's PnP mode introduces compatibility uncertainty with Next.js internals and native modules. The two-version confusion (Classic vs Berry) adds onboarding friction. pnpm's trajectory in the ecosystem is stronger and its Corepack integration is more mature.

**pnpm over Bun**: Bun's binary lockfile makes dependency audits opaque in pull requests — a meaningful concern for a security-focused project. Bun's Node.js compatibility gaps introduce risk in Next.js's server-side rendering paths. The performance advantage is real but not decisive for a control plane dashboard where install time is not a bottleneck. Orbit's deployment target is an internal platform (not a managed host), which removes any hosting-side preference. If Bun's compatibility story matures significantly, this decision should be revisited.

**Two-layer enforcement**: Neither mechanism alone is sufficient. `only-allow` catches the wrong package manager but not the wrong version. The `packageManager` field + Corepack catches the wrong version but does not block `npm install` when Corepack is inactive. Together they cover both failure modes. The `PNPM_VERSION` Makefile variable is the canonical version reference that drives `corepack install -g pnpm@$(PNPM_VERSION)`, making `make setup` self-bootstrapping with only Node as a prerequisite.

**`server-only` guard**: The operator dashboard BFF layer translates browser session cookies into API keys that call the Go control plane. Accidental client-side bundling of these credentials is a critical security failure. Build-time enforcement via `server-only` catches this class of error before it ships. Every module under `src/server/` and every API route handler under `src/app/api/` carries this import. A separate BFF service would provide a harder boundary but at infrastructure cost that is not justified at current scale.

**Three-zone directory layout**: The directory structure is the first line of defence — it makes the BFF boundary _visible_ before any tooling runs. When a developer sees `import { orbitClient } from "@/server/client"` inside a page component, the path itself signals the violation. The `server-only` package import is the build-time enforcement layer; the directory layout is the human-readable signal layer. Both are necessary: `server-only` catches what humans miss, and the directory structure guides humans before they make the mistake. The `shared/` zone makes the type contract between server and client explicit and importable from either side without risk.

**App Router over Pages Router**: The App Router's server-first component model aligns naturally with this project's security requirements. React Server Components are server-only by default — credentials and data fetching happen on the server unless a component explicitly opts into client rendering with `"use client"`. This is the secure default: the developer must take an affirmative action to move code to the client, rather than having to remember to keep it out. The three-zone directory layout and the `server-only` guard both complement App Router's component model; Pages Router would require more manual discipline to achieve the same boundary. Ongoing Next.js development is entirely focused on App Router; Pages Router receives maintenance only. The BFF surface is expressed as Route Handlers (`src/app/api/*/route.ts`) rather than Pages Router API routes (`src/pages/api/`), which integrates correctly with the three-zone layout in Decision 4.

**ESLint + Prettier**: Prettier eliminates style debates and makes formatting a non-issue in code review. `eslint-config-next` (maintained by the Next.js team) provides the correctness rules for React and Next.js patterns; Prettier handles all formatting uniformly across TypeScript, JSON, CSS, and Markdown. `eslint-config-prettier` bridges the two tools by disabling any ESLint formatting rules that would conflict with Prettier's output. Biome is not yet a complete replacement for `eslint-config-next` and would diverge from [language-and-framework-selection](2026-02-16-language-and-framework-selection.md)'s stated guidance without a clear benefit at this stage.

---

## Consequences

### Positive

- Reproducible installs: `make frontend-install` produces byte-identical results in CI (the Makefile target passes `--frozen-lockfile` when the `CI` environment variable is set) and installs freely on developer machines — one target, consistent behaviour across environments
- Phantom dependencies are structurally prevented by pnpm's strict layout
- New engineers run `make setup` (or `make bootstrap`) and receive the correct pnpm version automatically via Corepack — Node is the only prerequisite
- Server-side credential leakage to client bundles is caught at build time, not at code review
- `pnpm-lock.yaml` is a human-readable YAML file — lockfile diffs in pull requests are reviewable
- The three-zone directory layout makes security zone membership visible in import paths — `@/server/*` in a client file is wrong on sight, before linting or building
- App Router's server-first default means components are server-rendered unless explicitly opted into client rendering — the secure default requires an affirmative action (`"use client"`) to cross the boundary, not an affirmative action to stay server-side
- Prettier enforces uniform formatting across all TypeScript, JSON, CSS, and Markdown files automatically on commit — no style decisions accumulate in code review

### Negative

- pnpm version must be updated in two places when upgrading: `PNPM_VERSION` in `Makefile` and the `packageManager` field + sha512 hash in `package.json`
- Omitting `import "server-only"` from a new server-side module is a silent failure — the build succeeds and the unguarded module becomes importable from client components with no compile-time error. This is caught at lint time by the `local/require-server-only` rule, which targets `src/server/**` and `src/app/api/**/route.ts`
- Developers unfamiliar with pnpm's strict layout may encounter errors with poorly-written packages that rely on phantom dependencies (a shrinking problem as the ecosystem matures)
- The three-zone layout does not accommodate shared reusable components (used across multiple pages) — a `src/components/` directory will be added when that need arises
- App Router's `"use client"` directive requires deliberate thought about component tree boundaries — wrapping too high moves more code to the client; wrapping too low may cause prop-drilling through server/client boundaries
- Prettier adds two additional dev dependencies (`prettier`, `eslint-config-prettier`) that must be kept in sync when upgrading

### Neutral

- `frontend-clean` removes `.next/` and `node_modules/` — run before `frontend-install` when debugging dependency issues
- Corepack is labelled "experimental" in Node.js documentation, but ships in all current LTS releases and is stable in practice
- `pnpm-lock.yaml` replaces `package-lock.json` in version control; `package-lock.json` must not be regenerated

---

## Implementation

### Bootstrap Flow

A new engineer with only Node.js installed follows:

```
git clone <repo>
make bootstrap        # chains: env setup dev-up migrate-up
                      # setup → install-tools → frontend-install → hooks
                      # frontend-install runs:
                      #   corepack install -g pnpm@$(PNPM_VERSION)
                      #   pnpm install  (Makefile passes --frozen-lockfile when CI=true)
                      # hooks runs:
                      #   git config core.hooksPath .githooks
```

No manual pnpm installation step is required.

### Managing Dependencies

`make frontend-install` is the single install command for both local development and CI. It detects the environment via the standard `CI` environment variable:

- **Locally** (`CI` unset): runs `pnpm install` — installs and updates `pnpm-lock.yaml` as needed
- **In CI** (`CI=true`): runs `pnpm install --frozen-lockfile` — fails if `pnpm-lock.yaml` is out of sync with `package.json`, catching uncommitted lockfile changes

This gives local/CI parity with a single target: developers use `make frontend-install` for all environment management; CI uses the same target and gets strict lockfile enforcement automatically.

When adding or removing packages, run `pnpm` directly inside `frontend/` then reinstall via make:

```sh
cd frontend
pnpm add <package>        # add a runtime dependency
pnpm add -D <package>     # add a dev dependency
pnpm remove <package>     # remove a dependency
make frontend-install     # reconcile node_modules after package.json changes
```

Commit the updated `pnpm-lock.yaml` alongside `package.json`.

### Source Directory Structure

```
frontend/src/
├── server/            # Trusted kernel — server-only, credentials live here
│   ├── client.ts      #   Typed HTTP client for the Go control plane
│   └── config.ts      #   Environment variable validation and config object
├── app/               # Next.js App Router
│   ├── api/           #   BFF route handlers — only place that imports from src/server/
│   │   └── example/health/route.ts
│   ├── _components/   #   Co-located UI components (underscore = excluded from routing)
│   │   └── HealthStatus.tsx
│   ├── layout.tsx     #   Root layout (fonts, global providers)
│   ├── page.tsx       #   Root page
│   └── globals.css    #   Global stylesheet (CSS framework configured by design system ADR)
└── shared/            #   Safe zone — TypeScript interfaces only, no runtime code
    └── api.ts         #   Go backend response shapes (imported by both sides)
```

**Zone rules:**
- `src/server/*` → may only be imported by `src/app/api/*` route handlers
- `src/app/api/*` → BFF surface; imports `src/server/*`, exports sanitised JSON responses
- `src/app/**` (non-api) → client-facing; must never import from `src/server/*`
- `src/shared/*` → safe from anywhere; type-only, zero runtime imports

### BFF Boundary Convention

Every module under `frontend/src/server/` and every route handler under `frontend/src/app/api/` must include the following import immediately after any JSDoc block, before all other imports:

```typescript
import "server-only";
```

This import is enforced at two levels:

1. **Commit time** (`make check` → `frontend-check`): the `local/require-server-only` custom ESLint rule reports an error if the import is absent from any file matching `src/server/**` or `src/app/api/**/route.ts`. The pre-commit hook rejects the commit before it lands.
2. **Build time**: Next.js's bundler throws if a module guarded by `"server-only"` is imported into a client bundle. This acts as a second safety net if the lint check is skipped.

The directory layout provides a human-readable layer on top of this tooling enforcement: an import of `@/server/*` appearing in any file outside `src/app/api/` is visually wrong before any tooling runs.

### Code Quality Tooling

The frontend uses ESLint for code correctness and Prettier for formatting, configured in `eslint.config.mjs` (ESLint flat config format).

**ESLint** is run via `make frontend-lint` (`pnpm lint`) and **Prettier** via `make frontend-fmt`. `make frontend-check` runs both as a composite target and is what `make check` (and the pre-commit hook) calls. The ESLint config composes three layers:

1. `eslint-config-next/core-web-vitals` + `eslint-config-next/typescript` — React, Next.js, and TypeScript correctness rules maintained by the Next.js team
2. `local/require-server-only` — errors on any file in `src/server/**` or `src/app/api/**/route.ts` that is missing `import "server-only"`
3. `local/require-use-client-for-hooks` — errors on any file in `src/app/**` (excluding Route Handlers) that imports React client hooks (`useState`, `useEffect`, `useRef`, etc.) without a `"use client"` directive

The two `local/*` rules are inline custom rules defined in `eslint.config.mjs`; no separate plugin package is needed.

**Prettier** is enforced on every commit via the pre-commit hook (`.githooks/pre-commit` runs `make check → frontend-check`). `make frontend-fmt` runs `prettier --write` locally (fixing files in place) and `prettier --check` in CI (failing the build if files are not already formatted) — the same `$(if $(CI),--check,--write)` pattern used by `make frontend-install`. A commit that introduces formatting violations is rejected before it lands. Editor integration (format on save) is strongly recommended — it reduces friction but is not mandated. `eslint-config-prettier` disables all ESLint formatting rules so the two tools do not conflict.

### Upgrading pnpm

When upgrading pnpm:

1. Update `PNPM_VERSION` in `Makefile`
2. Run `corepack use pnpm@<new-version>` inside `frontend/` to update the `packageManager` field and sha512 hash in `package.json` and regenerate `pnpm-lock.yaml`
3. Commit both files

---

## Validation

### Success Metrics

- `make frontend-install` completes successfully on a machine with only Node.js installed, with no manual pnpm install step
- `npm install` in `frontend/` fails with a clear error directing the developer to use pnpm
- A client component importing from `src/server/` produces a build-time error, not a runtime error
- `make frontend-clean && make frontend-install` reliably resets the dependency state when debugging install issues
- `make frontend-install` exits 0 in CI (`CI=true`) on every branch with a clean, up-to-date lockfile
- `make frontend-lint` exits 0 on a clean codebase; `make frontend-fmt` fixes formatting locally and checks in CI; both are invoked together via `make frontend-check`, which is called by the pre-commit hook and in CI

### Monitoring

- Track CI install times — regression here may indicate lockfile churn or package bloat
- Review `pnpm-lock.yaml` diffs in pull requests to audit new dependencies

---

## Related Decisions

- [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) — Established the Next.js + Go monorepo structure and the principle of separate dependency management per component (`go.mod` for backend, `package.json` for frontend). This ADR extends [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md)'s monorepo layout by introducing the `frontend/` directory with its own pnpm-managed dependency graph. The absence of a root-level `package.json` is a direct consequence of [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md)'s model.
- [language-and-framework-selection](2026-02-16-language-and-framework-selection.md) — Established Next.js, React, TypeScript as frontend choices, and named ESLint and Prettier as code quality tools. This ADR formalises those choices and evaluates the App Router over the Pages Router. Terminology note: [language-and-framework-selection](2026-02-16-language-and-framework-selection.md) uses the older term "API routes"; this ADR uses the correct App Router term "Route Handlers".
- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — Established the backend's one-way dependency rule: platform packages may not import feature packages. The frontend's zone rule — `src/server/*` may only be imported by `src/app/api/*`, never by client-facing files — is the same architectural principle applied to the frontend. Both enforce a trusted kernel (backend: `internal/platform/`; frontend: `src/server/`) that only one designated layer may access. This is a project-wide value, not a backend-specific one. See Notes for more on the DDD parallel.
- [local-development-environment](2026-03-10-local-development-environment.md) — Defines `make bootstrap` / `make setup` targets that include `frontend-install`

---

## References

- [pnpm documentation — Strictness](https://pnpm.io/motivation)
- [Corepack documentation](https://nodejs.org/api/corepack.html)
- [Next.js App Router documentation](https://nextjs.org/docs/app)
- [Next.js Route Handlers documentation](https://nextjs.org/docs/app/building-your-application/routing/route-handlers)
- [Next.js `server-only` documentation](https://nextjs.org/docs/app/building-your-application/rendering/composition-patterns#keeping-server-only-code-out-of-the-client-environment)
- [only-allow package](https://github.com/pnpm/only-allow)
- [Prettier documentation](https://prettier.io/docs/en/index.html)
- [eslint-config-prettier](https://github.com/prettier/eslint-config-prettier)

---

## Notes

The `packageManager` field in `package.json` is labelled "experimental" in Node.js documentation because it is implemented via Corepack, which itself carries the "experimental" label. In practice the field is stable across all Node.js LTS releases (18, 20, 22) and is the mechanism recommended by the pnpm project for version pinning. This label should not be interpreted as instability — it reflects the pace of Node.js core's formal stabilisation process, not the real-world reliability of the feature.

The decision not to use Bun as a runtime (not just package manager) is deliberate and separate from the package manager selection. Replacing the Node.js runtime is a larger change with more surface area; that decision is deferred and requires its own evaluation if revisited.

**On the DDD parallel with the backend:** [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) established "DDD-lite" (Pragmatic Feature-Based Architecture) for the Go backend — it adopted the DDD principle of bounded contexts and unidirectional dependencies while avoiding DDD ceremony. One of the key structural outcomes is a trusted infrastructure kernel (`internal/platform/`) accessed only by feature packages, never the reverse. The frontend's zone rules in this ADR express the same principle: the trusted server kernel (`src/server/`) is accessed only by the BFF surface (`src/app/api/`), and that boundary is enforced both structurally (directory layout) and statically (the `server-only` guard). The mechanism differs (directory naming + build-time import errors vs. Go package boundaries), but the underlying value is identical: a clearly bounded, inward-only dependency on privileged infrastructure. Future decisions about frontend structure should preserve this directional discipline, consistent with how [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) preserves it on the backend.

**Last Updated:** 2026-07-10
