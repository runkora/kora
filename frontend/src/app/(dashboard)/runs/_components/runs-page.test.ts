import { describe, expect, it } from 'vitest';
import { runTitle } from './runs-page';

describe('runTitle', () => {
  it('shows the task summary without its full stop', () => {
    expect(runTitle({ summary: 'Fixing CI.', state: 'completed' })).toEqual({
      text: 'Fixing CI',
      known: true,
    });
  });

  it.each(['pending', 'starting'] as const)('reads "Waiting to start" while %s', (state) => {
    expect(runTitle({ summary: null, state })).toEqual({ text: 'Waiting to start', known: true });
  });

  it.each(['active', 'idle', 'completed', 'failed', 'terminated'] as const)(
    'reads "No task summary", muted, once %s',
    (state) => {
      expect(runTitle({ summary: null, state })).toEqual({
        text: 'No task summary',
        known: false,
      });
    },
  );
});
