import { describe, expect, it, vi } from 'vitest';

vi.mock('@runkora/server/events-client', () => ({ fetchEventPage: vi.fn() }));
vi.mock('@runkora/server/triggers-client', () => ({ listPlatformEventTypes: vi.fn() }));

import { STREAM_PARAMS, pickParams } from './events-server';

describe('pickParams', () => {
  it('keeps every value of an allowed parameter and drops the rest', () => {
    const picked = pickParams(
      new URLSearchParams('category=a&category=b&q=x&tenant=other'),
      STREAM_PARAMS,
    );
    expect(picked.toString()).toBe('category=a&category=b');
  });
});
