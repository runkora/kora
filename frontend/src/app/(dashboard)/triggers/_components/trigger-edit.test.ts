import { describe, expect, it } from 'vitest';

import { filtersFromMatchCondition, manualInputsCatalog, sameFilters } from './trigger-edit';

describe('filtersFromMatchCondition', () => {
  it('turns a stored condition back into editable rows', () => {
    expect(
      filtersFromMatchCondition({ action: ['opened', 'synchronize'], 'pr.base_ref': ['main'] }),
    ).toEqual([
      { field: 'action', operator: 'is', values: 'opened | synchronize' },
      { field: 'pr.base_ref', operator: 'is', values: 'main' },
    ]);
  });

  it('turns stored contains filters into contains rows after the exact ones', () => {
    expect(
      filtersFromMatchCondition({ action: ['created'] }, { 'comment.body': ['@kora-agent'] }),
    ).toEqual([
      { field: 'action', operator: 'is', values: 'created' },
      { field: 'comment.body', operator: 'contains', values: '@kora-agent' },
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
    // The one example ladder, shared with the wizard's own field builder.
    expect(num?.example).toBe('3');
    expect(bool?.example).toBe('true');
    expect(choice?.example).toBe('alpha');
  });

  it('is empty when the trigger declares nothing', () => {
    expect(manualInputsCatalog(undefined)).toEqual([]);
    expect(manualInputsCatalog([])).toEqual([]);
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

  it('treats a missing operator as is', () => {
    expect(
      sameFilters([{ field: 'a', values: 'x' }], [{ field: 'a', operator: 'is', values: 'x' }]),
    ).toBe(true);
  });

  it('tells is and contains apart', () => {
    expect(
      sameFilters(
        [{ field: 'a', operator: 'is', values: 'x' }],
        [{ field: 'a', operator: 'contains', values: 'x' }],
      ),
    ).toBe(false);
  });
});
