// RSC — fetches initial quota groups server-side.
// Requires platform:quotas:read scope; enforced at the API layer.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchQuotaGroups } from '../_services/quota-server';
import { QuotasPageClient } from './_components/quotas-page-client';
import type { QuotaGroup } from '../_services/quota-server';

export default async function QuotasPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return <QuotasPageClient initialGroups={[]} initialError="Session expired — please sign in again." />;
  }

  const groupsResult = await fetchQuotaGroups(session.accessToken);

  const initialGroups: QuotaGroup[] = groupsResult.ok ? (groupsResult.data.data ?? []) : [];
  const initialError = groupsResult.ok ? null : groupsResult.message;

  return <QuotasPageClient initialGroups={initialGroups} initialError={initialError} />;
}
