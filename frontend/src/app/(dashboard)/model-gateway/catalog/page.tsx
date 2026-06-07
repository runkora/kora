import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchVirtualModels, fetchProviderModels, fetchPricing } from '../_services/catalog-server';
import type { ProviderModel, PricingEntryWire } from '../_services/catalog-server';
import { CatalogTab } from './_components/catalog-tab';
import type { SyncSummary } from './_components/sync-status-banner';

function mergeProviderModelPricing(
  providerModels: ProviderModel[],
  pricingEntries: PricingEntryWire[],
): ProviderModel[] {
  const pricingMap = new Map<string, PricingEntryWire>();
  for (const e of pricingEntries) {
    pricingMap.set(`${e.provider}/${e.model_id}`, e);
  }
  return providerModels.map((m) => {
    const pricing = pricingMap.get(`${m.provider}/${m.model_id}`);
    return pricing ? { ...m, pricing } : m;
  });
}

function buildSyncSummary(
  pricingEntries: PricingEntryWire[],
  pricingFetchFailed: boolean,
): SyncSummary {
  if (pricingFetchFailed) {
    return { lastSyncedAt: null, modelsWithPricing: 0, pricingFetchFailed: true, isStale: false };
  }
  if (pricingEntries.length === 0) {
    return { lastSyncedAt: null, modelsWithPricing: 0, pricingFetchFailed: false, isStale: false };
  }
  const latest = pricingEntries.reduce<string | null>((acc, e) => {
    if (!acc) return e.last_synced_at;
    return e.last_synced_at > acc ? e.last_synced_at : acc;
  }, null);
  const isStale = latest ? Date.now() - new Date(latest).getTime() > 25 * 60 * 60 * 1000 : false;
  return {
    lastSyncedAt: latest,
    modelsWithPricing: pricingEntries.length,
    pricingFetchFailed: false,
    isStale,
  };
}

export default async function CatalogPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return (
      <CatalogTab
        initialVirtualModels={[]}
        initialProviderModels={[]}
        initialError="Not authenticated"
        syncSummary={{
          lastSyncedAt: null,
          modelsWithPricing: 0,
          pricingFetchFailed: false,
          isStale: false,
        }}
      />
    );
  }

  const [vmResult, pmResult, pricingResult] = await Promise.all([
    fetchVirtualModels(session.accessToken),
    fetchProviderModels(session.accessToken),
    fetchPricing(session.accessToken),
  ]);

  const virtualModels = vmResult.ok ? (vmResult.data.data ?? []) : [];
  const rawProviderModels = pmResult.ok ? (pmResult.data.data ?? []) : [];
  const pricingEntries = pricingResult.ok ? (pricingResult.data.data ?? []) : [];
  const providerModels = mergeProviderModelPricing(rawProviderModels, pricingEntries);
  const error = !vmResult.ok ? vmResult.message : null;
  const syncSummary = buildSyncSummary(pricingEntries, !pricingResult.ok);

  return (
    <CatalogTab
      initialVirtualModels={virtualModels}
      initialProviderModels={providerModels}
      initialError={error}
      syncSummary={syncSummary}
    />
  );
}
