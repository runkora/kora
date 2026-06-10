// RSC — fetches per-principal budget status, roles, and grants server-side.
// Requires platform:budgets:read; budget edits require platform:budgets:write.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchBudgetPrincipalsFromGateway } from '../_services/budgets-server';
import { fetchRoles, fetchGrants } from '../../policy/_services/policy-server';
import { BudgetsShell } from './_components/budgets-shell';

export default async function BudgetsPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <BudgetsShell
        principals={null}
        roles={[]}
        grants={[]}
        initialError="Session expired — please sign in again."
      />
    );
  }

  const [principalsResult, rolesResult, grantsResult] = await Promise.all([
    fetchBudgetPrincipalsFromGateway(session.accessToken),
    fetchRoles(session.accessToken, session.tenantId),
    fetchGrants(session.accessToken, session.tenantId, 1, 500),
  ]);

  const errors = [
    principalsResult.ok ? null : `Budget status: ${principalsResult.message}`,
    rolesResult.ok ? null : `Roles: ${rolesResult.message}`,
    grantsResult.ok ? null : `Grants: ${grantsResult.message}`,
  ].filter((e): e is string => e !== null);

  return (
    <BudgetsShell
      principals={principalsResult.ok ? principalsResult.data : null}
      roles={rolesResult.ok ? (rolesResult.data.roles ?? []) : []}
      grants={grantsResult.ok ? (grantsResult.data.grants ?? []) : []}
      initialError={errors.length > 0 ? errors.join(' · ') : null}
    />
  );
}
