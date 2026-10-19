import type { ModelObject } from '@runkora/shared/api';
import { AGENTS, DELIVERIES, FAILED_DELIVERY } from './triggers-mock-data';
import type { DeliveryRow } from '../_types';
import type { RoutingAgent } from './trigger-wizard/step-target-proposed';
import type { RoutingDecision } from './delivery-routing-proposed';

/** Mock data for the proposed "Let kora choose" stories. */

// ── Agents ───────────────────────────────────────────────────────────────────

const DESCRIPTIONS: Record<string, string> = {
  'code-reviewer':
    'Reviews pull requests for correctness, tests and readability, and leaves inline comments.',
  coder: 'Implements a change from an issue or a failing check and opens a pull request for it.',
  'security-reviewer':
    'Reviews changes that touch auth, secrets or dependencies, and flags security risks.',
  'docs-writer': 'Writes docs.',
  'triage-bot': 'Labels new issues, finds duplicates and assigns an owner from CODEOWNERS.',
};

const EXTRA: [string, string, string][] = [
  [
    'release-manager',
    'Release Manager',
    'Drafts release notes from merged pull requests and tags a release candidate.',
  ],
  [
    'dependency-bot',
    'Dependency Bot',
    'Upgrades dependencies, reads changelogs and opens one pull request per upgrade.',
  ],
  [
    'flaky-test-hunter',
    'Flaky Test Hunter',
    'Reruns failed checks, spots flaky tests and quarantines them with an issue.',
  ],
  [
    'incident-scribe',
    'Incident Scribe',
    'Summarises an incident channel into a timeline and a follow-up list.',
  ],
  [
    'perf-analyst',
    'Perf Analyst',
    'Profiles slow endpoints from a benchmark run and proposes a fix.',
  ],
  [
    'migration-checker',
    'Migration Checker',
    'Checks schema migrations for locks, backfills and rollback safety.',
  ],
];

/**
 * The trigger wizard's agents with the description routing reads. `count` caps
 * the routable list; the delegated agent and one agent without a run grant are
 * always included, since both must show disabled.
 */
export function routingAgents(
  count: number,
  descriptions: Record<string, string> = {},
): RoutingAgent[] {
  const base: RoutingAgent[] = AGENTS.filter((a) => a.identityMode !== 'delegated').map((a) => ({
    ...a,
    description: descriptions[a.slug] ?? DESCRIPTIONS[a.slug],
    canRun: true,
  }));
  const extra: RoutingAgent[] = EXTRA.map(([slug, name, description], i) => ({
    id: `at_r${String(i).padStart(2, '0')}`,
    slug,
    name,
    identityMode: 'autonomous',
    description: descriptions[slug] ?? description,
    canRun: true,
  }));
  const blocked: RoutingAgent = {
    id: 'at_noperm01',
    slug: 'billing-agent',
    name: 'Billing Agent',
    identityMode: 'autonomous',
    description: 'Reconciles invoices against usage and raises disputes with the vendor.',
    canRun: false,
  };
  const delegated = AGENTS.filter((a) => a.identityMode === 'delegated').map((a) => ({
    ...a,
    canRun: true,
  }));
  return [...[...base, ...extra].slice(0, count), blocked, ...delegated];
}

/** The ids of the first `n` agents, as preselected candidates. */
export const firstIds = (agents: RoutingAgent[], n: number) => agents.slice(0, n).map((a) => a.id);

// ── Model catalog ────────────────────────────────────────────────────────────

function catalogModel(id: string, name: string, capability: 'decision' | 'chat'): ModelObject {
  return {
    id,
    object: 'model',
    created: 1_758_000_000,
    owned_by: id.split('/')[0] ?? id,
    family: id.split('/')[0] ?? id,
    display_name: name,
    status: 'available',
    aliases: [],
    input_modalities: ['text'],
    output_modalities: ['text'],
    dialects: capability === 'chat' ? ['openai', 'anthropic'] : [],
    capabilities: [capability],
  };
}

const CHAT_MODELS: ModelObject[] = [
  catalogModel('anthropic/claude-sonnet-4-6', 'Claude Sonnet 4.6', 'chat'),
  catalogModel('anthropic/claude-opus-4-1', 'Claude Opus 4.1', 'chat'),
  catalogModel('openai/gpt-5', 'GPT-5', 'chat'),
];

