import { describe, expect, it } from 'vitest';

import {
  FIELD_IDS,
  deriveFieldKey,
  firstBlocker,
  initialState,
  overflowSentence,
  parallelWord,
  previewBody,
  resolvedFields,
  sharedReviewRows,
  stepsFor,
  toDraft,
  validateDelivery,
  validateName,
  validatePrompt,
  validateTarget,
} from './wizard-state';
import { SHARED_STEPS } from './shared-steps';
import { TRIGGER_KINDS } from './kinds';
import { NEW_SOURCE } from './types';
import type { WizardContext, WizardField, WizardState } from './types';

const SOURCES = [{ id: 'src-1', name: 'acme/widgets', provider: 'github' }];
const AGENTS = [{ id: 'at-1', slug: 'coder', name: 'Coder' }];

const CONTEXT: WizardContext = {
  agents: AGENTS,
  sources: SOURCES,
  ingestHost: 'orbit.local.clearroute.io:8443',
};

function field(overrides: Partial<WizardField> = {}): WizardField {
  return { id: 'f1', label: 'Question', type: 'text', required: true, ...overrides };
}

function state(overrides: Partial<WizardState> = {}): WizardState {
  return {
    ...initialState(TRIGGER_KINDS.webhook, SOURCES),
    name: 'github-push',
    promptTemplate: 'Review {{ event.ref }}',
    agentTypeId: 'at-1',
    ...overrides,
  };
}

describe('stepsFor', () => {
  it('puts the kind step first and the shared three after', () => {
    expect(stepsFor(TRIGGER_KINDS.webhook, SHARED_STEPS).map((s) => s.id)).toEqual([
      'source',
      'target',
      'delivery',
      'review',
    ]);
    expect(stepsFor(TRIGGER_KINDS.scheduled, SHARED_STEPS).map((s) => s.id)).toEqual([
      'schedule',
      'target',
      'delivery',
      'review',
    ]);
    expect(stepsFor(TRIGGER_KINDS.manual, SHARED_STEPS).map((s) => s.id)).toEqual([
      'inputs',
      'target',
      'delivery',
      'review',
    ]);
  });
});

describe('initialState', () => {
  it('applies the kind defaults over the shared ones', () => {
    expect(initialState(TRIGGER_KINDS.webhook, SOURCES).overflow).toBe('wait');
    expect(initialState(TRIGGER_KINDS.webhook, SOURCES).maxParallel).toBe(4);
    // A schedule that overlaps itself is nearly always a mistake.
    expect(initialState(TRIGGER_KINDS.scheduled, SOURCES).overflow).toBe('replace');
    expect(initialState(TRIGGER_KINDS.scheduled, SOURCES).maxParallel).toBe(1);
  });

  it('preselects the first source so the common case is one click', () => {
    expect(initialState(TRIGGER_KINDS.webhook, SOURCES).sourceChoice).toBe('src-1');
    expect(initialState(TRIGGER_KINDS.webhook, []).sourceChoice).toBe('');
  });
});

describe('deriveFieldKey', () => {
  it('snake_cases a label', () => {
    expect(deriveFieldKey('Row limit', 0)).toBe('row_limit');
    expect(deriveFieldKey('Question', 0)).toBe('question');
  });

  it('collapses punctuation and runs of separators', () => {
    expect(deriveFieldKey('Customer  ID / ref', 0)).toBe('customer_id_ref');
    expect(deriveFieldKey('  Trailing --- ', 0)).toBe('trailing');
  });

  it('falls back to a positional key when nothing survives', () => {
    expect(deriveFieldKey('', 0)).toBe('field_1');
    expect(deriveFieldKey('***', 2)).toBe('field_3');
  });
});

describe('resolvedFields', () => {
  it('drops unlabelled rows and keys the rest', () => {
    const fields = [
      field({ id: 'a', label: 'Question' }),
      field({ id: 'b', label: '   ' }),
      field({ id: 'c', label: 'Row limit', type: 'number', required: false }),
    ];
    expect(resolvedFields(fields)).toEqual([
      { name: 'question', label: 'Question', type: 'text', required: true },
      { name: 'row_limit', label: 'Row limit', type: 'number', required: false },
    ]);
  });
});

describe('previewBody', () => {
  it('shows string values, since the API takes a string map', () => {
    const body = previewBody([
      field({ id: 'a', label: 'Question' }),
      field({ id: 'b', label: 'Row limit', type: 'number' }),
      field({ id: 'c', label: 'Dry run', type: 'boolean' }),
    ]);
    expect(JSON.parse(body)).toEqual({
      variables: { question: '…', row_limit: '0', dry_run: 'true' },
    });
  });
});

describe('shared step validation', () => {
  it('requires an agent and in-range limits on the target step', () => {
    expect(validateTarget(state({ agentTypeId: '' })).valid).toBe(false);
    expect(validateTarget(state({ maxParallel: 33 })).valid).toBe(false);
    expect(validateTarget(state({ overflow: 'wait', maxQueueDepth: 0 })).valid).toBe(false);
    expect(validateTarget(state({ overflow: 'refuse', maxQueueDepth: 0 })).valid).toBe(true);
  });

  it('bounds the delivery timeout', () => {
    expect(validateDelivery(state({ timeoutSeconds: 4 })).valid).toBe(false);
    expect(validateDelivery(state({ timeoutSeconds: 601 })).valid).toBe(false);
    expect(validateDelivery(state({ timeoutSeconds: 30 })).valid).toBe(true);
  });

  it('names the blocker as a fragment and points at the control', () => {
    const missingAgent = validateTarget(state({ agentTypeId: '' }));
    expect(missingAgent.blocker).toBe('Needs an agent');
    expect(missingAgent.fieldId).toBe(FIELD_IDS.agent);

    const badTimeout = validateDelivery(state({ timeoutSeconds: 4 }));
    expect(badTimeout.fieldId).toBe(FIELD_IDS.timeout);
  });

  it('never blocks the review step', () => {
    const review = SHARED_STEPS.find((s) => s.id === 'review')!;
    expect(review.validate(state({ name: '' }), CONTEXT).valid).toBe(true);
  });
});

