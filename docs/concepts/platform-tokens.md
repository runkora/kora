# Platform tokens

In kora, people, agents, triggers and personal access tokens all authenticate with the same kind of token: a JWT signed by kora's identity service. Every kora service verifies it. Policy evaluates a person's token and an agent's token in the same way. The token identifies its holder and carries no permissions. Policy looks up the holder's grants and roles on every request. See [identity](identity.md) for the identity models these tokens represent.

## The token

A platform token is a short-lived JWT signed by the identity service. Every service verifies its signature against the identity service's published keys before trusting it.

| Claim | Holds |
| --- | --- |
| Subject | Who the token belongs to: a person's identity provider subject, an agent's SPIFFE ID, or a trigger's ID |
| Principal type | `HUMAN`, `AGENT`, `SERVICE` or `PAT` |
| Tenant | The tenant the principal belongs to |
| Identity source | How the identity was proven: federated through an identity provider, attested by SPIFFE, or issued by kora |
| Claims | A person's email and roles from their identity provider, or an agent's SPIFFE ID and, from a sandbox, its run ID |

A person's token, decoded:

```json
{
  "iss": "kora",
  "aud": "kora-platform",
  "sub": "00u1a2b3c4d5",
  "iat": 1792000000,
  "exp": 1792000900,
  "kora": {
    "principal_type": "HUMAN",
    "tenant_id": "kora",
    "identity_source": "FEDERATED",
    "display_name": "Sam Carter",
    "claims": {
      "email": "sam@kora.example",
      "issuer": "https://login.kora.example",
      "roles": "engineers,kora-builder"
    }
  }
}
```

An agent's token, requested from one of its sandboxes:

```json
{
  "iss": "kora",
  "aud": "kora-platform",
  "sub": "spiffe://kora.example/tenant/kora-tenant-kora/agent/coder",
  "iat": 1792000000,
  "exp": 1792000900,
  "kora": {
    "principal_type": "AGENT",
    "tenant_id": "kora",
    "identity_source": "ATTESTED",
    "display_name": "coder",
    "claims": {
      "spiffe_id": "spiffe://kora.example/tenant/kora-tenant-kora/agent/coder/sandbox/7f3e2a10",
      "run_id": "7f3e2a10"
    }
  }
}
```

## How each principal gets a token

| Principal | Gets its token by | Lifetime |
| --- | --- | --- |
| Person | Signing in through the tenant's identity provider. The console keeps the token server-side and refreshes it before it expires. | 15 minutes, refreshed for up to 7 days |
| Agent | Its pod's `identity-proxy` exchanging the pod's SPIFFE SVID with the identity service. The proxy attaches the token to every outbound request. | 15 minutes, re-exchanged before it expires |
| Personal access token | A person creating one with chosen scopes, each covered by their own access | 90 days by default, at most 365 |
| Trigger | The triggers service requesting a token for the trigger or its source | 2 minutes |

An agent never sees its token. The `identity-proxy` sidecar holds the token and adds it to requests as they leave the pod. A sandbox makes its requests as the same agent. kora attributes each of its requests to the run named in its token.

## Delegation

A run of a [delegated agent](identity.md#delegated-agents) carries a second token alongside the agent's platform token. In the delegation token, `sub` is the person who started the run, `act.sub` is the agent, and `run_id` is the run it can be used for. kora mints it when the person starts the run, the `identity-proxy` attaches it to the run's requests, and kora revokes it when the run ends.

```json
{
  "iss": "kora",
  "aud": "kora-platform",
  "sub": "00u1a2b3c4d5",
  "delegator_principal_type": "HUMAN",
  "act": { "sub": "spiffe://kora.example/tenant/kora-tenant-kora/agent/reviewer" },
  "run_id": "9c41d7e2",
  "tenant_id": "kora",
  "iat": 1792000000,
  "exp": 1792003600
}
```

Policy allows a delegated request only when the agent's own grants cover it and the person's live access also covers it. The person's access is checked on every request. Removing a person's access also removes it from their delegated runs. A delegation token for one run cannot be used for another.

## Related decisions

- [Unified identity model](../adr/2026-03-05-unified-identity-model.md)
- [Delegated agent identity](../adr/2026-07-23-delegated-agent-identity.md)