const DECISION_MODELS: ModelObject[] = [
  catalogModel('typesafe/jev-1-14', 'Jev 1.14', 'decision'),
  catalogModel('cloudflare/clef', 'Clef', 'decision'),
  catalogModel('cloudflare/clef-flash', 'Clef Flash', 'decision'),
  catalogModel('cloudflare/clef-omni', 'Clef Omni', 'decision'),
  catalogModel('liquid/d1', 'Liquid d1', 'decision'),
  catalogModel('liquid/d1-ultra', 'Liquid d1 Ultra', 'decision'),
  catalogModel('upstage/solar-decide', 'Solar Decide', 'decision'),
  catalogModel('upstage/solar-decide-flash', 'Solar Decide Flash', 'decision'),
  catalogModel('perplexity/pplx-decider-v1-2', 'Perplexity Decider V1.2', 'decision'),
  catalogModel('perplexity/pplx-decider-flash-v1-2', 'Perplexity Decider Flash V1.2', 'decision'),
];

/** What `GET /api/models` returns: chat models the picker must drop, plus the decision models. */
export function modelCatalog(withDecisionModels: boolean): ModelObject[] {
  return withDecisionModels
    ? [
        ...CHAT_MODELS.slice(0, 1),
        ...DECISION_MODELS.slice(0, 5),
        ...CHAT_MODELS.slice(1, 2),
        ...DECISION_MODELS.slice(5),
        ...CHAT_MODELS.slice(2),
      ]
    : CHAT_MODELS;
}

// ── Routed deliveries ────────────────────────────────────────────────────────

export type RoutingCase = 'confident' | 'joined-by-key' | 'fallback' | 'no-run' | 'model-failed';

const BASE: DeliveryRow =
  DELIVERIES.find((d) => d.status === 'dispatched' && d.payloadExcerpt !== null) ?? FAILED_DELIVERY;

const LIQUID = { id: 'liquid/d1', name: 'Liquid d1' };
const BALANCED = { preset: 'Balanced' as const, value: 0.7 };
const KEY = 'acme/platform#1427';
const CONFIDENT_SCORES = [
  { agentSlug: 'code-reviewer', agentName: 'Code Reviewer', probability: 0.91 },
  { agentSlug: 'security-reviewer', agentName: 'Security Reviewer', probability: 0.06 },
  { agentSlug: 'coder', agentName: 'Coder', probability: 0.03 },
];
const UNSURE_SCORES = [
  { agentSlug: 'coder', agentName: 'Coder', probability: 0.48 },
  { agentSlug: 'code-reviewer', agentName: 'Code Reviewer', probability: 0.37 },
  { agentSlug: 'security-reviewer', agentName: 'Security Reviewer', probability: 0.15 },
];

/** One routed delivery and the decision recorded for it. */
export function routedDelivery(c: RoutingCase): {
  delivery: DeliveryRow;
  decision: RoutingDecision;
} {
  const runId = {
    confident: 'run_7c21e4b2',
    'joined-by-key': 'run_3f9c1ab04d',
    fallback: 'run_a90d33b2',
    'no-run': null,
    'model-failed': 'run_51be07b2',
  }[c];
  const status = runId === null ? 'skipped' : 'dispatched';
  const delivery: DeliveryRow = {
    ...BASE,
    status,
    runId,
    reason:
      runId === null
        ? 'The decision model was not sure which agent fits, and no fallback is set.'
        : null,
    runs: BASE.runs.map((r) => ({ ...r, runId, status })),
  };
  const common: RoutingDecision = {
    scores: CONFIDENT_SCORES,
    agent: { slug: 'code-reviewer', name: 'Code Reviewer' },
    run: runId === null ? null : { kind: 'new', runId },
    joinKey: KEY,
    keyMatched: false,
    fallbackFired: false,
    threshold: BALANCED,
    model: LIQUID,
    modelError: null,
  };
  const decisions: Record<RoutingCase, RoutingDecision> = {
    confident: common,
    'joined-by-key': {
      ...common,
      scores: [],
      run: { kind: 'joined', runId: runId ?? '' },
      keyMatched: true,
      model: null,
    },
    fallback: {
      ...common,
      scores: UNSURE_SCORES,
      agent: { slug: 'triage-bot', name: 'Triage Bot' },
      fallbackFired: true,
    },
    'no-run': { ...common, scores: UNSURE_SCORES, agent: null, run: null },
    'model-failed': {
      ...common,
      scores: [],
      agent: { slug: 'triage-bot', name: 'Triage Bot' },
      fallbackFired: true,
      modelError: 'decide: liquid/d1: context deadline exceeded after 10s',
    },
  };
  return { delivery, decision: decisions[c] };
}
