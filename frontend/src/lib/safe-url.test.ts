import { describe, expect, it } from 'vitest';
import { isSafeUrl } from './safe-url';

describe('isSafeUrl', () => {
  it.each([
    ['https://github.com/o/r/pull/6', true],
    ['http://example.com', true],
    ['javascript:alert(1)', false],
    ['data:text/html,hi', false],
    ['not a url', false],
    ['', false],
    [undefined, false],
  ])('%s', (url, want) => {
    expect(isSafeUrl(url)).toBe(want);
  });
});
