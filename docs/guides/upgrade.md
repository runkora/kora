# Upgrade runkora

In kora, a release ships the `kora-crds` and `kora` charts together at one version. An upgrade migrates the database schema before it changes any workload. Each release's schema works with the binaries of the release before it. The current release keeps serving during the rollout. A rollback leaves the database untouched.

The following guide assumes kora is already installed. See [Install runkora on Kubernetes](install-on-kubernetes.md) for the install.

## Before you upgrade

Each release's `compatibility.yaml` lists the Kubernetes, Istio, SPIRE, PostgreSQL and Helm versions it supports. Read it with the release notes on GitHub. Upgrade anything the new release no longer supports first.

You can upgrade from any earlier release straight to the target. The migration applies every pending change in one pass. When an upgrade skips a minor version, pods from the old release can return errors until the rollout finishes.

## Upgrade the charts

Upgrade `kora-crds` first, then `kora`, with the values file you installed with:

```bash
helm upgrade kora-crds oci://ghcr.io/runkora/charts/kora-crds \
  --namespace kora-system --version <target>
helm upgrade kora oci://ghcr.io/runkora/charts/kora \
  --namespace kora-system --version <target> --values kora.yaml
```

If you use a [theme](white-labelling.md), pass `--set-file theme.document=theme.yaml` on every upgrade. An upgrade without it removes the theme.

Keep `global.trustDomain` unchanged. Every workload's identity contains the trust domain. Changing it means migrating every identity on the platform.

Once the rollout finishes, check the platform the same way as after the install. See [Verify the installation](install-on-kubernetes.md#verify-the-installation).

## When a migration fails

Helm runs the migration as the `kora-migrate` Job before it changes any workload. A failed Job stops the upgrade. The current release keeps serving. Read the Job's output to find the cause:

```bash
kubectl logs job/kora-migrate -n kora-system
```

A migration that runs longer than `migrate.activeDeadlineSeconds`, 240 seconds by default, fails. If you raise the value, also raise Helm's `--timeout` above it. Otherwise Helm fails the release while the Job is still running.

Fix the cause, then run the same `helm upgrade` again.

## Roll back

kora supports rolling back to the previous revision only:

```bash
helm rollback kora <previous revision> --namespace kora-system
```

A rollback reverts kora's workloads. The database schema and the CRDs stay at the newer release. The previous release's binaries run against them. kora never runs a down migration.

## References

- [Database migrations](../adr/2026-09-10-database-migrations.md): the compatibility contract and the migration Job.
- [Install and distribution model](../adr/2026-09-04-install-and-distribution-model.md): chart order and rollback.
