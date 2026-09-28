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

  it('offers a neutral values hint for a field with no listed values', () => {
    render(<Harness initial={[{ field: 'status', values: '' }]} />);
    expect(screen.getByLabelText('Filter 1 values')).toHaveAttribute(
      'placeholder',
      'value | other',
    );
  });
});
