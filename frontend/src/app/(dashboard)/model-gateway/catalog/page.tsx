import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchVirtualModels, fetchProviderModels } from '../_services/catalog-server';
import { CatalogTab } from './_components/catalog-tab';

export default async function CatalogPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return (
      <CatalogTab
        initialVirtualModels={[]}
        initialProviderModels={[]}
        initialError="Not authenticated"
      />
    );
  }

  const [vmResult, pmResult] = await Promise.all([
    fetchVirtualModels(session.accessToken),
    fetchProviderModels(session.accessToken),
  ]);

  const virtualModels = vmResult.ok ? (vmResult.data.data ?? []) : [];
  const providerModels = pmResult.ok ? (pmResult.data.data ?? []) : [];
  const error = !vmResult.ok ? vmResult.message : null;

  return (
    <CatalogTab
      initialVirtualModels={virtualModels}
      initialProviderModels={providerModels}
      initialError={error}
    />
  );
}
