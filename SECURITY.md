# Security policy

Report a suspected vulnerability in kora privately, through a GitHub security advisory or by email. Do not open a public issue, pull request or discussion about it.

## Supported versions

Security fixes go to `main` and the latest minor release. A minor release stops receiving fixes when the next one ships.

| Version | Supported |
| --- | --- |
| `main` | Yes |
| Latest minor release | Yes |
| Earlier releases | No |

## Report a vulnerability

Open a report with **Report a vulnerability** on the [Security Advisories page](https://github.com/runkora/kora/security/advisories). The report stays private between you and the maintainers until a fix is published. You can also email **security@runkora.io**.

Include:

- the affected component, and the kora version or commit
- steps to reproduce, or a proof of concept
- what an attacker could do with it
- a suggested fix, if you have one

## Scope

kora issues identities and credentials to agents and enforces the policy around them. These areas are the most sensitive:

| Area | Examples |
| --- | --- |
| Identity | The identity service, platform and delegation tokens, the `identity-proxy`, SPIFFE attestation |
| Policy | A request allowed without a matching grant, a delegated request that exceeds the person's access, a bypass of the OPA sidecars |
| Credentials | The egress gateway and connections, credentials exposed in logs, events or an agent's context |
| Sandboxes | Escaping a sandbox, commands reaching `sandbox-exec` from an unauthorised caller, one run reaching another run's workspace |
| Tenancy | One tenant reaching another tenant's agents, runs, budgets or data |
| Supply chain | Published images, Helm charts and release artifacts |

## Disclosure

When a fix is released, the maintainers publish a GitHub security advisory with the affected versions, the impact and the fix. The advisory credits the reporter unless they ask not to be named.

To hear about advisories, watch the repository with **Custom** → **Security alerts** selected.
