# Declarative resource examples

These manifests define one of each declarative kind for the local example tenant. Every name starts with `gitops-` to stay clear of items built in the dashboard. [Declarative resources](../../../docs/guides/declarative-resources.md) describes each kind.

- `virtual-model.yaml`: the `VirtualModel` `gitops/sonnet` in `kora-system`, routed to the Bedrock provider `bedrock:us`.
- `agent.yaml`: the `Environment` `gitops-base` on `kora-base` and the autonomous `Agent` `gitops-reviewer`.
- `triggers.yaml`: a labeled Secret, the GitHub `TriggerSource` `gitops-github`, the webhook `Trigger` `gitops-review-pr` and the disabled scheduled `Trigger` `gitops-weekly-summary`.
- `policy-role.yaml`: the `PolicyRole` `gitops-reviewers` with the agent's run scopes and a monthly budget.

The virtual model needs a Bedrock provider from `make cluster-bedrock-creds`. Change `bedrock:us` to `bedrock:<alias>` for another alias in `BEDROCK_ACCOUNTS`. Each provider model can be the route of one virtual model only. If a dashboard-built virtual model already routes to `us.anthropic.claude-sonnet-4-20250514-v1:0`, pick another `modelID`.

Apply them in any order:

```bash
kubectl apply -f deploy/local/declarative/
kubectl get virtualmodels -n kora-system
kubectl get environments,agents.runkora.io,triggersources,triggers.runkora.io,policyroles -n kora-tenant-example
```

Each resource reports `Ready=True` once its references exist. `kubectl delete -f deploy/local/declarative/` removes them.
