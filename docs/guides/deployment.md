# Deploying runkora

runkora runs on your own Kubernetes cluster. These guides take you from an empty cluster to a platform your teams can sign in to, and cover running it day to day.

If you are new to kora, start with [runkora/examples](https://github.com/runkora/examples). Its [quickstart](https://github.com/runkora/examples/tree/main/quickstart) runs `make up` to install kora on a local k3d cluster with a working example, from a single tenant to an agent factory. Each example is laid out as a GitOps repository you would run in production. When you move to a real cluster, you can start from the example closest to your setup.

## Install on a cluster

Work through these two guides in order:

1.  [Prepare your cluster](prepare-your-cluster.md): set up Istio, SPIRE, PostgreSQL, Redis, storage and DNS.
2.  [Install runkora on Kubernetes](install-on-kubernetes.md): create the platform secrets, set the chart values and install with Helm.

Once the install is verified, [onboard your first tenant](tenants.md) so a team can sign in.

## Configure and operate

Use these guides as your install needs them:

- [Configure sandboxes on your cluster](sandbox-nodes.md) to run sandboxes on their own node pool.
- [Pod labels and annotations for agents](pod-metadata.md) to add your own pod metadata.
- [White-labelling theme](white-labelling.md) to show a client's brand.
- [Monitor runkora](monitor-kora.md) to export telemetry and watch runs.
- [Upgrade runkora](upgrade.md) to move to a new release.
