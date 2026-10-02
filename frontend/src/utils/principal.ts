/**
 * The agent name at the end of an agent's SPIFFE ID, for example
 * `spiffe://orbit.test/tenant/orbit-tenant-example/agent/ci-failure-investigator`
 * gives `ci-failure-investigator`. Every agent ID shares the same long prefix,
 * so the last segment is the only part that tells two agents apart. Returns
 * undefined for anything that is not a SPIFFE ID with a path.
 */
export function agentNameFromPrincipalId(principalId: string): string | undefined {
  if (!principalId.startsWith('spiffe://')) return undefined;
  const segments = principalId
    .slice('spiffe://'.length)
    .split('/')
    .filter((segment) => segment.length > 0);
  // The first segment is the trust domain; a name needs at least one path segment.
  return segments.length > 1 ? segments[segments.length - 1] : undefined;
}
