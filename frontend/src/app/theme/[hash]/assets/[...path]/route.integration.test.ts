/**
 * Integration tests for GET /theme/<hash>/assets/<path>.
 *
 * `@orbit/server/theme` is vi.mock'd so the route is exercised against a known
 * asset without a bundle on disk; the module's own tests cover the loading.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('@orbit/server/theme', () => ({
  getThemeAsset: vi.fn(),
}));

import { GET } from './route';
import { getThemeAsset } from '@orbit/server/theme';

const HASH = 'a'.repeat(64);

function call(hash: string, path: string[]) {
  return GET(new Request(`http://localhost/theme/${hash}/assets/${path.join('/')}`), {
    params: Promise.resolve({ hash, path }),
  });
}

describe('GET /theme/[hash]/assets/[...path]', () => {
  beforeEach(() => {
    vi.mocked(getThemeAsset).mockReset();
  });

  it('serves a referenced asset with its media type and an immutable cache header', async () => {
    const svg = new TextEncoder().encode('<svg xmlns="http://www.w3.org/2000/svg"/>');
    vi.mocked(getThemeAsset).mockResolvedValue({ bytes: svg, contentType: 'image/svg+xml' });

    const res = await call(HASH, ['logo-dark.svg']);

    expect(getThemeAsset).toHaveBeenCalledWith(HASH, 'logo-dark.svg');
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toBe('image/svg+xml');
    expect(res.headers.get('cache-control')).toBe('public, max-age=31536000, immutable');
    expect(res.headers.get('content-length')).toBe(String(svg.byteLength));
    // Array.from: jsdom and Node hand out typed arrays from different realms.
    expect(Array.from(new Uint8Array(await res.arrayBuffer()))).toEqual(Array.from(svg));
  });

  it('joins nested path segments and serves fonts as font/woff2', async () => {
    vi.mocked(getThemeAsset).mockResolvedValue({
      bytes: new Uint8Array([0x77, 0x4f, 0x46, 0x32]),
      contentType: 'font/woff2',
    });

    const res = await call(HASH, ['fonts', 'sans.woff2']);

    expect(getThemeAsset).toHaveBeenCalledWith(HASH, 'fonts/sans.woff2');
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toBe('font/woff2');
  });

  it('returns 404 with no body when the module serves nothing for the hash and path', async () => {
    vi.mocked(getThemeAsset).mockResolvedValue(null);

    const res = await call('b'.repeat(64), ['logo-dark.svg']);

    expect(res.status).toBe(404);
    expect(await res.text()).toBe('');
    expect(res.headers.get('cache-control')).toBeNull();
  });
});
