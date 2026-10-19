/**
 * Integration tests for GET /api/health.
 *
 * `@orbit/server/theme` is vi.mock'd so the route reports a controlled theme
 * state; the module's own tests cover the loading.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('@orbit/server/theme', () => ({
  getTheme: vi.fn(),
}));

import { GET } from './route';
import { getTheme } from '@orbit/server/theme';

type Loaded = Awaited<ReturnType<typeof getTheme>>;

describe('GET /api/health', () => {
  beforeEach(() => {
    vi.mocked(getTheme).mockReset();
  });

  it('reports ok, the instance version and the loaded theme hash', async () => {
    vi.mocked(getTheme).mockResolvedValue({
      koraVersion: '1.0.0',
      theme: { hash: 'c'.repeat(64) },
    } as Loaded);

    const res = await GET();

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      status: 'ok',
      version: '1.0.0',
      theme: { hash: 'c'.repeat(64) },
    });
  });

  it('reports theme null, and a null version for a build that is not from a release, when no theme is mounted', async () => {
    vi.mocked(getTheme).mockResolvedValue({
      koraVersion: null,
      theme: null,
    } as Loaded);

    const res = await GET();

    expect(await res.json()).toEqual({
      status: 'ok',
      version: null,
      theme: null,
    });
  });

  it('fails when the theme failed to load, so the pod never becomes ready', async () => {
    vi.mocked(getTheme).mockRejectedValue(new Error('theme at /etc/orbit/theme is invalid'));

    await expect(GET()).rejects.toThrow('is invalid');
  });
});
