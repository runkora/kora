/**
 * Mock gateway data for the Model Gateway page stories: a realistic kora
 * workspace with models behind several providers, plus the catalog and
 * analytics payloads the Catalog and Overview pages render.
 */

import type {
  APIDialect,
  AnalyticsBucket,
  AnalyticsCostByModel,
  AnalyticsKPIs,
  InferenceUsageRecord,
  Modality,
  ModelCapability,
  ModelObject,
  ModelObjectPricing,
} from '../_types';
import { toModelRow } from '../_types';
import type { PricingEntryWire, ProviderModel, VirtualModel } from '../_services/catalog-server';

// ── Models ────────────────────────────────────────────────────────────────────

interface ModelSeed {
  id: string;
  name: string;
  provider: string;
  family: string;
  aliases?: string[];
  capabilities?: ModelCapability[];
  dialects?: APIDialect[];
  input?: Modality[];
  output?: Modality[];
  pricing?: ModelObjectPricing;
  status?: ModelObject['status'];
}

const CREATED = 1_758_000_000;

/** Fills the fields every chat model shares so each entry only states what differs. */
function model(seed: ModelSeed): ModelObject {
  return {
    id: seed.id,
    object: 'model',
    created: CREATED,
    owned_by: seed.provider,
    family: seed.family,
    display_name: seed.name,
    status: seed.status ?? 'available',
    aliases: seed.aliases ?? [],
    input_modalities: seed.input ?? ['text', 'image'],
    output_modalities: seed.output ?? ['text'],
    dialects: seed.dialects ?? ['openai', 'anthropic'],
    capabilities: seed.capabilities ?? ['chat'],
    ...(seed.pricing !== undefined ? { pricing: seed.pricing } : {}),
  };
}

const tokens = (input: number, output: number, cacheRead?: number): ModelObjectPricing => ({
  effective_input_cost_per_1m: input,
  effective_output_cost_per_1m: output,
  ...(cacheRead !== undefined
    ? { cache_read_cost_per_1m: cacheRead, cache_write_cost_per_1m: input * 1.25 }
    : {}),
});

