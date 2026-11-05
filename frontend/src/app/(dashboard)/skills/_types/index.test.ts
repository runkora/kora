/** Unit tests for the search minimum, which counts code points as skills-service counts runes. */
import { describe, expect, it } from 'vitest';
import { toSearch } from './index';

describe('toSearch', () => {
  it.each([
    ['👍👍', ''],
    ['👍👍👍', '👍👍👍'],
    ['  pd ', ''],
    [' pdf ', 'pdf'],
  ])('reads %j as %j', (text, search) => {
    expect(toSearch(text)).toBe(search);
  });
});
