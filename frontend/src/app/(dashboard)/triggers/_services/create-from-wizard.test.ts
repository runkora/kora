import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createFromWizardDraft } from './create-from-wizard';
import { createSource, createTrigger } from './triggers-api';
import type { TriggerDraft } from '../_components/trigger-wizard';

vi.mock('./triggers-api', () => ({
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
      draft({ senderRestriction: { logins: ['octocat'], associations: ['MEMBER'] } }),
      OPTIONS,
    );
    expect(mockCreateTrigger).toHaveBeenCalledWith(
      expect.objectContaining({
        sender_restriction: { logins: ['octocat'], associations: ['MEMBER'] },
      }),
    );

    await createFromWizardDraft(draft(), OPTIONS);
    expect(mockCreateTrigger.mock.calls[1]?.[0]).not.toHaveProperty('sender_restriction');
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