export const MODEL_OBJECTS: ModelObject[] = [
  model({
    id: 'anthropic/claude-opus-4-1',
    name: 'Claude Opus 4.1',
    provider: 'anthropic',
    family: 'anthropic',
    aliases: ['opus'],
    pricing: tokens(15, 75, 1.5),
  }),
  model({
    id: 'anthropic/claude-sonnet-4-6',
    name: 'Claude Sonnet 4.6',
    provider: 'anthropic',
    family: 'anthropic',
    aliases: ['sonnet', 'claude-latest'],
    pricing: tokens(3, 15, 0.3),
  }),
  model({
    id: 'anthropic/claude-haiku-4-5',
    name: 'Claude Haiku 4.5',
    provider: 'anthropic',
    family: 'anthropic',
    aliases: ['haiku'],
    pricing: tokens(1, 5, 0.1),
  }),
  model({
    id: 'bedrock/claude-sonnet-4-5-20250929-v1',
    name: 'Claude Sonnet 4.5 (Bedrock)',
    provider: 'bedrock',
    family: 'anthropic',
    pricing: tokens(3, 15, 0.3),
  }),
  model({
    id: 'bedrock/claude-haiku-4-5-20251001-v1',
    name: 'Claude Haiku 4.5 (Bedrock)',
    provider: 'bedrock',
    family: 'anthropic',
    pricing: tokens(1, 5, 0.1),
  }),
  model({
    id: 'bedrock/amazon-nova-pro-v1',
    name: 'Amazon Nova Pro',
    provider: 'bedrock',
    family: 'amazon',
    dialects: ['openai'],
    input: ['text', 'image', 'video'],
    pricing: tokens(0.8, 3.2),
  }),
  model({
    id: 'bedrock/amazon-nova-lite-v1',
    name: 'Amazon Nova Lite',
    provider: 'bedrock',
    family: 'amazon',
    dialects: ['openai'],
    input: ['text', 'image', 'video'],
    pricing: tokens(0.06, 0.24),
  }),
  model({
    id: 'bedrock/llama-3-3-70b-instruct',
    name: 'Llama 3.3 70B Instruct',
    provider: 'bedrock',
    family: 'meta',
    dialects: ['openai'],
    input: ['text'],
    pricing: tokens(0.72, 0.72),
  }),
  model({
    id: 'bedrock/titan-embed-text-v2',
    name: 'Titan Text Embeddings v2',
    provider: 'bedrock',
    family: 'amazon',
    capabilities: ['embedding'],
    dialects: [],
    input: ['text'],
    output: ['embedding'],
    pricing: tokens(0.02, 0),
  }),
  model({
    id: 'vertexai/gemini-2-5-pro',
    name: 'Gemini 2.5 Pro',
    provider: 'vertexai',
    family: 'google',
    aliases: ['gemini-pro'],
    dialects: ['openai'],
    input: ['text', 'image', 'audio', 'video'],
    pricing: tokens(1.25, 10, 0.31),
  }),
  model({
    id: 'vertexai/gemini-2-5-flash',
    name: 'Gemini 2.5 Flash',
    provider: 'vertexai',
    family: 'google',
    aliases: ['gemini-flash'],
    dialects: ['openai'],
    input: ['text', 'image', 'audio', 'video'],
    pricing: tokens(0.3, 2.5, 0.075),
  }),
  model({
    id: 'vertexai/imagen-4',
    name: 'Imagen 4',
    provider: 'vertexai',
    family: 'google',
    capabilities: ['image'],
    dialects: [],
    input: ['text'],
    output: ['image'],
    pricing: { non_token_only: true, image_output_cost: 0.04 },
  }),
  model({
    id: 'vertexai/text-embedding-005',
    name: 'Text Embedding 005',
    provider: 'vertexai',
    family: 'google',
    capabilities: ['embedding'],
    dialects: [],
    input: ['text'],
    output: ['embedding'],
    pricing: tokens(0.025, 0),
  }),
  model({
    id: 'azure/gpt-5',
    name: 'GPT-5',
    provider: 'azure',
    family: 'openai',
    aliases: ['gpt'],
    dialects: ['openai', 'responses'],
    pricing: tokens(1.25, 10, 0.125),
  }),
  model({
    id: 'azure/gpt-5-mini',
    name: 'GPT-5 mini',
    provider: 'azure',
    family: 'openai',
    dialects: ['openai', 'responses'],
    pricing: tokens(0.25, 2, 0.025),
  }),
  model({
    id: 'azure/gpt-4-1',
    name: 'GPT-4.1',
    provider: 'azure',
    family: 'openai',
    dialects: ['openai', 'responses'],
    pricing: tokens(2, 8, 0.5),
  }),
  model({
    id: 'azure/text-embedding-3-large',
    name: 'Text Embedding 3 Large',
    provider: 'azure',
    family: 'openai',
    capabilities: ['embedding'],
    dialects: [],
    input: ['text'],
    output: ['embedding'],
    pricing: tokens(0.13, 0),
  }),
  model({
    id: 'azure/whisper-1',
    name: 'Whisper',
    provider: 'azure',
    family: 'openai',
    capabilities: ['audio'],
    dialects: [],
    input: ['audio'],
    output: ['text'],
    pricing: { non_token_only: true, second_input_cost: 0.0001 },
  }),
  model({
    id: 'vllm/qwen3-coder-30b',
    name: 'Qwen3 Coder 30B (self-hosted)',
    provider: 'openai-compatible',
    family: 'alibaba',
    aliases: ['qwen-coder'],
    dialects: ['openai'],
    input: ['text'],
    pricing: tokens(0, 0),
  }),
  model({
    id: 'vllm/llama-3-1-8b-instruct',
    name: 'Llama 3.1 8B Instruct (self-hosted)',
    provider: 'openai-compatible',
    family: 'meta',
    dialects: ['openai'],
    input: ['text'],
    pricing: tokens(0, 0),
    status: 'unavailable',
  }),
  model({
    id: 'bedrock/mistral-large-2407-v1',
    name: 'Mistral Large 2',
    provider: 'bedrock',
    family: 'mistral',
    dialects: ['openai'],
    input: ['text'],
    status: 'unavailable',
  }),
  model({
    id: 'azure/gpt-4o-mini',
    name: 'GPT-4o mini',
    provider: 'azure',
    family: 'openai',
    dialects: ['openai', 'responses'],
    pricing: tokens(0.15, 0.6, 0.075),
  }),
];

/** Rows as the page receives them from the server component. */
export const MODEL_ROWS = MODEL_OBJECTS.map(toModelRow);

/** Extra rows so the table has to paginate and scroll. */
export const MANY_MODEL_ROWS = [
  ...MODEL_ROWS,
  ...Array.from({ length: 40 }, (_, i) => {
    const n = i + 1;
    return toModelRow(
      model({
        id: `vllm/team-finetune-${String(n).padStart(2, '0')}`,
        name: `Team fine-tune ${String(n).padStart(2, '0')}`,
        provider: 'openai-compatible',
        family: i % 2 === 0 ? 'meta' : 'alibaba',
        dialects: ['openai'],
        input: ['text'],
        pricing: tokens(0, 0),
        status: i % 9 === 0 ? 'unavailable' : 'available',
      }),
    );
  }),
];

// ── Catalog ───────────────────────────────────────────────────────────────────

