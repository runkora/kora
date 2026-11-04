// RSC — fetches models server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchModelsFromGateway } from '../_services/models-server';
import { ModelsTabShell } from '../_components/models-tab-shell';

export default async function ModelsPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchModelsFromGateway(session.accessToken) : null;
  const initialModels = result?.ok ? result.models : [];
  const initialError = result && !result.ok ? result.message : null;

  return <ModelsTabShell initialModels={initialModels} initialError={initialError} />;
}
