// RSC — fetches the first page of inference logs and PATs server-side so the
// page renders with real data. The co-located loading.tsx shows a skeleton
// while this fetch completes.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchUsageFromGateway } from '../_services/usage-server';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { LogsTabShell } from '../_components/logs-tab-shell';

export default async function LogsPage({
  searchParams,
}: {
  searchParams: Promise<{ key?: string }>;
}) {
  const { key } = await searchParams;
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <LogsTabShell
        selectedKeyId={key}
        initialRecords={[]}
        initialHasNextPage={false}
        initialNextCursor={null}
        initialPats={[]}
        initialError="Session expired — please sign in again."
      />
    );
  }

  const [usageResult, patsResult] = await Promise.all([
    fetchUsageFromGateway(session.accessToken, { tokenId: key }),
    listPATsFromIdentity(session.accessToken),
  ]);

  const initialRecords = usageResult.ok ? usageResult.records : [];
  const initialHasNextPage = usageResult.ok ? usageResult.hasNextPage : false;
  const initialNextCursor = usageResult.ok ? usageResult.nextCursor : null;
  const initialPats = patsResult.ok ? patsResult.tokens : [];
  const initialError = usageResult.ok ? null : usageResult.message;

  return (
    <LogsTabShell
      selectedKeyId={key}
      initialRecords={initialRecords}
      initialHasNextPage={initialHasNextPage}
      initialNextCursor={initialNextCursor}
      initialPats={initialPats}
      initialError={initialError}
    />
  );
}
