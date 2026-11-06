import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useScheduleDraft } from './schedule-builder';

describe('useScheduleDraft', () => {
  // Each of these is a valid cron the builder writes back differently, so the
  // draft can never equal the stored text. Rendering must still settle: before
  // the fix it threw "Too many re-renders" on the first render.
  it.each(['*/05 * * * *', '00 9 * * *', '0 9 * * 5,1'])(
    'settles on a stored cron the builder spells differently: %s',
    (cronExpression) => {
      const value = { cronExpression, timezone: 'UTC', catchUp: false };
      const { result, rerender } = renderHook(() => useScheduleDraft(value, () => undefined));
      const first = result.current[0];
      rerender();
      expect(result.current[0]).toBe(first);
    },
  );
});
