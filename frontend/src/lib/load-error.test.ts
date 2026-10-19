import { describe, expect, it } from 'vitest';
import { loadErrorMessage } from './load-error';

describe('loadErrorMessage', () => {
  it.each([
    [401, 'Session expired. Sign in again.'],
    [403, 'You do not have access to triggers.'],
    [404, 'Could not load triggers. Try again.'],
    [500, 'Could not load triggers. Try again.'],
  ])('status %i', (status, want) => {
    expect(loadErrorMessage(status, 'triggers')).toBe(want);
  });

  it('uses the backend detail for other failures only', () => {
    expect(loadErrorMessage(400, 'runs', 'limit must be a positive integer')).toBe(
      'limit must be a positive integer',
    );
    expect(loadErrorMessage(403, 'runs', 'forbidden')).toBe('You do not have access to runs.');
  });
});
