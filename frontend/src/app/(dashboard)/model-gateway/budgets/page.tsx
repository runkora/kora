// RSC — fetches per-principal budget status, roles, grants, and the tenant
// user list (for principal display names) server-side.
// Requires platform:budgets:read; budget edits require platform:budgets:write.

import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchBudgetPrincipalsFromGateway } from '../_services/budgets-server';
import { fetchAllTenantUsers } from '../_services/tokens-server';
import { fetchRoles, fetchGrants } from '../../policy/_services/policy-server';
import type { BudgetPrincipalsView, PrincipalIdentity } from '../_types';
import { BudgetsShell } from './_components/budgets-shell';

export default async function BudgetsPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <BudgetsShell
        principals={null}
        roles={[]}
        grants={[]}
        identities={{}}
        initialError="Session expired. Please sign in again."
      />
    );
  }

  const [principalsResult, rolesResult, grantsResult, usersResult] = await Promise.all([
    fetchBudgetPrincipalsFromGateway(session.accessToken),
    fetchRoles(session.accessToken, session.tenantId),
    fetchGrants(session.accessToken, session.tenantId, 1, 500),
    fetchAllTenantUsers(session.accessToken),
  ]);

  // Identity attribution: principal_id → {email, display_name}, same pattern
  // as the analytics principals BFF route. Best-effort — name resolution
  // failing must not take down the budgets page.
  const identities: Record<string, PrincipalIdentity> = {};
  if (usersResult.ok) {
    for (const u of usersResult.users) {
      identities[u.id] = { email: u.email ?? null, display_name: u.display_name ?? null };
    }
  }

  const principals: BudgetPrincipalsView | null = principalsResult.ok
    ? {
        period: principalsResult.data.period,
        tenant_cap: principalsResult.data.tenant_cap,
        data: principalsResult.data.data.map((row) => ({
          ...row,
          email: identities[row.principal_id]?.email ?? null,
          display_name: identities[row.principal_id]?.display_name ?? null,
        })),
      }
    : null;

  const errors = [
    principalsResult.ok ? null : `Budget status: ${principalsResult.message}`,
    rolesResult.ok ? null : `Roles: ${rolesResult.message}`,
    grantsResult.ok ? null : `Grants: ${grantsResult.message}`,
  ].filter((e): e is string => e !== null);

  return (
    <BudgetsShell
      principals={principals}
      roles={rolesResult.ok ? (rolesResult.data.roles ?? []) : []}
      grants={grantsResult.ok ? (grantsResult.data.grants ?? []) : []}
      identities={identities}
      initialError={errors.length > 0 ? errors.join(' · ') : null}
    />
  );
}
