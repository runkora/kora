import { getSession, isSessionValid } from '@orbit/server/session';
import {
  fetchVirtualModels,
  fetchProviderModels,
  fetchPricing,
} from '../_services/catalog-server';
import type { ProviderModel, PricingEntryWire } from '../_services/catalog-server';
import { CatalogTab } from './_components/catalog-tab';

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

  return (
    <CatalogTab
      initialVirtualModels={virtualModels}
      initialProviderModels={providerModels}
      initialError={error}
    />
  );
}
