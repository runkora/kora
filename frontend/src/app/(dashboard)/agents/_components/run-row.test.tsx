import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import type { RunRow } from '../_types';
import { RUN_ROWS } from './agents-mock-data';
import { StarterMark, triggerKindIcon } from './run-row';

describe('triggerKindIcon', () => {
  it.each([
    ['webhook', 'zap'],
    ['scheduled', 'clock'],
    ['manual', 'play'],
    ['event', 'activity'],
  ] as const)('marks a %s trigger with %s', (kind, icon) => {
    expect(triggerKindIcon(kind)).toBe(icon);
  });

  it('falls back to zap for a run recorded without a kind', () => {
    expect(triggerKindIcon(null)).toBe('zap');
  });
});

describe('StarterMark', () => {
  const row: RunRow = { ...RUN_ROWS[0]!, createdByName: 'Dana Lee', startedByTrigger: null };

  it('names the trigger that started the run, whatever its kind', () => {
    renderWithProviders(
      <StarterMark
        row={{
          ...row,
          startedByTrigger: {
            triggerId: 'trg_nightly',
            triggerName: 'nightly dependency scan',
            triggerKind: 'scheduled',
            deliveryId: null,
          },
        }}
      />,
    );
    expect(screen.getByLabelText('Started by nightly dependency scan')).toBeInTheDocument();
  });

  it('names the person when no trigger started the run', () => {
    renderWithProviders(<StarterMark row={row} />);
    expect(screen.getByLabelText('Started by Dana Lee')).toBeInTheDocument();
  });
});
