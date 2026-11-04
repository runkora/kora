# Secrets

Secrets are values that, if leaked, would require a security incident response: passwords, API keys, tokens, certificates, private keys. These never go in YAML config files or version control.

runkora reads secrets from environment variables at runtime.

---

## Local Development

`make setup` generates a machine-local `.env` at the repository root. It is gitignored and never committed. It holds only `DB_PASSWORD`, `REDIS_PASSWORD` and `BFF_SESSION_SECRET`. The Postgres and Redis passwords are fixed local defaults; `BFF_SESSION_SECRET` is generated with `openssl rand`. Nothing needs editing before the platform runs.

Everything with a safe default lives elsewhere: compose settings in `docker-compose.yml`, in-cluster settings in `deploy/reference/environments/local.yaml`. `.env.example` is not a template to copy; it documents the optional overrides (Bedrock, port collisions).

`make up` reads `.env`, injects these values into the cluster's `kora-secrets`, and brings the platform up.

---

## Accessing Secrets in Code

Read secrets directly from the environment using `os.Getenv`. Never store them in a config struct or pass them through YAML.

```go
clientID := os.Getenv("KORA_OIDC_CLIENT_ID")
if clientID == "" {
    return fmt.Errorf("KORA_OIDC_CLIENT_ID is required")
}
```

Fail fast on startup if a required secret is missing, don't let the service start in a broken state.

---

## Multi-Line Secrets (RSA Keys)

RSA private keys are PEM-encoded and multi-line. Standard `.env` parsers do not reliably preserve newlines, and `pem.Decode` requires real newlines to parse PEM headers and footers.

**For local development**, `make gen-signing-key` writes the key to `secrets/signing.key` (gitignored). `make up` generates it automatically if absent, then loads it into the cluster's `kora-secrets` as the inline `KORA_TOKEN_SIGNING_KEY`.

```bash
make gen-signing-key          # writes secrets/signing.key (gitignored)
```

**For CI/production**, secret managers (GitHub Actions secrets, Kubernetes Secrets, Vault) inject multi-line values with real newlines. Use the inline variable:

```bash
KORA_TOKEN_SIGNING_KEY=-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA...
-----END RSA PRIVATE KEY-----
```

**Priority**: `KORA_TOKEN_SIGNING_KEY_FILE` is checked first. `KORA_TOKEN_SIGNING_KEY` is the fallback. If neither is set and `APP_ENV=local`, an ephemeral key is generated at startup (tokens do not survive a restart, and a warning is logged).

---

## Dex OIDC Credentials (Local Only)

Dex authenticates the local test user against `glauth`, an in-memory LDAP server, not against a static password list. The password hash is `GLAUTH_DEV_PASSWORD_HASH`, a sha256 of the literal `password`, defaulted in `docker-compose.yml` and rendered into `config/glauth/config.cfg` at container start.

Nothing to generate. The test user is `dev@local.runkora.io` with password `password`. Override the hash in `.env` if you want a different one.

---

## Secret Reference

### Application

| Variable | Where Set | Description |
|---|---|---|
| `APP_ENV` | `.env` for host-run binaries | Environment: `local` or `production` (the default). Not a secret but selects the `{env}.yaml` config layer. The chart does not set it. |

### Database

| Variable | Where Set | Description |
|---|---|---|
| `DB_PASSWORD` | `.env` / secret manager | PostgreSQL password. Generated into `.env` by `make setup` (value `kora`), defaulted in `docker-compose.yml`. |

### Redis

| Variable | Where Set | Description |
|---|---|---|
| `REDIS_PASSWORD` | `.env` / secret manager | Redis password. Required in all environments. Generated into `.env` by `make setup` (value `kora`), defaulted in `docker-compose.yml`. |

### Token Signing

| Variable | Where Set | Description |
|---|---|---|
| `KORA_TOKEN_SIGNING_KEY_FILE` | code fallback | Path to an RSA private key PEM file (mode 600). Checked first by the code. |
| `KORA_TOKEN_SIGNING_KEY` | `kora-secrets` (local) / secret manager | Inline RSA private key PEM. Local dev generates `secrets/signing.key` with `make gen-signing-key`; `make up` injects it here. |

### OIDC / Federated Identity

| Variable | Where Set | Description |
|---|---|---|
| `KORA_OIDC_CLIENT_ID` | `.env` / secret manager | OAuth2 client ID. For local Dex: `kora-local`. |
| `KORA_OIDC_CLIENT_SECRET` | `.env` / secret manager | OAuth2 client secret. Empty for public clients (local Dex is a public client). |
| `KORA_OIDC_ISSUER_URL` | `.env` / secret manager | Runtime override for the OIDC issuer URL. Not strictly a secret, but can encode tenant IDs. |

### Dex (Local Dev Only)

| Variable | Where Set | Description |
|---|---|---|
| `GLAUTH_DEV_PASSWORD_HASH` | `docker-compose.yml` default | sha256 of the local test user's password. Override in `.env` to change it. |

---

## Production

In deployed environments, inject secrets via your platform's secret mechanism:

- **Kubernetes**: `Secret` objects mounted as environment variables or projected volumes
- **AWS**: Secrets Manager or Parameter Store via the pod's IAM role
- **GCP**: Secret Manager with Workload Identity
- **HashiCorp Vault**: Agent sidecar injection

The application code is identical regardless: it always reads `os.Getenv`.

---

## Adding a New Secret

1. Add `os.Getenv("YOUR_VAR")` with a fail-fast check in the relevant service initializer
2. If it is machine-local, generate it into `.env` in the `env` target in the `Makefile`, and wire it into `kora-secrets` via `scripts/cluster/create-secrets.sh`
3. Add the key to the `kora.platformEnv` key list of each workload that reads it (`charts/kora/templates/<service>/deployment*.yaml`, or `migrate/job.yaml`) and to the expected keys in `backend/internal/platform/config/chart_secrets_test.go`. A pod receives only the `kora-secrets` keys listed there
4. If it is an optional override, document it in `.env.example`
5. Add it to this reference table above
6. Add it to the relevant deployment secret store for staging/production

---

## Reference

- [config.md](../contributing/configuration.md): YAML-based configuration for non-secret values
- [contributing/local-development.md](../contributing/local-development.md): Full local setup walkthrough
- [yaml-file-based-configuration](../adr/2026-02-18-yaml-file-based-configuration.md): Configuration system design
- [oidc-provider-abstraction-and-local-identity-provider](../adr/2026-03-16-oidc-provider-abstraction-and-local-identity-provider.md): OIDC provider abstraction
- `SECURITY.md`: reporting vulnerabilities
