import { describe, expect, it } from 'vitest';

import {
  PROVIDER_EVENTS,
  eventOptionsFor,
  filtersFromMatchCondition,
  isValidCron,
  manualInputsCatalog,
  sameFilters,
} from './trigger-edit';

describe('eventOptionsFor', () => {
  it('offers the provider common events', () => {
    expect(eventOptionsFor('github', '')).toEqual(PROVIDER_EVENTS.github);
    expect(eventOptionsFor('alertmanager', '')).toEqual(['firing', 'resolved']);
  });

  it('keeps an event the trigger already listens on, even off the common list', () => {
    // The backend takes whatever the provider sends, so a trigger can hold an
    // event the console does not list. Dropping it would rewrite the trigger.
    expect(eventOptionsFor('github', 'deployment_status')).toContain('deployment_status');
  });

  it('does not duplicate an event already listed', () => {
    const options = eventOptionsFor('github', 'push');
    expect(options.filter((e) => e === 'push')).toHaveLength(1);
  });

  it('is empty for a provider with no adapter', () => {
    expect(eventOptionsFor('gitlab', '')).toEqual([]);
  });
});

describe('filtersFromMatchCondition', () => {
  it('turns a stored condition back into editable rows', () => {
    expect(
      filtersFromMatchCondition({ action: ['opened', 'synchronize'], 'pr.base_ref': ['main'] }),
    ).toEqual([
      { field: 'action', values: 'opened | synchronize' },
      { field: 'pr.base_ref', values: 'main' },
    ]);
  });

  it('is empty for no condition', () => {
    expect(filtersFromMatchCondition(undefined)).toEqual([]);
    expect(filtersFromMatchCondition({})).toEqual([]);
  });

  it('round-trips the separator the values input expects', () => {
    const rows = filtersFromMatchCondition({ action: ['opened', 'closed'] });
    expect(rows[0]?.values).toBe('opened | closed');
  });
});

describe('manualInputsCatalog', () => {
  it('names one var per declared input', () => {
    expect(
      manualInputsCatalog([
        { name: 'question', type: 'text', label: 'Question' },
        { name: 'row_limit', type: 'number', label: 'Row limit' },
      ]).map((e) => e.token),
    ).toEqual(['var.question', 'var.row_limit']);
  });

  it('describes an input by its label, falling back to its key', () => {
    expect(
      manualInputsCatalog([{ name: 'question', type: 'text', label: 'Question' }])[0]?.description,
    ).toBe('The "Question" input');
    expect(manualInputsCatalog([{ name: 'question', type: 'text' }])[0]?.description).toBe(
      'The question input',
    );
  });

  it('gives the preview a plausible example per type', () => {
    const [num, bool, choice] = manualInputsCatalog([
      { name: 'n', type: 'number' },
      { name: 'b', type: 'boolean' },
      { name: 'c', type: 'choice', choices: ['alpha', 'beta'] },
    ]);
    expect(num?.example).toBe('12');
    expect(bool?.example).toBe('true');
    expect(choice?.example).toBe('alpha');
  });

  it('is empty when the trigger declares nothing', () => {
    expect(manualInputsCatalog(undefined)).toEqual([]);
    expect(manualInputsCatalog([])).toEqual([]);
  });
});

describe('isValidCron', () => {
  it('accepts five-field expressions', () => {
    expect(isValidCron('0 2 * * *')).toBe(true);
    expect(isValidCron('*/15 * * * *')).toBe(true);
    expect(isValidCron('0 9 * * 1-5')).toBe(true);
    expect(isValidCron('30 3 1 * *')).toBe(true);
  });

  it('tolerates surrounding and repeated whitespace', () => {
    expect(isValidCron('  0   2 * * *  ')).toBe(true);
  });

  it('rejects the wrong number of fields', () => {
    expect(isValidCron('')).toBe(false);
    expect(isValidCron('0 2 * *')).toBe(false);
    expect(isValidCron('0 2 * * * *')).toBe(false);
  });

  it('rejects a field with characters no cron field carries', () => {
    expect(isValidCron('0 2 * * every-day!')).toBe(false);
    expect(isValidCron('0 2 * * $')).toBe(false);
  });
});

describe('sameFilters', () => {
  it('compares row content, not identity', () => {
    expect(
      sameFilters([{ field: 'action', values: 'opened' }], [{ field: 'action', values: 'opened' }]),
    ).toBe(true);
  });

  it('notices a changed field, value, or length', () => {
    expect(
      sameFilters([{ field: 'action', values: 'opened' }], [{ field: 'action', values: 'closed' }]),
    ).toBe(false);
    expect(
      sameFilters(
        [{ field: 'action', values: 'opened' }],
        [{ field: 'label.name', values: 'opened' }],
      ),
    ).toBe(false);
    expect(sameFilters([], [{ field: 'action', values: 'opened' }])).toBe(false);
  });
});
