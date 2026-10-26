# Declarative resource examples

These manifests define one of each declarative kind for the local example tenant. Every name starts with `gitops-` to stay clear of items built in the dashboard. [Declarative resources](../../../docs/guides/declarative-resources.md) describes each kind.

Each provider model can be the route of one virtual model only. If a dashboard-built virtual model already routes to `us.anthropic.claude-sonnet-5-5`, pick another `modelID`.

Apply them in any order:

```bash
kubectl apply -f deploy/local/declarative/
kubectl get virtualmodels -n kora-system
kubectl get environments,agents.runkora.io,triggersources,triggers.runkora.io,policyroles -n kora-tenant-example
```

Each resource reports `Ready=True` once the resources it references are `Ready`. `kubectl delete -f deploy/local/declarative/` removes them.
