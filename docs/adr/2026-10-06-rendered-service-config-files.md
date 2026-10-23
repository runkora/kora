# Rendered Service Config Files

<!-- Filename: 2026-10-06-rendered-service-config-files.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-10-06
**Deciders:** Justin Wilkin
**Technical Story:** Amends [YAML File-Based Configuration with Typed Generic Loader](2026-02-18-yaml-file-based-configuration.md). Related: clear-route/orbit#402, clear-route/orbit#403.

---

## Context

[2026-02-18](2026-02-18-yaml-file-based-configuration.md) decided that services load typed YAML through `internal/platform/config` and read secrets from env vars only. It listed a file and env overlay as a non-goal and recorded "No env var override layer" as a negative consequence. Its text has drifted from the code in two places. It describes a `gopkg.in/yaml.v3` loader and rejects Viper, but the loader imports `spf13/viper` and `go-viper/mapstructure` (`backend/internal/platform/config/config.go:33-34`). It states that `APP_ENV` defaults to `local`, but `Environment()` returns `production` when `APP_ENV` is unset (`config.go:40`, `config.go:48-53`). `LoadService` already merges `base.yaml` and `{env}.yaml` (`config.go:106-130`) and decodes with `UnmarshalExact`. An unknown key fails the decode (`config.go:148-157`).

The change touches none of the three questions. Secrets keep their env path, and identity, policy and credential brokering are unchanged.

### Background

Each image copies its modules' `config/` directories into `/app` (`backend/Dockerfile:43-51`, `backend/Dockerfile:71`, `backend/Dockerfile.session-manager:34-41`, `Dockerfile.operator:30-36`). The `orbit` chart never sets `APP_ENV`, and every in-cluster service loads the `production` layer. No module under `backend/cmd/` has a `production.yaml` file for that layer to load. `backend/cmd/operator/config/development.yaml` states that it loads when `APP_ENV=development`, and nothing in the repository sets that value. The same holds for `backend/internal/platform/observability/config/development.yaml` and `backend/internal/platform/storage/config/development.yaml`.

Per-install values therefore reach services as env vars. The chart sets 104 literal `env` values across 11 Deployments and the migrate Job in `charts/orbit/templates/`. Of these, 46 carry five platform settings (`ORBIT_LOG_FORMAT`, `ORBIT_OTLP_ENDPOINT`, `ORBIT_SAMPLING_RATE`, `ORBIT_POSTGRES_SSL_MODE`, `ORBIT_POSTGRES_CAPTURE_QUERY_TEXT`) repeated per workload. 44 of the 46 go through `orbit.serviceConfig` (`charts/orbit/templates/_helpers.tpl:83-90`), and the operator Deployment sets the other two directly. DB and Redis hosts reach eight Deployments through the `orbit-config` ConfigMap (`charts/orbit/templates/platform/configmap-orbit-config.yaml:11-16`) and `orbit.platformEnvFrom` (`_helpers.tpl:271-277`). No workload carries a checksum of `orbit-config`.

Lists travel as comma-separated strings (`charts/orbit/templates/operator/deployment.yaml:64`, `:93`, `charts/orbit/templates/agents/deployment.yaml:105`). Resource requirements travel as JSON (`charts/orbit/templates/operator/deployment.yaml:73`, `:85`). Each service layers these values over its YAML by hand:

- `backend/cmd/operator/config/config.go:120-199`: `loadInstall`, `envOr`, `intEnv`, `splitList` and `resourcesEnv`.
- `backend/cmd/kora-agents/config/config.go:191-237`: `applyEnvOverrides`.
- `backend/cmd/session-manager/config/config.go:126-139`: `applyEnvOverrides`.
- `backend/internal/identity/config.go:77-98`: token TTL overrides through `overrideInt`.
- `backend/internal/platform/observability/config.go:103-118`: OTLP, log format and sampling rate.
- `backend/internal/platform/storage/postgres_config.go:45-78` and `redis_config.go:36-56`: hosts, SSL mode, query capture and TLS.
- `backend/cmd/inference-gateway/config/config.go:93` and `backend/cmd/policy-service/config/config.go:63`: single values.

Several of these parsers drop a bad value without an error. `intEnv` returns the default when `strconv.Atoi` fails (`backend/cmd/operator/config/config.go:162-169`). The `orbit-agents` guardrail integers keep their YAML value on a parse failure (`backend/cmd/kora-agents/config/config.go:224-228`). `overrideInt` ignores a non-positive or unparseable value (`backend/internal/identity/config.go:91-98`). `ORBIT_SAMPLING_RATE` is ignored when it is not a float (`backend/internal/platform/observability/config.go:113-117`).

The current loader cannot decode `corev1.ResourceRequirements`. Viper with `yaml` tags fails on `limits: {cpu: 500m}` with `'resources.Limits[cpu]' expected a map or struct, got "string"`.

[2026-09-16](2026-09-16-first-class-workload-placement.md) rejected JSON in env vars for structured values and chose a rendered ConfigMap. [2026-09-15](2026-09-15-manifest-patches-configmap.md) moved manifest patches to a mounted ConfigMap and superseded the env-var overrides of [2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md). `charts/orbit/templates/agents/workloads-configmap.yaml` is the working precedent. It renders values with `toYaml`, the `orbit-agents` Deployment mounts it and rolls on its `checksum/workloads` annotation (`charts/orbit/templates/agents/deployment.yaml:30`), and `loadWorkloads` fails startup when `Validate()` rejects the file (`backend/cmd/kora-agents/render.go:28-47`).

The chart renders neither the session-manager pod nor the identity-proxy sidecar. `orbit-agents` writes the session-manager container env in Go (`backend/internal/agents/k8s.go:1549-1560`), and the operator webhook writes the sidecar env (`backend/internal/operator/pod_webhook.go:233-243`).