/** Provider models the catalog lists, derived from the chat models above. */
export const PROVIDER_MODELS: ProviderModel[] = MODEL_OBJECTS.map((m) => ({
  provider: m.owned_by === 'anthropic' ? 'bedrock' : m.owned_by,
  model_id: m.id.split('/')[1] ?? m.id,
  display_name: m.display_name,
  family: m.family,
  capabilities: m.capabilities,
  aliases: m.aliases,
  input_modalities: m.input_modalities,
  output_modalities: m.output_modalities,
  dialects: m.dialects,
  enabled: m.status === 'available',
}));

const syncedAt = '2026-10-01T02:00:00Z';

/** Synced pricing for every provider model that has token prices. */
export const PRICING_ENTRIES: PricingEntryWire[] = MODEL_OBJECTS.flatMap((m) => {
  const p = m.pricing;
  if (p === undefined || p.effective_input_cost_per_1m === undefined) return [];
  const input = p.effective_input_cost_per_1m;
  const output = p.effective_output_cost_per_1m ?? 0;
  const overridden = m.id === 'azure/gpt-5';
  return [
    {
      provider: m.owned_by === 'anthropic' ? 'bedrock' : m.owned_by,
      model_id: m.id.split('/')[1] ?? m.id,
      synced_input_cost_per_1m: overridden ? 1.5 : input,
      synced_output_cost_per_1m: overridden ? 12 : output,
      synced_cache_read_cost_per_1m: p.cache_read_cost_per_1m ?? 0,
      synced_cache_write_cost_per_1m: p.cache_write_cost_per_1m ?? 0,
      override_input_cost_per_1m: overridden ? input : null,
      override_output_cost_per_1m: overridden ? output : null,
      override_cache_read_cost_per_1m: null,
      override_cache_write_cost_per_1m: null,
      effective_input_cost_per_1m: input,
      effective_output_cost_per_1m: output,
      has_override: overridden,
      last_synced_at: syncedAt,
    },
  ];
});

const route = (
  id: string,
  provider: string,
  modelId: string,
  priority: number,
  weight: number,
  input: number,
  output: number,
) => ({
  id,
  provider,
  model_id: modelId,
  priority,
  weight,
  input_cost_per_1m: input,
  output_cost_per_1m: output,
  enabled: true,
});

export const VIRTUAL_MODELS: VirtualModel[] = [
  {
    id: 'vm_sonnet',
    family: 'anthropic',
    name: 'claude-sonnet-4-6',
    display_name: 'Claude Sonnet 4.6',
    description: 'Default coding model. Bedrock first, direct Anthropic as the fallback.',
    strategy: 'priority',
    enabled: true,
    created_at: '2026-08-12T01:00:00Z',
    updated_at: '2026-09-22T05:12:00Z',
    routes: [
      route('r1', 'bedrock:us', 'claude-sonnet-4-5-20250929-v1', 1, 100, 3, 15),
      route('r2', 'bedrock:eu', 'claude-sonnet-4-5-20250929-v1', 2, 100, 3, 15),
    ],
    pricing: { effective_input_cost_per_1m: 3, effective_output_cost_per_1m: 15 },
  },
  {
    id: 'vm_haiku',
    family: 'anthropic',
    name: 'claude-haiku-4-5',
    display_name: 'Claude Haiku 4.5',
    description: 'Fast and cheap model for triage and summaries.',
    strategy: 'lowest_cost',
    enabled: true,
    created_at: '2026-08-12T01:05:00Z',
    updated_at: '2026-09-02T03:40:00Z',
    routes: [route('r3', 'bedrock:us', 'claude-haiku-4-5-20251001-v1', 1, 100, 1, 5)],
    pricing: { effective_input_cost_per_1m: 1, effective_output_cost_per_1m: 5 },
  },
  {
    id: 'vm_gpt5',
    family: 'openai',
    name: 'gpt-5',
    display_name: 'GPT-5',
    description: 'Azure OpenAI deployment shared by the data platform team.',
    strategy: 'weighted',
    enabled: true,
    created_at: '2026-08-20T07:30:00Z',
    updated_at: '2026-09-28T23:10:00Z',
    routes: [
      route('r4', 'azure', 'gpt-5', 1, 70, 1.25, 10),
      route('r5', 'azure', 'gpt-5-eastus2', 1, 30, 1.25, 10),
    ],
    pricing: { effective_input_cost_per_1m: 1.25, effective_output_cost_per_1m: 10 },
  },
  {
    id: 'vm_gemini',
    family: 'google',
    name: 'gemini-2-5-flash',
    display_name: 'Gemini 2.5 Flash',
    description: 'Long-context document review.',
    strategy: 'priority',
    enabled: false,
    created_at: '2026-09-05T04:00:00Z',
    updated_at: '2026-09-05T04:00:00Z',
    routes: [route('r6', 'vertexai', 'gemini-2-5-flash', 1, 100, 0.3, 2.5)],
    pricing: { effective_input_cost_per_1m: 0.3, effective_output_cost_per_1m: 2.5 },
  },
];

