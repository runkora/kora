import type {
  VirtualModel as ApiVirtualModel,
  ProviderModel as ApiProviderModel,
} from '../../_services/catalog-server';
import type { VirtualModel, ProviderModel, Provider, RoutingStrategy } from '../_types/catalog';

// ── API adapters ──────────────────────────────────────────────────────────────

export function toVirtualModel(m: ApiVirtualModel): VirtualModel {
  return {
    id: m.id,
    family: m.family,
    name: m.name,
    displayName: m.display_name,
    description: m.description,
    strategy: m.strategy as RoutingStrategy,
    enabled: m.enabled,
    routes: m.routes.map((r) => ({
      id: r.id,
      provider: r.provider as Provider,
      modelId: r.model_id,
      priority: r.priority,
      weight: r.weight,
      inputCostPer1M: r.input_cost_per_1m,
      outputCostPer1M: r.output_cost_per_1m,
      enabled: r.enabled,
    })),
  };
}

export function toProviderModel(m: ApiProviderModel): ProviderModel {
  return {
    provider: m.provider as Provider,
    modelId: m.model_id,
    displayName: m.display_name,
    family: m.family,
    capabilities: m.capabilities,
    aliases: m.aliases,
    inputModalities: m.input_modalities ?? [],
    outputModalities: m.output_modalities ?? [],
    dialects: m.dialects ?? [],
    enabled: m.enabled,
  };
}

export function vmToApiPayload(vm: VirtualModel, partial?: Partial<VirtualModel>) {
  const merged = { ...vm, ...partial };
  return {
    family: merged.family,
    name: merged.name,
    display_name: merged.displayName,
    description: merged.description,
    strategy: merged.strategy,
    enabled: merged.enabled,
    routes: merged.routes.map((r) => ({
      id: r.id,
      provider: r.provider,
      model_id: r.modelId,
      priority: r.priority,
      weight: r.weight,
      input_cost_per_1m: r.inputCostPer1M,
      output_cost_per_1m: r.outputCostPer1M,
      enabled: r.enabled,
    })),
  };
}

// ── Derived helpers ───────────────────────────────────────────────────────────

export function vmFullId(vm: Pick<VirtualModel, 'family' | 'name'>): string {
  return `${vm.family}/${vm.name}`;
}

export function vmPolicyScope(vm: Pick<VirtualModel, 'family' | 'name'>): string {
  return `inference:${vm.family}:${vm.name}:use`;
}
