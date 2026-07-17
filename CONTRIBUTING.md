# Contributing to Orbit

Thanks for your interest in Orbit, the control plane for AI agents, the agentic engineering platform that takes an agent from a developer's laptop to a secure, governed production deployment. This guide is the **human front door** for contributors. It covers how to get set up, the workflow we follow, and the bar your change must clear before it merges.

For the exhaustive, machine-enforced engineering rules, **[`AGENTS.md`](AGENTS.md) is the canonical guide.** It documents every coding convention, every lint rule, and the rationale behind them. This document summarizes and points there; it does not duplicate it. Architecture lives in [`docs/`](docs/): start with the [architecture overview](docs/architecture.md), the [ADR index](docs/adr/README.md), and the [PRD index](docs/prd/README.md).

By contributing, you agree your work is licensed under the [Apache License 2.0](LICENSE).

---

## 1. Getting set up

Orbit's backend is **Go 1.26**. Local development runs in Kubernetes. You'll need Docker (Docker Desktop or Colima) with at least 4 CPUs and 8 GiB of RAM allocated to the VM, Node 24 with pnpm 10 for the frontend, and Make. Three commands take you from clone to a running platform:

```bash
make setup   # once per machine: generate .env, install tools, git hooks, local DNS, mkcert TLS
make up      # backing services, k3d cluster, migrations, operator (SPIRE + Istio + Orbit), tenants, workloads. Idempotent.
make auth    # Device Authorization Grant against the cluster → dev@orbit.local.clearroute.io / password
```

`make setup` generates a machine-local `.env` with the secrets the platform needs, including `BFF_SESSION_SECRET`, which the frontend BFF refuses to start without. `make up` composes the granular `dev-cluster`, `migrate-up`, `cluster-operator`, and `cluster-deploy` recipes, and is idempotent: re-run it whenever the cluster drifts. `make down` tears it down. `make auth` writes the token pair to `/tmp/orbit_credentials.json`; `make auth-refresh` rotates it when the token expires.

If this is your first day, [docs/guides/day-one.md](docs/guides/day-one.md) walks the whole path, backend and frontend.

The inner loop is `make redeploy` (`SERVICE=<name>` narrows it to one service). For the full walkthrough, how to call authenticated APIs, and troubleshooting, see the **[Local Development Guide](docs/guides/local-development.md)**.

> Run any `make` target without arguments (`make` or `make help`) to list every available target.

---

## 2. Development workflow

Orbit uses a **PRD → ADR → implementation** process so that the "why" is captured before the "how", and the "how" is captured before the code.

### New features or significant changes

1. **Write a PRD.** Copy [`docs/prd/template.md`](docs/prd/template.md) into `docs/prd/` and define the problem, user needs, and high-level solution. Collaborate with a maintainer to refine scope. See the [PRD index](docs/prd/README.md).
2. **Write an ADR.** After the PRD is agreed, copy [`docs/adr/template.md`](docs/adr/template.md) into `docs/adr/` documenting the technical approach: Context → Options → Decision → Consequences. See the [ADR index](docs/adr/README.md).
3. **Implement.** Only after the PRD and ADR are agreed. Write tests alongside the code and keep documentation current.

**An ADR before significant implementation is a hard rule.** Architectural decisions are not made inline in a PR. They're made in an ADR that the PR then references.

### Bug fixes and minor changes

Lighter weight, no PRD/ADR required:

1. Open an issue describing the problem (or reference an existing one).
2. Fix it, with tests and any doc updates.
3. Run the quality gate and open a PR.

---

## 3. ADR & PRD conventions

ADRs are named by **decision date plus a short slug**:

```
docs/adr/YYYY-MM-DD-kebab-slug.md
```

There is **no sequential number**. The slug is the citation handle: reference an ADR by its title/slug and link to the file (e.g. the *inference-gateway-architecture* ADR), **not** by a numeric "ADR-00NN". The [ADR index](docs/adr/README.md) is the catalogue and explains status values (`Proposed`, `Accepted`, `Deprecated`/`Superseded`); it also preserves the old sequential numbers in a *Former #* column for traceability with older commits.

PRDs follow the same spirit: start from [`docs/prd/template.md`](docs/prd/template.md) and link them to their primary ADR(s) in the [PRD index](docs/prd/README.md).

---

## 4. Code conventions

The codebase enforces **strict conventions mechanically**. Violations fail the quality gate, so you'll find out fast. In brief, the backend bans ORMs (explicit SQL via `pgx/v5` only, never `database/sql`), third-party HTTP routers, the stdlib `log`/`slog` (use the injected logger), hand-written migrations, and raw `http.Client` construction (use typed platform clients). Service-to-service calls go through typed clients in `internal/platform/clients/`; feature packages must not import each other. The frontend bans raw HTML primitives: all product JSX uses the **Terra** design system (`@orbit/terra`), enforced by ESLint with no escape hatches.

This is the short version. **The full rules, the lint-rule tables, and the "why" all live in [`AGENTS.md`](AGENTS.md).** Read it before writing non-trivial code. Enforcement is centralized: do not add inline `//nolint` or `eslint-disable` directives.

---

## 5. The quality gate

Every change must pass the quality gate with **zero issues** before a PR. The same checks run in CI.

**Backend**, run from the **repository root** (not `backend/`):

```bash
make check   # tidy + gofmt + go vet + golangci-lint + gitleaks (secret scan) + docs-check + tests + frontend checks
```

`docs-check` (`scripts/docs/check-docs.sh`) is a doc-drift guard: it fails on broken links, ADR/PRD index gaps, dead `make` targets, a stale binary table, and hardcoded counts, so the docs can't quietly assert something the code has changed underneath them.

**Frontend**, if you touched anything under `frontend/`:

```bash
cd frontend && pnpm lint && pnpm tsc --noEmit
```

Both must pass with zero errors. (`make check` already invokes the frontend checks via `frontend-check`; running the two `pnpm` commands directly is the fast inner-loop equivalent while iterating on the UI.)

Useful narrower targets: `make test` (Go unit tests with race detector + coverage), `make lint`, `make fmt`, `make secrets`, `make frontend-check`.

---

## 6. Commits & pull requests

- **Conventional Commits.** This repo uses `feat:`, `fix:`, `chore:`, etc., with optional scopes, e.g. `feat(agents): add run lifecycle` or `fix(inferencegw): correct SSE serialization`. Browse `git log --oneline` for the house style.
- **Reference the PRD/ADR** that motivates the change in your commit messages and PR description, so reviewers can trace the decision.
- **CI must be green.** PRs run `make check`; a red build will not be merged.
- Keep PRs focused and reviewable. Write a clear description of what changed and why.
- Branch from `main`; do not push directly to `main`.

---

## 7. Security

Orbit is infrastructure for autonomous agents. Security is not optional.

- **Never commit secrets.** No API keys, tokens, or credentials in code or config. The `gitleaks` scan in `make check` is a backstop, not a substitute for care. Secrets load via environment variables only.
- **Report vulnerabilities privately.** Do **not** open a public issue. See [`SECURITY.md`](SECURITY.md) for the disclosure process.

---

## 8. License

Orbit is licensed under the [Apache License 2.0](LICENSE). All contributions are accepted under the same license.

---

Questions about architecture? Reference the relevant ADR in a GitHub Discussion or issue. Thanks for helping build Orbit.