// ── Overview ──────────────────────────────────────────────────────────────────

/** Thirty daily buckets ending the day before the mock "today" (2026-10-01). */
export const ANALYTICS_BUCKETS: AnalyticsBucket[] = Array.from({ length: 30 }, (_, i) => {
  const day = new Date(Date.UTC(2026, 8, 1 + i));
  const weekend = day.getUTCDay() === 0 || day.getUTCDay() === 6;
  const requests = Math.round((weekend ? 5200 : 12800) + ((i * 937) % 2100));
  const failed = Math.round(requests * (0.012 + ((i * 7) % 5) / 1000));
  const prompt = requests * 1450;
  return {
    date: day.toISOString().slice(0, 10),
    requests,
    successful_requests: requests - failed,
    failed_requests: failed,
    prompt_tokens: prompt,
    completion_tokens: Math.round(prompt * 0.22),
    cache_read_tokens: Math.round(prompt * 0.6),
    cache_creation_tokens: Math.round(prompt * 0.05),
    estimated_cost_usd: Math.round(requests * 0.0094 * 100) / 100,
    p50_latency_ms: 880 + ((i * 53) % 140),
    p95_latency_ms: 3100 + ((i * 97) % 600),
    p99_latency_ms: 6400 + ((i * 131) % 900),
  };
});

const sum = (key: keyof AnalyticsBucket) =>
  ANALYTICS_BUCKETS.reduce((total, b) => total + Number(b[key]), 0);

export const ANALYTICS_KPIS: AnalyticsKPIs = {
  total_requests: sum('requests'),
  total_tokens: sum('prompt_tokens') + sum('completion_tokens'),
  prompt_tokens: sum('prompt_tokens'),
  completion_tokens: sum('completion_tokens'),
  cache_read_tokens: sum('cache_read_tokens'),
  cache_creation_tokens: sum('cache_creation_tokens'),
  estimated_cost_usd: sum('estimated_cost_usd'),
  avg_latency_ms: 1120,
  p95_latency_ms: 3380,
  p99_latency_ms: 6900,
  error_rate: 0.014,
  cache_hit_rate: 0.58,
};

export const COST_BY_MODEL: AnalyticsCostByModel[] = [
  { model_id: 'anthropic/claude-sonnet-4-6', estimated_cost_usd: 1840.2 },
  { model_id: 'anthropic/claude-opus-4-1', estimated_cost_usd: 912.55 },
  { model_id: 'azure/gpt-5', estimated_cost_usd: 504.1 },
  { model_id: 'anthropic/claude-haiku-4-5', estimated_cost_usd: 188.75 },
  { model_id: 'vertexai/gemini-2-5-flash', estimated_cost_usd: 61.3 },
];

const usage = (
  n: number,
  model: string,
  latency: number,
  minutesAgo: number,
  status: InferenceUsageRecord['status'] = 'success',
): InferenceUsageRecord => ({
  id: `use_${n}`,
  tenant_id: 'ten_kora',
  principal_id: 'agt_code_reviewer',
  principal_type: 'agent',
  token_id: null,
  api_dialect: 'anthropic',
  provider: 'bedrock',
  virtual_model_id: model,
  model_id: model.split('/')[1] ?? model,
  model_family: model.split('/')[0] ?? null,
  routing_strategy: 'priority',
  prompt_tokens: 18400,
  completion_tokens: 920,
  total_tokens: 19320,
  cache_read_input_tokens: 15200,
  cache_creation_input_tokens: 0,
  estimated_cost_usd: 0.021,
  status,
  retry_count: 0,
  error_code: status === 'success' ? null : status,
  failure_details: null,
  latency_ms: latency,
  created_at: new Date(Date.UTC(2026, 9, 1, 3, 30 - minutesAgo)).toISOString(),
});

export const RECENT_USAGE: InferenceUsageRecord[] = [
  usage(1, 'anthropic/claude-sonnet-4-6', 2140, 1),
  usage(2, 'anthropic/claude-haiku-4-5', 640, 2),
  usage(3, 'azure/gpt-5', 4820, 4, 'provider_error'),
  usage(4, 'anthropic/claude-sonnet-4-6', 1890, 5),
  usage(5, 'anthropic/claude-opus-4-1', 7310, 9),
  usage(6, 'anthropic/claude-sonnet-4-6', 2010, 11),
  usage(7, 'vertexai/gemini-2-5-flash', 980, 14),
  usage(8, 'anthropic/claude-haiku-4-5', 570, 15),
];
