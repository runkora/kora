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
    fireEvent.mouseEnter(screen.getByLabelText('Filter 1 values'));
    fireEvent.click(screen.getByRole('button', { name: 'Remove filter 1' }));
    expect((screen.getByLabelText('Filter 1 name') as HTMLInputElement).value).toBe('severity');
  });

  it('offers the remove button only while the pointer is over the row', () => {
    render(
      <FilterRowsEditor
        rows={[{ field: 'status', values: 'firing' }]}
        fields={FIELDS}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Remove filter 1' })).toBeNull();
    fireEvent.mouseEnter(screen.getByLabelText('Filter 1 values'));
    expect(screen.getByRole('button', { name: 'Remove filter 1' })).toBeTruthy();
    fireEvent.mouseLeave(screen.getByLabelText('Filter 1 values'));
    expect(screen.queryByRole('button', { name: 'Remove filter 1' })).toBeNull();
  });

  it('defaults a row to is and switches it to contains', () => {
    render(<Harness initial={[{ field: 'status', values: '' }]} />);
    expect(screen.getByLabelText('Filter 1 operator')).toHaveTextContent('is');
    fireEvent.click(screen.getByLabelText('Filter 1 operator'));
    fireEvent.click(screen.getByRole('option', { name: 'contains' }));
    expect(screen.getByLabelText('Filter 1 operator')).toHaveTextContent('contains');
  });

  it('marks a row with a field and no value only when asked to', () => {
    const rows = [{ field: 'status', values: '' }];
    const { rerender } = render(
      <FilterRowsEditor rows={rows} fields={FIELDS} onChange={() => undefined} />,
    );
    expect(screen.queryByText(/choose at least one value/i)).toBeNull();
    rerender(
      <FilterRowsEditor rows={rows} fields={FIELDS} onChange={() => undefined} showIncomplete />,
    );
    expect(screen.getByText(/choose at least one value/i)).toBeInTheDocument();
    expect(screen.getByLabelText('Filter 1 values')).toHaveAttribute('placeholder', 'Add a value');
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
    fireEvent.mouseEnter(screen.getByLabelText('Filter 1 values'));
    expect(screen.queryByRole('button', { name: 'Remove filter 1' })).toBeNull();
    expect(screen.getByRole('button', { name: /add filter/i })).toBeDisabled();
  });
});
