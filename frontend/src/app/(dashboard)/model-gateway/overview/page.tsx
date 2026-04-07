import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchModelsFromGateway } from '../_services/models-server';
import { OverviewTab } from '../_components/overview-tab';
import type { ModelRow } from '../_types';

export default async function OverviewPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchModelsFromGateway(session.accessToken)
    : null;
  const models: ModelRow[] = result?.ok ? result.models : [];

  return <OverviewTab models={models} />;
}
