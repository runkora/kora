# Pod labels and annotations for agents

Each agent runs as an agent runtime pod, and each run can start a sandbox pod. Add your own labels and annotations to these pods for cost allocation, network policies or admission controllers that select pods by them.

The following guide assumes kora is already installed. See [Install runkora on Kubernetes](install-on-kubernetes.md) for the install.

## Add labels and annotations

This labels every agent pod with a cost centre and annotates sandbox pods only:

```yaml
agents:
  workloads:
    common:
      podLabels:
        cost-centre: platform
    sandbox:
      podAnnotations:
        example.com/workload-tier: sandbox
```

- `common`: applies to agent runtime pods and sandbox pods.
- `sessionManager`: applies to agent runtime pods.
- `sandbox`: applies to sandbox pods.

Each block takes `podLabels` and `podAnnotations`. A per-workload block overrides `common` for the same key, and [manifest patches](advanced-customisations.md) override both.

kora reserves the labels and annotations that identify a pod to SPIRE, the sidecar injector and its StatefulSet, or wire it into the mesh. These include `app`, `sidecar.istio.io/inject`, `proxy.istio.io/config` and keys under `runkora.io/` and `spiffe.io/`. `kora-agents` fails to start when your values set one, and the error shows the key.

## References

- [First-class workload placement](../adr/2026-09-16-first-class-workload-placement.md): the `agents.workloads` settings.
