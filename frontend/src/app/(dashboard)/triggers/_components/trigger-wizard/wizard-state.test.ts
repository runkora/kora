import { describe, expect, it } from 'vitest';

import {
  describeMatchCondition,
  deriveFieldKey,
  initialState,
  matchConditionFrom,
  admissionSummary,
  parallelWord,
  previewBody,
  resolvedFields,
  splitFilterValues,
  stepsFor,
  toDraft,
  validateDelivery,
  validateFilters,
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
  it('asks the target step for an agent, and nothing else', () => {
    expect(validateTarget(state({ agentTypeId: '' })).valid).toBe(false);
    expect(validateTarget(state({ agentTypeId: 'at-1' })).valid).toBe(true);
    // The queue bounds moved to Delivery, so an out-of-range one must not
    // block the step that no longer shows it.
    expect(validateTarget(state({ maxParallel: 33 })).valid).toBe(true);
    expect(validateTarget(state({ overflow: 'wait', maxQueueDepth: 0 })).valid).toBe(true);
  });

  it('bounds all three queue limits on the delivery step', () => {
    expect(validateDelivery(state({ maxParallel: 33 })).valid).toBe(false);
    // The wizard refuses at the same ceiling the API does: 17 was accepted here
    // and rejected on submit while the bound was written down twice.
    expect(validateDelivery(state({ maxParallel: 17 })).valid).toBe(false);
    expect(validateDelivery(state({ maxParallel: 16 })).valid).toBe(true);
    expect(validateDelivery(state({ overflow: 'wait', maxQueueDepth: 0 })).valid).toBe(false);
    expect(validateDelivery(state({ timeoutSeconds: 4 })).valid).toBe(false);
    expect(validateDelivery(state({ timeoutSeconds: 601 })).valid).toBe(false);
    expect(validateDelivery(state()).valid).toBe(true);
  });

  it('ignores the queue depth when nothing queues', () => {
    expect(validateDelivery(state({ overflow: 'refuse', maxQueueDepth: 0 })).valid).toBe(true);
  });

  it('explains what is missing', () => {
    expect(validateTarget(state({ agentTypeId: '' })).hint).toBeTruthy();
  });

  it('never blocks the review step', () => {
    const review = SHARED_STEPS.find((s) => s.id === 'review')!;
    expect(review.validate(state({ name: '' }), CONTEXT).valid).toBe(true);
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

  it('folds the filter rows into matchCondition', () => {
    const draft = toDraft(
      TRIGGER_KINDS.webhook,
      state({
        filters: [
          { field: 'action', values: 'opened | synchronize' },
          { field: 'pr.base_ref', values: 'main' },
        ],
      }),
    );
    expect(draft.matchCondition).toEqual({
      action: ['opened', 'synchronize'],
      'pr.base_ref': ['main'],
    });
  });

  it('omits matchCondition entirely when no filter is set', () => {
    const draft = toDraft(TRIGGER_KINDS.webhook, state({ filters: [] }));
    expect(draft.matchCondition).toBeUndefined();
    expect('matchCondition' in draft).toBe(false);
  });
});

describe('splitFilterValues', () => {
  it('splits on the pipe and trims', () => {
    expect(splitFilterValues('opened | synchronize')).toEqual(['opened', 'synchronize']);
  });

  it('drops empties, so a trailing pipe mid-typing is not a value', () => {
    expect(splitFilterValues('opened |')).toEqual(['opened']);
    expect(splitFilterValues('  ')).toEqual([]);
  });
});

describe('matchConditionFrom', () => {
  it('folds rows into the backend shape', () => {
    expect(
      matchConditionFrom([
        { field: 'action', values: 'opened | synchronize' },
        { field: 'pr.base_ref', values: 'main' },
      ]),
    ).toEqual({ action: ['opened', 'synchronize'], 'pr.base_ref': ['main'] });
  });

  it('is undefined when nothing is set, not an empty condition', () => {
    expect(matchConditionFrom([])).toBeUndefined();
    expect(matchConditionFrom([{ field: '', values: '' }])).toBeUndefined();
  });

  it('ignores a half-written row rather than sending a broken clause', () => {
    expect(matchConditionFrom([{ field: 'action', values: '' }])).toBeUndefined();
    expect(matchConditionFrom([{ field: '', values: 'opened' }])).toBeUndefined();
  });

  it('merges repeated fields, since a clause is a set of alternatives', () => {
    expect(
      matchConditionFrom([
        { field: 'action', values: 'opened' },
        { field: 'action', values: 'closed' },
      ]),
    ).toEqual({ action: ['opened', 'closed'] });
  });
});

describe('validateFilters', () => {
  it('passes on no filters and on complete ones', () => {
    expect(validateFilters([]).valid).toBe(true);
    expect(validateFilters([{ field: 'action', values: 'opened' }]).valid).toBe(true);
  });

  it('blocks a half-written filter, which would silently do nothing', () => {
    const noValues = validateFilters([{ field: 'action', values: '' }]);
    expect(noValues.valid).toBe(false);
    expect(noValues.hint).toBe('Finish or remove the empty filter');

    expect(validateFilters([{ field: '', values: 'opened' }]).valid).toBe(false);
  });
});

describe('describeMatchCondition', () => {
  it('reads as the condition it composes', () => {
    expect(
      describeMatchCondition([
        { field: 'action', values: 'opened | synchronize' },
        { field: 'pr.base_ref', values: 'main' },
      ]),
    ).toBe('action=opened|synchronize · pr.base_ref=main');
  });

  it('is null when there is nothing to say', () => {
    expect(describeMatchCondition([])).toBeNull();
  });
});

describe('parallelWord', () => {
  it('reads as prose at both ends', () => {
    expect(parallelWord(1)).toBe('One at a time');
    expect(parallelWord(4)).toBe('Up to 4 at once');
  });
});

describe('admissionSummary', () => {
  it('names all three bounds when deliveries queue', () => {
    expect(
      admissionSummary(
        state({ maxParallel: 4, overflow: 'wait', maxQueueDepth: 32, timeoutSeconds: 30 }),
        'webhook',
      ),
    ).toBe(
      'Up to 4 at once. Further deliveries wait in order, up to 32 of them, and each has up to 30s to start once it is admitted.',
    );
  });

  it('does not claim a queue bound under replace, where nothing queues up', () => {
    const sentence = admissionSummary(
      state({ maxParallel: 1, overflow: 'replace', maxQueueDepth: 32, timeoutSeconds: 45 }),
      'scheduled',
    );
    expect(sentence).toBe(
      'One at a time. While one runs, a newer delivery replaces the one waiting, and it has up to 45s to start.',
    );
    expect(sentence).not.toContain('32');
  });

  it('does not claim a queue bound under refuse either', () => {
    const sentence = admissionSummary(
      state({ maxParallel: 8, overflow: 'refuse', maxQueueDepth: 32, timeoutSeconds: 30 }),
      'manual',
    );
    expect(sentence).toBe(
      'Up to 8 at once. Beyond that, a fire is refused with 429, and an admitted delivery has up to 30s to start.',
    );
    expect(sentence).not.toContain('32');
  });

  it('promises a 429 only where there is a caller to answer', () => {
    // Only a manual fire is synchronous. A webhook or scheduled arrival beyond
    // the limit is recorded as dropped, so claiming a 429 would be a lie.
    for (const kind of ['webhook', 'scheduled'] as const) {
      const sentence = admissionSummary(state({ overflow: 'refuse' }), kind);
      expect(sentence).not.toContain('429');
      expect(sentence).toContain('recorded as dropped');
    }
  });

  it('says the timeout bounds starting, not running', () => {
    for (const overflow of ['wait', 'replace', 'refuse'] as const) {
      const sentence = admissionSummary(state({ overflow, timeoutSeconds: 90 }), 'webhook');
      expect(sentence).toContain('up to 90s to start');
    }
  });
});