describe('firstBlocker', () => {
  it('reports one thing at a time, in the order the step reads', () => {
    const blank = state({ name: '', promptTemplate: '' });
    expect(
      firstBlocker(
        () => validateName(blank),
        () => validatePrompt(blank),
      ).blocker,
    ).toBe('Needs a name');

    const named = state({ promptTemplate: '' });
    expect(
      firstBlocker(
        () => validateName(named),
        () => validatePrompt(named),
      ).blocker,
    ).toBe('Needs a prompt');
  });

  it('passes when every check passes', () => {
    const good = state();
    expect(
      firstBlocker(
        () => validateName(good),
        () => validatePrompt(good),
      ).valid,
    ).toBe(true);
  });
});

describe('kind step blockers', () => {
  it('asks the webhook step for a source name only when creating one', () => {
    const step = TRIGGER_KINDS.webhook.steps[0]!;
    const creating = state({ sourceChoice: NEW_SOURCE, newSourceName: '' });
    expect(step.validate(creating, CONTEXT).blocker).toBe('Needs a source name');

    const none = state({ sourceChoice: '' });
    expect(step.validate(none, CONTEXT).blocker).toBe('Needs a source');
  });

  it('asks the manual step for one labelled field', () => {
    const step = TRIGGER_KINDS.manual.steps[0]!;
    expect(step.validate(state({ fields: [] }), CONTEXT).blocker).toBe('Needs one labelled field');
    expect(step.validate(state({ fields: [field()] }), CONTEXT).valid).toBe(true);
  });
});

describe('sharedReviewRows', () => {
  it('labels every row with the exact string on the control that sets it', () => {
    const rows = sharedReviewRows(state(), CONTEXT);
    expect(rows.map((r) => r.key)).toEqual([
      'Agent to run',
      'Overlapping runs',
      'Timeout',
      'Suppress duplicates',
      'Auto-pause on failure',
    ]);
  });

  it('gives every row a description', () => {
    for (const row of sharedReviewRows(state(), CONTEXT)) {
      expect(row.description?.length).toBeGreaterThan(0);
    }
  });

  it('names the agent and reads the concurrency as one sentence', () => {
    const rows = sharedReviewRows(state({ maxParallel: 4, overflow: 'wait' }), CONTEXT);
    expect(rows.find((r) => r.key === 'Agent to run')?.value).toBe('Coder');
    expect(rows.find((r) => r.key === 'Overlapping runs')?.value).toBe(
      'Up to 4 at once, then wait',
    );
  });
});

describe('overflowSentence', () => {
  it('joins the ceiling and what happens past it', () => {
    expect(overflowSentence(state({ maxParallel: 1, overflow: 'replace' }))).toBe(
      'One at a time, then replace',
    );
    expect(overflowSentence(state({ maxParallel: 8, overflow: 'refuse' }))).toBe(
      'Up to 8 at once, then refuse',
    );
  });
});

describe('toDraft', () => {
  it('carries the existing source and event type for a webhook', () => {
    const draft = toDraft(
      TRIGGER_KINDS.webhook,
      state({ sourceChoice: 'src-1', eventType: 'push' }),
    );
    expect(draft.kind).toBe('webhook');
    expect(draft.source).toEqual({ existingId: 'src-1' });
    expect(draft.eventType).toBe('push');
    expect(draft.schedule).toBeUndefined();
    expect(draft.manualInputs).toBeUndefined();
  });

  it('asks for a new source when one was requested', () => {
    const draft = toDraft(
      TRIGGER_KINDS.webhook,
      state({ sourceChoice: NEW_SOURCE, newSourceName: ' acme ', newSourceProvider: 'github' }),
    );
    expect(draft.source).toEqual({ newName: 'acme', provider: 'github' });
  });

  it('derives the cron for a scheduled trigger', () => {
    const draft = toDraft(
      TRIGGER_KINDS.scheduled,
      state({ freq: 'daily', time: '02:00', timezone: 'UTC', catchUp: true }),
    );
    expect(draft.schedule).toEqual({
      cronExpression: '0 2 * * *',
      timezone: 'UTC',
      catchUp: true,
    });
  });

  it('carries only labelled inputs for a manual trigger', () => {
    const draft = toDraft(
      TRIGGER_KINDS.manual,
      state({ fields: [field({ id: 'a', label: 'Question' }), field({ id: 'b', label: '' })] }),
    );
    expect(draft.manualInputs).toEqual([
      { name: 'question', label: 'Question', type: 'text', required: true },
    ]);
  });
});

describe('parallelWord', () => {
  it('reads as prose at both ends', () => {
    expect(parallelWord(1)).toBe('One at a time');
    expect(parallelWord(4)).toBe('Up to 4 at once');
  });
});
