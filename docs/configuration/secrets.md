# Secrets

Secrets are values that, if leaked, would require a security incident response — passwords, API keys, tokens, certificates, connection strings. These never go in YAML config files or version control.

Orbit reads secrets from environment variables at runtime.

---

## Local development

Use a `.env` file in the `backend/` directory for local secrets. This file is gitignored and never committed.

```bash
# backend/.env
DATABASE_URL=postgres://orbit:password@localhost:5432/orbit
JWT_SECRET=dev-only-secret
SOME_API_KEY=abc123
```

Start the server with your `.env` loaded:

```bash
# Using make (recommended)
make run

# Or manually
source .env && go run ./cmd/orbit
```

Copy `.env.example` to get started — it lists every required variable with placeholder values:

```bash
cp backend/.env.example backend/.env
```

---

## Accessing secrets in code

Read secrets directly from the environment using `os.Getenv`. Never store them in a config struct or pass them through YAML.

```go
dsn := os.Getenv("DATABASE_URL")
if dsn == "" {
    return fmt.Errorf("DATABASE_URL is required")
}
```

Fail fast on startup if a required secret is missing — don't let the service start in a broken state.

---

## Production

In deployed environments, inject secrets via your platform's secret mechanism:

- **Kubernetes** — `Secret` objects mounted as environment variables
- **AWS** — Secrets Manager or Parameter Store via the pod's IAM role
- **GCP** — Secret Manager with Workload Identity

The application code is identical regardless — it always reads `os.Getenv`.

---

## .env.example

The `.env.example` file documents every secret the application needs. It must be kept up to date. When you add a new secret:

1. Add it to `.env.example` with a placeholder value and a comment explaining what it is
2. Add it to your local `.env`
3. Add it to the relevant deployment secret store

```bash
# backend/.env.example

# PostgreSQL connection string
DATABASE_URL=postgres://orbit:password@localhost:5432/orbit

# HS256 key for signing JWTs — generate with: openssl rand -hex 32
JWT_SECRET=
```

---

## Reference

- [config.md](config.md) — YAML-based configuration for non-secret values
- `SECURITY.md` — reporting vulnerabilities
