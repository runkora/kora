import { describe, expect, it } from 'vitest';

import { DEFAULT_PAGE_SIZE, PAGE_SIZES, pageWindow } from './pagination';

describe('page sizes', () => {
  it('offers the sizes the list is designed around, smallest first', () => {
    expect(PAGE_SIZES).toEqual([12, 25, 50]);
    expect(DEFAULT_PAGE_SIZE).toBe(12);
  });
});

describe('pageWindow', () => {
  it('slices the first page', () => {
    const win = pageWindow(40, 12, 0);
    expect(win).toMatchObject({ page: 0, pageCount: 4, start: 0, end: 12, from: 1, to: 12 });
  });

  it('slices a middle page', () => {
    expect(pageWindow(40, 12, 1)).toMatchObject({ start: 12, end: 24, from: 13, to: 24 });
  });

  it('stops the last page at the real total, not the page size', () => {
    const win = pageWindow(40, 12, 3);
    expect(win).toMatchObject({ start: 36, end: 40, from: 37, to: 40 });
  });

  it('clamps a page that outlived the rows behind it', () => {
    // Narrowing a search while on page 3 must not leave an empty grid that
    // looks broken.
    expect(pageWindow(2, 12, 3)).toMatchObject({ page: 0, start: 0, end: 2, from: 1, to: 2 });
  });

  it('clamps a negative page', () => {
    expect(pageWindow(40, 12, -1).page).toBe(0);
  });

  it('numbers an empty list as nothing rather than as a first item', () => {
    expect(pageWindow(0, 12, 0)).toMatchObject({
      page: 0,
      pageCount: 1,
      start: 0,
      end: 0,
      from: 0,
      to: 0,
      total: 0,
    });
  });

  it('keeps one page when everything fits', () => {
    expect(pageWindow(5, 12, 0).pageCount).toBe(1);
  });

  it('repages when the size grows, without losing rows off the end', () => {
    // The same 40 rows at each offered size.
    expect(pageWindow(40, 12, 0).pageCount).toBe(4);
    expect(pageWindow(40, 25, 0).pageCount).toBe(2);
    expect(pageWindow(40, 50, 0)).toMatchObject({ pageCount: 1, to: 40 });
  });

  it('survives a nonsensical page size rather than dividing by zero', () => {
    expect(pageWindow(10, 0, 0)).toMatchObject({ pageCount: 10, start: 0, end: 1 });
  });
});
