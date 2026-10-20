import { describe, expect, it } from 'vitest';

import type { ModelObject } from '@runkora/shared/api';
import {
  DEFAULT_ROUTING,
  decisionModelsOf,
  initialState,
  routingRowOf,
  toDraft,
  toRoutingAgent,
  validateRouting,
  validateTarget,
} from './wizard-state';
import { TRIGGER_KINDS } from './kinds';
import type { RoutingDraft, RoutingOptions, WizardContext } from './types';
import { toRoutingRequest, toRoutingRow, toTriggerRow } from '../../_types';
import type { Trigger } from '@runkora/shared/api';

function model(
  id: string,
  capabilities: ModelObject['capabilities'],
  status: ModelObject['status'] = 'available',
): ModelObject {
  return {
    id,
    object: 'model',
    created: 0,
    owned_by: 'x',
    family: 'x',
    display_name: id === 'liquid/d1' ? 'Liquid d1' : '',
    status,
    aliases: [],
    input_modalities: ['text'],
    output_modalities: ['text'],
    dialects: [],
    capabilities,
  };
}

const AGENTS = [
  toRoutingAgent({ id: 'a1', slug: 'coder', name: 'Coder', description: '', runnable: true }),
  toRoutingAgent({ id: 'a2', slug: 'reviewer', name: 'Reviewer', description: '', runnable: true }),
];

const READY: RoutingOptions = {
  candidates: { status: 'ready', items: AGENTS },
  models: { status: 'ready', items: [{ id: 'liquid/d1', name: 'Liquid d1' }] },
};

const CHOSEN: RoutingDraft = {
  ...DEFAULT_ROUTING,
  mode: 'choose',
  candidateIds: ['a1', 'a2'],
  modelId: 'liquid/d1',
  fallbackId: 'a1',
};

describe('decisionModelsOf', () => {
  it('keeps only available models with the decision capability', () => {
    const models = decisionModelsOf([
      model('anthropic/claude-sonnet-4-6', ['chat']),
      model('liquid/d1', ['decision']),
      model('liquid/d1-old', ['decision'], 'unavailable'),
      model('mixed/m1', ['chat', 'decision']),
    ]);
    expect(models).toEqual([
      { id: 'liquid/d1', name: 'Liquid d1' },
      { id: 'mixed/m1', name: 'mixed/m1' },
    ]);
  });

  it('skips a model the gateway lists without capabilities', () => {
    const bare = {
      ...model('openai/gpt-5-5', []),
      capabilities: undefined,
    } as unknown as ModelObject;
    expect(decisionModelsOf([bare, model('liquid/d1', ['decision'])])).toEqual([
      { id: 'liquid/d1', name: 'Liquid d1' },
    ]);
  });
});

describe('validateRouting', () => {
  it('needs two candidates, a model and a fallback among the candidates', () => {
    expect(validateRouting({ ...CHOSEN, candidateIds: ['a1'] }, READY).valid).toBe(false);
    expect(validateRouting({ ...CHOSEN, modelId: '' }, READY).hint).toBe(
      'Choose a decision model.',
    );
    expect(validateRouting({ ...CHOSEN, fallbackId: 'other' }, READY).hint).toBe(
      'Choose the fallback agent.',
    );
    expect(validateRouting({ ...CHOSEN, unsure: 'none', fallbackId: '' }, READY).valid).toBe(true);
    expect(validateRouting(CHOSEN, READY).valid).toBe(true);
  });

  it('blocks Continue without a hint when the catalog has no decision model', () => {
    const none: RoutingOptions = { ...READY, models: { status: 'ready', items: [] } };
    expect(validateRouting(CHOSEN, none)).toEqual({ valid: false });
    expect(validateRouting({ ...CHOSEN, candidateIds: [] }, none)).toEqual({ valid: false });
  });

  it('refuses a join key over 256 bytes', () => {
    expect(validateRouting({ ...CHOSEN, joinKey: 'k'.repeat(257) }, READY).valid).toBe(false);
    expect(
      validateRouting({ ...CHOSEN, joinKey: 'k'.repeat(257), joinOpenRuns: false }, READY).valid,
    ).toBe(true);
  });
});

describe('validateTarget', () => {
  const base = initialState(TRIGGER_KINDS.webhook, []);
  const context: WizardContext = { agents: [], sources: [], ingestHost: 'h', routing: READY };

  it('checks the routing settings only when kora chooses', () => {
    expect(validateTarget({ ...base, routing: CHOSEN }, context).valid).toBe(true);
    expect(validateTarget({ ...base, routing: { ...CHOSEN, mode: 'one' } }, context).hint).toBe(
      'Choose the agent this trigger runs.',
    );
  });

  it('needs one agent where routing is not offered', () => {
    const offline: WizardContext = { agents: [], sources: [], ingestHost: 'h' };
    expect(validateTarget({ ...base, routing: CHOSEN }, offline).valid).toBe(false);
  });
});

describe('the routing payload', () => {
  it('maps a routed draft to target_kind and the routing object', () => {
    const state = {
      ...initialState(TRIGGER_KINDS.webhook, []),
      agentTypeId: 'a1',
      routing: { ...CHOSEN, confidence: 'strict' as const, joinKey: ' {{ issue.number }} ' },
    };
    const draft = toDraft(TRIGGER_KINDS.webhook, state);
    expect(draft.targetKind).toBe('routed');
    expect(draft.agentTypeId).toBe('');
    expect(draft.routing && toRoutingRequest(draft.routing)).toEqual({
      candidates: ['a1', 'a2'],
      model: 'liquid/d1',
      confidence: 'strict',
      fallback_agent_type_id: 'a1',
      join_runs: true,
      join_key: '{{ issue.number }}',
    });
  });

  it('sends a null fallback for the no-run choice and no join key while joining is off', () => {
    const row = routingRowOf({ ...CHOSEN, unsure: 'none', joinOpenRuns: false, joinKey: 'k' });
    expect(toRoutingRequest(row)).toEqual({
      candidates: ['a1', 'a2'],
      model: 'liquid/d1',
      confidence: 'balanced',
      fallback_agent_type_id: null,
      join_runs: false,
    });
  });

  it('keeps one agent for every kind but webhook', () => {
    const state = {
      ...initialState(TRIGGER_KINDS.scheduled, []),
      agentTypeId: 'a1',
      routing: CHOSEN,
    };
    const draft = toDraft(TRIGGER_KINDS.scheduled, state);
    expect(draft.targetKind).toBe('agent');
    expect(draft.agentTypeId).toBe('a1');
    expect(draft.routing).toBeUndefined();
  });

  it('reads a routed trigger back with its routing and no agent', () => {
    const wire = {
      id: 't1',
      tenant_id: 'x',
      kind: 'webhook',
      name: 'route',
      target_kind: 'routed',
      agent_type_id: '',
      agent_slug: '',
      prompt_template: 'p',
      enabled: true,
      created_at: '',
      updated_at: '',
      routing: {
        candidates: ['a1', 'a2'],
        model: 'liquid/d1',
        confidence: 'loose',
        fallback_agent_type_id: null,
        join_runs: false,
      },
    } satisfies Trigger;
    const row = toTriggerRow(wire);
    expect(row.targetKind).toBe('routed');
    expect(row.routing).toEqual({
      candidates: ['a1', 'a2'],
      model: 'liquid/d1',
      confidence: 'loose',
      fallbackAgentTypeId: null,
      joinRuns: false,
      joinKey: '',
    });
    expect(toRoutingRow({ ...wire.routing, join_runs: undefined }).joinRuns).toBe(true);
  });
});
