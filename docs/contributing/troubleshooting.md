# Troubleshooting local development

This guide covers the failures you are most likely to meet in the local k3d environment. For setup and the normal development loop, see [local development](local-development.md).

## Start with the environment

Before debugging an individual service, check that you are looking at the local cluster and that each layer of the environment is running.

```bash
kubectl config current-context                    # expected: k3d-orbit-local
docker compose ps                                 # Postgres, Redis, glauth, Dex, HAProxy
kubectl get orbitplatform orbit                   # operator reconciliation state
kubectl get pods -n kora-system                  # control-plane services
kubectl get pods -n kora-tenant-example          # example tenant workloads
```

The operator's preflight reports whether SPIRE and Istio meet kora's prerequisites. If a tenant is not `Ready`, check its `PrerequisitesSatisfied` condition and the operator log before chasing an application pod:

```bash
kubectl describe koratenant <name>
kubectl logs deployment/kora-operator -n kora-system
```

`make up` is idempotent. Re-running it is the first recovery step when setup stopped partway through or the cluster drifted after a Docker restart.

## Inspect a failing pod

A pod can be running while one of its containers is unready or repeatedly restarting. Check the containers separately.

```bash
kubectl get pod <pod> -n kora-system \
  -o jsonpath='{range .status.initContainerStatuses[*]}{.name}{"\t"}{.ready}{"\t"}{.restartCount}{"\n"}{end}{range .status.containerStatuses[*]}{.name}{"\t"}{.ready}{"\t"}{.restartCount}{"\n"}{end}'

kubectl logs <pod> -n kora-system -c <container>
kubectl logs <pod> -n kora-system -c <container> --previous
kubectl describe pod <pod> -n kora-system
```

Use `--previous` for a crash loop: it shows the logs from the container invocation that has already failed. `kubectl describe` shows scheduling failures, image-pull errors, failed probes, and recent pod events.

Every meshed pod has an `istio-proxy`. Agent workloads also have an `identity-proxy`. If the application container is healthy but the pod is not ready, inspect the unready sidecar rather than the application again.

Port-forwarding reaches a Kubernetes Service, but it does not reproduce the public ingress path or provide a workload identity. Use the documented ingress URLs for user-facing APIs. Use logs, readiness state, and in-cluster requests when diagnosing service-to-service traffic.

If the pod is running an older image than the one in the local registry:

```bash
make redeploy-status
make redeploy SERVICE=<service>
```

## `make auth` fails with `oidc_not_configured`

The identity service builds its OIDC provider registry from the `KoraTenant` resources reconciled by the operator. On a cold cluster it may start before the example tenant exists.

First, re-run the idempotent setup and check the identity logs:

```bash
make up
kubectl logs deployment/kora-identity -n kora-system -c kora-identity \
  | grep -E 'OIDC provider initialized|hot-reload disabled|no tenants in registry'
```

If the logs contain `too many open files`, the Docker VM has exhausted its inotify instances. kora uses file watchers to reload operator-managed configuration, including the tenant registry. `make up` raises the VM limit to 1024, but the setting is lost when the Docker VM restarts.

Check the value seen by the k3d node:

```bash
docker exec k3d-orbit-local-server-0 \
  cat /proc/sys/fs/inotify/max_user_instances
```

If it is below 1024, apply the same recovery performed by `make up`, then restart identity:

```bash
docker run --rm --privileged alpine:3.20 \
  sysctl -w fs.inotify.max_user_instances=1024
kubectl rollout restart deployment/kora-identity -n kora-system
kubectl rollout status deployment/kora-identity -n kora-system
```

Run `make auth` again once the identity logs show that the OIDC provider has been initialised.

## `make auth` times out

The device code expired before the browser flow completed. Run `make auth` again and enter the new code. If authentication previously worked but the access token has expired, use `make auth-refresh`. If the refresh token has also expired, start again with `make auth`.

## Dex or glauth is unhealthy

Local accounts live in glauth and Dex authenticates them over LDAP. Both Compose services must be healthy for the browser sign-in flow to work.

```bash
docker compose ps glauth dex
docker compose logs glauth dex
docker compose restart glauth dex
```

After restarting them, wait for both health checks to pass before running `make auth` again.

## The local HTTPS endpoint is unreachable

The local domain must resolve to `127.0.0.1`, HAProxy must own host ports 80 and 443, and the k3d gateway must be running.

```bash
dig +short orbit.local.clearroute.io              # expected: 127.0.0.1
docker compose ps port-proxy
kubectl get pods -n istio-system
kubectl get service -n istio-system
```

Run `make setup-dns` if the name does not resolve. Some corporate DNS resolvers block public records that resolve to loopback addresses. The command reports the expected records and the available fixes.

If HAProxy is not running, inspect its logs and check whether another process is using port 443:

```bash
docker compose logs port-proxy
lsof -nP -iTCP:443 -sTCP:LISTEN
```

The k3d load balancer remains available directly on `https://orbit.local.clearroute.io:8443`, which helps distinguish a host-port conflict from a cluster ingress failure.

If the browser reports a certificate error rather than a connection failure, regenerate and trust the local certificates:

```bash
make setup-tls
make cluster-tls-secret
```

## A pod cannot reach Postgres or Redis

Check the backing service first:

```bash
docker compose ps postgres redis
docker compose logs postgres redis
```

The local cluster relies on Docker networking and a CoreDNS route to the Compose services. If the backing service is healthy but pods started failing after a Docker or Colima restart, rebuild the disposable cluster:

```bash
make reset
```

`make reset` deletes and recreates the k3d cluster but preserves the Postgres and Redis volumes. Do not use `make dev-down-clean` unless you intend to delete local data.

## Identity fails to parse the signing key

An error containing `failed to parse RSA private key` means `secrets/signing.key` is missing or invalid. Generate a new local key and redeploy identity:

```bash
make gen-signing-key
make redeploy SERVICE=identity
```

Regenerating the key invalidates platform tokens signed with the previous key. Run `make auth` again after the deployment is ready.

## A query fails with `relation does not exist`

Check whether the service's migrations have been applied, then re-run the migrate hook:

```bash
make migrate-status
make redeploy SERVICE=migrate
```

If the migration is present but the error remains, confirm that the failing service is using the same database configured in the local environment before changing the schema.