### Problem Statement

Per-install settings travel as untyped env strings that each service parses by hand. Structured values need string encodings that 2026-09-16 rejected, and some bad values fail silently.

### Goals

- Every non-secret per-install value of a chart-rendered workload reaches its service as typed YAML through the shared loader.
- An unknown key or a malformed value fails startup.
- A config change rolls the pods that read it.

### Non-Goals

- Hot reload of config.
- The `orbit-secrets` fan-out to every service (clear-route/orbit#403).

---

## Decision Drivers

- Lists, maps and Kubernetes types such as `corev1.ResourceRequirements` decode without a string encoding.
- Chart values stay the product surface defined by [2026-09-04](2026-09-04-install-and-distribution-model.md).
- One loader path serves every service.

---

## Considered Options

### Option A: Env struct tags in the core loader (rejected)

**Description:** The core loader reads an `env` struct tag on each config field and overlays the named env var on the YAML value.

It keeps JSON and comma-separated values in env vars against 2026-09-16. Every value stays a string until the loader parses it. A misspelled env var name is ignored without an error.

### Option B: Rendered config files (chosen)

**Description:** The chart renders each service's per-install config as YAML into a ConfigMap. The loader merges the mounted file as the last layer with strict decoding.

It follows the `workloads-configmap.yaml` precedent and reuses the existing loader, file layout and strict decoding.

### Option C: Viper `AutomaticEnv` or `BindEnv` (rejected)

**Description:** The loader enables Viper's env binding and maps each config key path to an env var.

`AutomaticEnv` skips keys that are absent from the YAML. A test against the repository's Viper version set `A_PRESENT` and `A_ABSENT` and decoded only `a.present`. The derived names rename every existing variable. The decoder still cannot read Kubernetes types. 2026-02-18 rejected Viper's env binding and listed an env overlay as a non-goal.

### Option D: Chart values mirror the Go config structs 1:1 (rejected)

**Description:** Each service's chart values block copies its Go config struct, and the chart renders it into the file unchanged.

It exposes internal config as the public chart API against the product surface that 2026-09-04 defines.

---

## Decision

**Chosen Option:** Option B, Rendered Config Files

### 1. Rendered files

The chart will render each service's per-install config as YAML into a ConfigMap, with one file per config module. The ConfigMap will mount at `/etc/orbit/config/`, and `items[].path` will place each module's file at `/etc/orbit/config/{module}.yaml`. The config files baked into the image will stay visible under `/app`. The chart will derive service URLs from the system namespace when it renders these files (clear-route/orbit#402). The migrate Job's ConfigMap will be a `pre-install,pre-upgrade` hook with a lower hook weight than the Job.

### 2. Loader

`config.LoadService` and `config.Load` will merge `/etc/orbit/config/{module}.yaml` as the last layer. Decoding will stay strict, and an unknown key will fail startup. A decode hook will decode Kubernetes types such as `corev1.ResourceRequirements`. A missing file will mean no overlay.

### 3. Rollout

Each workload will carry a `checksum/` annotation for its rendered config, and a config change will roll its pods. There will be no hot reload.

### 4. Env vars

Env vars on chart-rendered workloads will carry only secrets and third-party conventions (e.g., `SPIFFE_ENDPOINT_SOCKET`, `HOME`). The old env names will be removed.

### 5. Chart values

Each service will expose curated chart keys and a raw `configOverrides` block that merges last. `extraEnv` will remain for secrets only. The `orbit.serviceConfig` env fan-out will be removed. DB and Redis hosts will move from the `orbit-config` `envFrom` into the storage module's file.

### 6. Go-rendered env

The session-manager and the webhook-injected identity-proxy sidecar will keep env vars that Go code sets. A typed Go producer will write those variables.

### 7. Environment layers

The `{env}.yaml` layer will stay for binaries that run on the host. The `development.yaml` files under `backend/cmd/operator/config/`, `backend/internal/platform/observability/config/` and `backend/internal/platform/storage/config/` will be removed.

### 8. Phasing

1. The loader change, `orbit-agents` and the operator.
2. The shared platform settings and `orbit-config`.
3. The remaining single values.

### Rationale

None at this time.

---

## Consequences

### Positive

- The hand-written env parsers in the operator, `orbit-agents`, identity, observability and storage will be deleted.
- The effective per-install config of each service will be readable in one ConfigMap.

### Negative

- Every workload will gain a ConfigMap, a volume, a mount and a checksum annotation.
- An install that sets a removed env name through `extraEnv` will lose that setting without an error.
- A `configOverrides` entry depends on Go config key names. A renamed key will fail startup on the next upgrade of an install that overrides it.
- The session-manager and the identity-proxy sidecar will keep a second, env-based delivery path.

### Neutral

- The loader, the module file layout and the secrets rule of 2026-02-18 stay in force.

---

## Implementation

None at this time.

---

## Validation

None at this time.

---

## Related Decisions

- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md) - amended by this decision.
- [first-class-workload-placement](2026-09-16-first-class-workload-placement.md)
- [manifest-patches-configmap](2026-09-15-manifest-patches-configmap.md)
- [k8s-manifest-overrides-via-env-vars](2026-06-12-k8s-manifest-overrides-via-env-vars.md)
- [install-and-distribution-model](2026-09-04-install-and-distribution-model.md)

---

## References

- [clear-route/orbit#402: Service URLs hardcode the orbit-system namespace](https://github.com/clear-route/orbit/issues/402)
- [clear-route/orbit#403: Every service receives every orbit-secrets key](https://github.com/clear-route/orbit/issues/403)

---

**Last Updated:** 2026-10-06
