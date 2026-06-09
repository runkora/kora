// RSC — fetches initial quota roles server-side.
// Requires platform:quotas:read scope; enforced at the API layer.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchQuotaRoles } from '../_services/quota-server';
import { QuotasPageClient } from './_components/quotas-page-client';
import type { QuotaRole } from '../_services/quota-server';

export default async function QuotasPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <QuotasPageClient initialRoles={[]} initialError="Session expired — please sign in again." />
    );
  }

  const rolesResult = await fetchQuotaRoles(session.accessToken);

  const initialRoles: QuotaRole[] = rolesResult.ok ? (rolesResult.data.data ?? []) : [];
  const initialError = rolesResult.ok ? null : rolesResult.message;

  return <QuotasPageClient initialRoles={initialRoles} initialError={initialError} />;
}
