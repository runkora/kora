import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createFromWizardDraft } from './create-from-wizard';
import { createSource, createTrigger, errorMessageOf } from './triggers-api';
import type { TriggerDraft } from '../_components/trigger-wizard';

vi.mock('./triggers-api', async (original) => ({
  errorMessageOf: (await original<typeof import('./triggers-api')>()).errorMessageOf,
  createSource: vi.fn(),
  createTrigger: vi.fn(),
}));

const mockCreateSource = vi.mocked(createSource);
const mockCreateTrigger = vi.mocked(createTrigger);

const OPTIONS = { ingestHost: 'api.orbit.example.com' };

function draft(overrides: Partial<TriggerDraft> = {}): TriggerDraft {
  return {
    kind: 'webhook',
    name: 'gh-push',
    promptTemplate: 'Review {{ push.branch }}',
    source: { existingId: 'src-1' },
    eventType: 'push',
    targetKind: 'agent',
    agentTypeId: 'at-1',
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,
    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,
    ...overrides,
  };
}

function trigger(id = 't1') {
  return { trigger: { id }, error: null };
}

beforeEach(() => {
  vi.resetAllMocks();
  mockCreateTrigger.mockResolvedValue(trigger() as never);
});

describe('webhook drafts', () => {
  it('uses an existing source without creating one', async () => {
    const result = await createFromWizardDraft(draft(), OPTIONS);

    expect(mockCreateSource).not.toHaveBeenCalled();
    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({ source_id: 'src-1', event_type: 'push' }),
    );
    expect(result).toEqual({
      ok: true,
      triggerId: 't1',
      endpoint: 'https://api.orbit.example.com/api/v1/webhooks/src-1',
    });
  });

  it('sends the sender restriction when the draft carries one, and omits it otherwise', async () => {
    await createFromWizardDraft(
      draft({ senderRestriction: { ids: ['octocat'], roles: ['MEMBER'] } }),
      OPTIONS,
    );
    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({
        sender_restriction: { ids: ['octocat'], roles: ['MEMBER'] },
      }),
    );

    await createFromWizardDraft(draft(), OPTIONS);
    expect(mockCreateTrigger.mock.calls[1]?.[0]).not.toHaveProperty('sender_restriction');
  });

  it('sends coalesce for a webhook draft that carries it, and omits it otherwise', async () => {
    await createFromWizardDraft(
      draft({ coalesce: { key: '{{ pr.number }}', windowSeconds: 30 } }),
      OPTIONS,
    );
    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({ coalesce: { key: '{{ pr.number }}', window_seconds: 30 } }),
    );

    await createFromWizardDraft(draft(), OPTIONS);
    expect(mockCreateTrigger.mock.calls[1]?.[0]).not.toHaveProperty('coalesce');
  });

  it('never sends coalesce for a scheduled or manual draft', async () => {
    const coalesce = { key: 'k', windowSeconds: 30 };
    await createFromWizardDraft(
      draft({
        kind: 'scheduled',
        source: undefined,
        eventType: undefined,
        coalesce,
        schedule: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
      }),
      OPTIONS,
    );
    await createFromWizardDraft(
      draft({ kind: 'manual', source: undefined, eventType: undefined, coalesce }),
      OPTIONS,
    );
    expect(mockCreateTrigger.mock.calls[0]?.[0]).not.toHaveProperty('coalesce');
    expect(mockCreateTrigger.mock.calls[1]?.[0]).not.toHaveProperty('coalesce');
  });

  it('creates the source first, and passes its one-time secret through', async () => {
    mockCreateSource.mockResolvedValue({
      source: { id: 'src-new', secret: 'whsec_abc' },
      error: null,
    } as never);

    const result = await createFromWizardDraft(
      draft({ source: { newName: 'acme/widgets', provider: 'github' } }),
      OPTIONS,
    );

    expect(mockCreateSource).toHaveBeenCalledWith({
      name: 'acme/widgets',
      provider: 'github',
    });
    expect(result).toEqual({
      ok: true,
      triggerId: 't1',
      endpoint: 'https://api.orbit.example.com/api/v1/webhooks/src-new',
      secret: 'whsec_abc',
    });
  });

  it('sends the signing secret a provider issued, and shows none back', async () => {
    mockCreateSource.mockResolvedValue({ source: { id: 'src-slack' }, error: null } as never);

    const result = await createFromWizardDraft(
      draft({ source: { newName: 'acme', provider: 'slack', signingSecret: 'slack-secret' } }),
      OPTIONS,
    );

    expect(mockCreateSource).toHaveBeenCalledWith({
      name: 'acme',
      provider: 'slack',
      signing_secret: 'slack-secret',
    });
    expect(result).toEqual({
      ok: true,
      triggerId: 't1',
      endpoint: 'https://api.orbit.example.com/api/v1/webhooks/src-slack',
    });
  });

  it('stops at a failed source and never creates a trigger pointing nowhere', async () => {
    mockCreateSource.mockResolvedValue({ source: null, error: 'name already taken' } as never);

    const result = await createFromWizardDraft(
      draft({ source: { newName: 'acme', provider: 'github' } }),
      OPTIONS,
    );

    expect(result).toEqual({ ok: false, error: 'name already taken' });
    expect(mockCreateTrigger).not.toHaveBeenCalled();
  });

  it('refuses a webhook draft with no source at all', async () => {
    const result = await createFromWizardDraft(draft({ source: undefined }), OPTIONS);

    expect(result).toEqual({ ok: false, error: 'Choose a source.' });
    expect(mockCreateTrigger).not.toHaveBeenCalled();
  });

  it('sends an empty event type as-is, since that means every event', async () => {
    await createFromWizardDraft(draft({ eventType: '' }), OPTIONS);

    expect(mockCreateTrigger).toHaveBeenCalledWith(expect.objectContaining({ event_type: '' }));
  });

  it('folds the match condition through when one was set', async () => {
    await createFromWizardDraft(draft({ matchCondition: { action: ['opened'] } }), OPTIONS);

    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({ match_condition: { action: ['opened'] } }),
    );
  });

  it('folds the contains filters through when some were set', async () => {
    await createFromWizardDraft(
      draft({ matchContains: { 'comment.body': ['@kora-agent'] } }),
      OPTIONS,
    );

    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({ match_contains: { 'comment.body': ['@kora-agent'] } }),
    );
  });
});

