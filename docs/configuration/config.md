# Configuration

Orbit uses YAML files for non-secret configuration, one file per environment. The active environment is selected by the `APP_ENV` environment variable (default: `local`).

```
APP_ENV=production  →  loads {module}/config/production.yaml
APP_ENV=local       →  loads {module}/config/local.yaml  (default)
```

Supported values: `local`, `development`, `production`.

> **Secrets do not go in YAML files.** Passwords, API keys, and certificates belong in environment variables or a secret manager. If rotating a value would trigger a security incident, it's a secret.

---

## Two loading patterns

There are two places config is loaded, each with a slightly different pattern.

### Binary entry point (`cmd/{service}`)

Each service binary under `backend/cmd/` (for example the main API binary `cmd/identity`) has a dedicated `config/` sub-package with a `config.go` that exposes a package-level variable initialised at startup. Binaries use the **per-service loader** `config.MustLoadService[T]` (or `config.LoadService[T]` for the error-returning variant), which reads `backend/cmd/{service}/config/base.yaml` plus an optional `{env}.yaml` overlay:

```go
// cmd/identity/config/config.go
package identityConfig

import "github.com/clear-route/orbit/internal/platform/config"

// Config holds the identity service's own configuration values.
type Config struct {
    App struct {
        Version string `yaml:"version"`
    } `yaml:"app"`

    Server struct {
        Port                int    `yaml:"port"`
        BasePath            string `yaml:"basePath"`
        ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
    } `yaml:"server"`

    Observability struct {
        ServiceName string `yaml:"serviceName"`
    } `yaml:"observability"`
}

// Current is initialised once at program start, reading
// cmd/identity/config/base.yaml (+ optional {env}.yaml overlay).
var Current = config.MustLoadService[Config]("cmd/identity", config.Environment())
```

`MustLoadService` panics on startup if the file is missing or unparseable: this surfaces misconfiguration immediately rather than silently at first use. `LoadService` returns the error instead of panicking.

> **`LoadService` vs `Load`.** The per-service loader `LoadService[T](serviceModule, env)` resolves config under `backend/cmd/{service}/config/` and is the primary pattern used by the service binaries. The generic loader `Load[T](module, env)` resolves config under an arbitrary module path (e.g. `internal/{feature}`) and is used by feature packages, as shown below. Both have `Must*` variants that panic on error.

### Feature packages (`internal/{feature}`)

Feature packages do **not** create a `config/config.go` sub-package. Instead they:

1. Put their YAML config files directly in `{feature}/config/`
2. Define their config struct inside `service.go` (or a dedicated `config.go` at the feature root)
3. Call `config.Load[T]()` explicitly inside a `LoadConfig()` function

```go
// internal/identity/service.go  (abbreviated)

// Config is the identity service configuration.
type Config struct {
    OIDC struct {
        IssuerURL string   `yaml:"issuerUrl"`
        Scopes    []string `yaml:"scopes"`
    } `yaml:"oidc"`

    // Secrets are never in YAML — tagged "-" and read from env vars directly.
    TokenSigningKey     string `yaml:"-"`
    TokenSigningKeyFile string `yaml:"-"`
    OIDCClientID        string `yaml:"-"`
    OIDCClientSecret    string `yaml:"-"`
}

// LoadConfig loads the YAML config for the current environment and overlays
// secrets from environment variables.
func LoadConfig() (*Config, error) {
    cfg, err := config.Load[Config]("internal/identity", config.Environment())
    if err != nil {
        return nil, err
    }

    // Overlay secrets from environment — never from YAML.
    cfg.TokenSigningKeyFile = os.Getenv("ORBIT_TOKEN_SIGNING_KEY_FILE")
    cfg.TokenSigningKey     = os.Getenv("ORBIT_TOKEN_SIGNING_KEY")
    cfg.OIDCClientID        = os.Getenv("ORBIT_OIDC_CLIENT_ID")
    cfg.OIDCClientSecret    = os.Getenv("ORBIT_OIDC_CLIENT_SECRET")

    // Runtime override: allows pointing at a real provider without editing YAML.
    if url := os.Getenv("ORBIT_OIDC_ISSUER_URL"); url != "" {
        cfg.OIDC.IssuerURL = url
    }

    return cfg, nil
}
```

---

## Adding a value to an existing config

1. Add the field to the config struct:

```go
type appConfig struct {
    Server struct {
        Port                int    `yaml:"port"`
        BasePath            string `yaml:"basePath"`
        ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
        ReadTimeoutSecs     int    `yaml:"readTimeoutSecs"`  // ← new
    } `yaml:"server"`
}
```

2. Add the value to each environment YAML file:

```yaml
# cmd/identity/config/base.yaml
server:
  port: 8080
  basePath: /api/v1
  shutdownTimeoutSecs: 5
  readTimeoutSecs: 30  # ← new
```

The loader uses `dec.KnownFields(true)`, so unknown keys in YAML are rejected at startup: typos surface immediately.

---

## Adding config for a new feature

1. Create the YAML files (no `config.go` sub-package needed):

```
backend/internal/{feature}/config/
├── local.yaml
├── development.yaml
└── production.yaml
```

2. Define the config struct and `LoadConfig()` in `service.go` (or a top-level `config.go` in the feature package):

```go
// internal/{feature}/service.go

type Config struct {
    Cache struct {
        TTLSeconds int `yaml:"ttlSeconds"`
        MaxSize    int `yaml:"maxSize"`
    } `yaml:"cache"`

    // Secrets always use yaml:"-" and are loaded from env vars.
    APIKey string `yaml:"-"`
}

func LoadConfig() (*Config, error) {
    cfg, err := config.Load[Config]("internal/{feature}", config.Environment())
    if err != nil {
        return nil, err
    }
    cfg.APIKey = os.Getenv("ORBIT_FEATURE_API_KEY")
    return cfg, nil
}
```

3. Call `LoadConfig()` from `main.go` when wiring the service:

```go
// cmd/identity/main.go
featureCfg, err := feature.LoadConfig()
if err != nil {
    logger.Error(ctx, "failed to load feature config", logging.Field{Key: "error", Value: err})
    os.Exit(1)
}
svc := feature.NewService(db, redis, logger, featureCfg)
```

---

## Reference

- `internal/platform/config/config.go`: the loader package (`Load`, `MustLoad`, `LoadService`, `MustLoadService`, `Environment`)
- `internal/identity/service.go`: canonical example of feature-level `LoadConfig()` with secret overlays
- `cmd/identity/config/config.go`: canonical example of binary-level `MustLoadService` package var
- [yaml-file-based-configuration](../adr/2026-02-18-yaml-file-based-configuration.md): decision record and rationale
- [Secrets reference](secrets.md): how secrets are separated from config
