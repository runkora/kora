import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';

import { FilterRowsEditor, type FilterRow } from './filter-rows-editor';
import type { SchemaCatalogEntry } from './trigger-wizard/field-catalog';

const FIELDS: SchemaCatalogEntry[] = [
  { token: 'status', description: 'firing or resolved', group: 'event' },
  { token: 'commonLabels.<name>', description: 'A label', group: 'commonLabels', pattern: true },
];

function Harness({ initial }: { initial: FilterRow[] }) {
  const [rows, setRows] = useState(initial);
  return <FilterRowsEditor rows={rows} fields={FIELDS} onChange={setRows} />;
}

describe('FilterRowsEditor', () => {
  it("keeps each row's typed name when a row above is removed", () => {
    render(
      <Harness
        initial={[
          {
            field: 'commonLabels.team',
            values: 'a',
            pattern: 'commonLabels.<name>',
            patternName: 'team',
          },
          {
            field: 'commonLabels.severity',
            values: 'b',
            pattern: 'commonLabels.<name>',
            patternName: 'severity',
          },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove filter 1' }));
    expect((screen.getByLabelText('Filter 1 name') as HTMLInputElement).value).toBe('severity');
  });

  it('defaults a row to is and switches it to contains', () => {
    render(<Harness initial={[{ field: 'status', values: '' }]} />);
    const operator = screen.getByLabelText('Filter 1 operator') as HTMLSelectElement;
    expect(operator.value).toBe('is');
    fireEvent.change(operator, { target: { value: 'contains' } });
    expect((screen.getByLabelText('Filter 1 operator') as HTMLSelectElement).value).toBe(
      'contains',
    );
  });

  it('hints allowed values only on is rows', () => {
    const fields: SchemaCatalogEntry[] = [
      { token: 'action', description: '', group: 'event', enum: ['created', 'edited'] },
    ];
    render(
      <FilterRowsEditor
        rows={[
          { field: 'action', values: '' },
          { field: 'action', operator: 'contains', values: '' },
        ]}
        fields={fields}
        onChange={() => undefined}
      />,
    );
    expect(screen.getByLabelText('Filter 1 values')).toHaveAttribute(
      'placeholder',
      'created | edited',
    );
    expect(screen.getByLabelText('Filter 2 values')).toHaveAttribute('placeholder', 'text | other');
  });

  it('offers a neutral values hint for a field with no listed values', () => {
    render(<Harness initial={[{ field: 'status', values: '' }]} />);
    expect(screen.getByLabelText('Filter 1 values')).toHaveAttribute(
      'placeholder',
      'value | other',
    );
  });

  it('disables every row and Add filter when disabled', () => {
    render(
      <FilterRowsEditor
        rows={[{ field: 'action', values: 'opened' }]}
        fields={FIELDS}
        onChange={() => undefined}
        disabled
      />,
    );
    expect(screen.getByLabelText('Filter 1 values')).toBeDisabled();
    expect(screen.getByLabelText('Filter 1 operator')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Remove filter 1' })).toBeDisabled();
    expect(screen.getByRole('button', { name: /add filter/i })).toBeDisabled();
  });
});