describe('scheduled drafts', () => {
  it('maps the schedule to snake_case and names no source', async () => {
    const result = await createFromWizardDraft(
      draft({
        kind: 'scheduled',
        source: undefined,
        eventType: undefined,
        schedule: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: true },
      }),
      OPTIONS,
    );

    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'scheduled',
        schedule_config: { cron_expression: '0 2 * * *', timezone: 'UTC', catch_up: true },
      }),
    );
    // No source, so nothing to point a provider at.
    expect(result).toEqual({ ok: true, triggerId: 't1' });
  });

  it('does not send an event type for a kind that has none', async () => {
    await createFromWizardDraft(
      draft({
        kind: 'scheduled',
        source: undefined,
        schedule: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
      }),
      OPTIONS,
    );

    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.not.objectContaining({ event_type: expect.anything() }),
    );
  });
});

describe('manual drafts', () => {
  it('maps declared inputs into manual_config', async () => {
    await createFromWizardDraft(
      draft({
        kind: 'manual',
        source: undefined,
        eventType: undefined,
        manualInputs: [
          { name: 'question', label: 'Question', type: 'text', required: true },
          { name: 'mode', label: 'Mode', type: 'choice', required: false, choices: ['a', 'b'] },
        ],
      }),
      OPTIONS,
    );

    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({
        manual_config: {
          inputs: [
            { name: 'question', label: 'Question', type: 'text', required: true },
            { name: 'mode', label: 'Mode', type: 'choice', required: false, choices: ['a', 'b'] },
          ],
        },
      }),
    );
  });
});

describe('failures', () => {
  it('reports the trigger failure, leaving a just-created source in place', async () => {
    mockCreateSource.mockResolvedValue({
      source: { id: 'src-new', secret: 'whsec_abc' },
      error: null,
    } as never);
    mockCreateTrigger.mockResolvedValue({ trigger: null, error: 'agent not found' } as never);

    const result = await createFromWizardDraft(
      draft({ source: { newName: 'acme', provider: 'github' } }),
      OPTIONS,
    );

    // The source is real and the retry can pick it from the existing list, so
    // it is deliberately not rolled back.
    expect(mockCreateSource).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ ok: false, error: 'agent not found' });
  });

  it('falls back to a sentence when the API gives no message', async () => {
    mockCreateTrigger.mockResolvedValue({ trigger: null, error: null } as never);

    expect(await createFromWizardDraft(draft(), OPTIONS)).toEqual({
      ok: false,
      error: 'Failed to create the trigger.',
    });
  });
});

describe('routed drafts', () => {
  it('sends target_kind and routing without an agent', async () => {
    await createFromWizardDraft(
      draft({
        targetKind: 'routed',
        agentTypeId: '',
        routing: {
          candidates: ['at-1', 'at-2'],
          model: 'liquid/d1',
          confidence: 'balanced',
          fallbackAgentTypeId: null,
          joinRuns: true,
          joinKey: '{{ issue.number }}',
        },
      }),
      OPTIONS,
    );
    const sent = mockCreateTrigger.mock.calls[0]?.[0];
    expect(sent).toMatchObject({
      target_kind: 'routed',
      routing: {
        candidates: ['at-1', 'at-2'],
        model: 'liquid/d1',
        confidence: 'balanced',
        fallback_agent_type_id: null,
        join_runs: true,
        join_key: '{{ issue.number }}',
      },
    });
    expect(sent).not.toHaveProperty('agent_type_id');
  });

  it('shows the decision model scope a refused routed trigger is missing', async () => {
    mockCreateTrigger.mockResolvedValue({
      trigger: null,
      error: errorMessageOf(403, {
        message: 'caller does not hold the scopes this routed trigger would be granted',
        details: { missing_scopes: ['inference:liquid:d1:use'] },
      }),
    } as never);
    const result = await createFromWizardDraft(draft(), OPTIONS);
    expect(result).toEqual({
      ok: false,
      error: 'You do not hold every scope this trigger needs. Missing: inference:liquid:d1:use.',
    });
  });

  it('shows the refusal for a candidate the caller cannot run', async () => {
    mockCreateTrigger.mockResolvedValue({
      trigger: null,
      error: errorMessageOf(403, {
        message: 'not authorized to run the agent this trigger binds: agent type "at_coder"',
      }),
    } as never);
    const result = await createFromWizardDraft(draft(), OPTIONS);
    expect(result).toEqual({
      ok: false,
      error: 'not authorized to run the agent this trigger binds: agent type "at_coder"',
    });
  });
});
