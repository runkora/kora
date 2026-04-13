// RSC — fetches usage logs and PATs server-side so the page renders with real
// data. The ?key=<pat-id> query param pre-selects the key filter (deep-link
// from the API Keys page).

import { getSession, isSessionValid } from '@orbit/server/session';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { fetchUsageFromGateway } from '../_services/usage-server';
import { LogsTabShell } from '../_components/logs-tab-shell';

interface LogsPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function LogsPage({ searchParams }: LogsPageProps) {
  const params = await searchParams;
  const selectedKeyId = typeof params.key === 'string' ? params.key : '';

  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <LogsTabShell
        initialRecords={[]}
        initialTotal={0}
        initialPats={[]}
        selectedKeyId={selectedKeyId}
        initialError="Session expired — please sign in again."
      />
    );
  }

  // Fetch PATs and usage in parallel. A PAT fetch failure is non-fatal — logs
  // still render, just without resolved key names.
  //
  // TODO(pagination): fetchUsageFromGateway uses the backend default limit (50).
  // Users with more than 50 records will see a truncated list. The toolbar
  // "N of Y requests" count will reveal the discrepancy. Implement limit/offset
  // controls (or cursor-based pagination) before this page exits beta.
  const [patsResult, usageResult] = await Promise.all([
    listPATsFromIdentity(session.accessToken),
    fetchUsageFromGateway(session.accessToken, {
      tokenId: selectedKeyId || undefined,
    }),
  ]);

  const initialPats = patsResult.ok ? patsResult.tokens : [];
  const initialRecords = usageResult.ok ? usageResult.records : [];
  const initialTotal = usageResult.ok ? usageResult.total : 0;
  const initialError = usageResult.ok ? null : `Failed to load logs: ${usageResult.message}`;

  return (
    <LogsTabShell
      initialRecords={initialRecords}
      initialTotal={initialTotal}
      initialPats={initialPats}
      selectedKeyId={selectedKeyId}
      initialError={initialError}
    />
  );
}
