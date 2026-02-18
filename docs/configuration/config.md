# Configuration

Orbit uses YAML files for non-secret configuration, one file per environment. The active environment is selected by the `APP_ENV` environment variable (default: `local`).

```
APP_ENV=production  →  loads {module}/config/production.yaml
APP_ENV=local       →  loads {module}/config/local.yaml  (default)
```

Supported values: `local`, `development`, `production`.

> **Secrets do not go in YAML files.** Passwords, API keys, and certificates belong in environment variables or a secret manager. If rotating a value would trigger a security incident, it's a secret.

---

## Reading config values

Each binary and feature owns its config struct. For `cmd/orbit`, the config is loaded once at startup and available as a package-level variable:

```go
import oc "github.com/clear-route/orbit/cmd/orbit/config"

port := oc.Config.Server.Port
timeout := oc.Config.ShutdownTimeout()
```

---

## Adding a value to an existing config

1. Add the field to the config struct:

```go
// cmd/orbit/config/config.go
type orbitConfig struct {
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
# cmd/orbit/config/local.yaml
server:
  port: 6069
  basePath: /api/v1
  shutdownTimeoutSecs: 5
  readTimeoutSecs: 30  # ← new
```

That's it. The loader will unmarshal the new field automatically.

---

## Adding config for a new feature

1. Create the config package with its YAML files:

```
backend/internal/{feature}/config/
├── config.go
├── local.yaml
├── development.yaml
└── production.yaml
```

2. Define the config struct and package-level variable in `config.go` — same pattern as a binary:

```go
// internal/{feature}/config/config.go
package config

var Config = config.MustLoad[featureConfig]("internal/{feature}", config.Environment())

type featureConfig struct {
    Cache struct {
        TTLSeconds int `yaml:"ttlSeconds"`
        MaxSize    int `yaml:"maxSize"`
    } `yaml:"cache"`
}
```

3. Import and use it wherever needed in the feature:

```go
import fc "github.com/clear-route/orbit/internal/{feature}/config"

ttl := time.Duration(fc.Config.Cache.TTLSeconds) * time.Second
```

---

## Reference

- `internal/platform/config` — the loader package (`Load`, `MustLoad`, `Environment`)
- [ADR-0006](adr/0006-yaml-file-based-configuration.md) — decision record and rationale
