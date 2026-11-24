import { describe, expect, it } from 'vitest';

import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from './pagination';

describe('page sizes', () => {
  it('offers the sizes the lists are designed around, smallest first', () => {
    expect(PAGE_SIZES).toEqual([12, 25, 50]);
    expect(DEFAULT_PAGE_SIZE).toBe(12);
  });
});
