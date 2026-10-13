/**
 * Unit tests for the server theme module, against the fixture bundles beside
 * it. `loadThemeFrom` is exercised directly; the process-wide accessors are
 * exercised through a fresh module instance per test, with ORBIT_THEME_DIR
 * pointing at a fixture, because they cache for the life of the module.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import path from 'node:path';
import manifest from '@clear-route/hades/manifest.json';
import { defaultTheme } from '@orbit/theme/default';
import { ThemeLoadError, loadThemeFrom, themeAssetUrl } from './index';

const FIXTURES = path.join(__dirname, '__fixtures__');
const VALID = path.join(FIXTURES, 'valid');
const WITH_FONT = path.join(FIXTURES, 'with-font');
const MALFORMED = path.join(FIXTURES, 'malformed');
const ABSENT = path.join(FIXTURES, 'does-not-exist');

async function freshModule(dir: string) {
  vi.resetModules();
  process.env.ORBIT_THEME_DIR = dir;
  return import('./index');
}

describe('loadThemeFrom', () => {
  let info: ReturnType<typeof vi.spyOn>;
  let error: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    info = vi.spyOn(console, 'info').mockImplementation(() => {});
    error = vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('loads a valid bundle and derives the brand, the CSS and the report', async () => {
    const loaded = await loadThemeFrom(VALID);
    expect(loaded.hadesVersion).toBe(manifest.version);
    expect(loaded.theme).not.toBeNull();
    const { theme, brand, css, report } = loaded;
    const hash = theme!.hash;
    expect(hash).toMatch(/^[0-9a-f]{64}$/);

    expect(brand.productName).toBe('Acme Agents');
    expect(brand.logo).toEqual({
      dark: `/theme/${hash}/assets/logo-dark.svg`,
      light: `/theme/${hash}/assets/logo-light.svg`,
    });
    // Favicon set for dark only: one entry, light falls through.
    expect(brand.favicon).toEqual({ dark: `/theme/${hash}/assets/favicon.svg` });

    // Regex assertions: the colour values are the fixture's, and the
    // hardcoded-colour rule reads string literals, not patterns.
    expect(css).toMatch(
      /html:root:not\(\[data-theme='light'\]\)\{--hades-raw-100:#0a0a0a;--hades-raw-accent-500:oklch\(70% 0\.15 250\)\}/,
    );
    expect(css).toMatch(/html\[data-theme='light'\]\{--hades-raw-100:#fafafa\}/);
    expect(css).not.toContain('--hades-raw-not-a-token');
    expect(css).not.toContain('@font-face');
    expect(loaded.fontPreloads).toEqual([]);

    expect(report.ignored).toEqual([
      { kind: 'token', name: '--hades-raw-not-a-token' },
      { kind: 'slot', name: 'not-a-slot' },
    ]);
    expect(report.fallThrough).toEqual(['favicon.light']);
  });

  it('logs the versions, the hash, the ignored entries and the fall-through values', async () => {
    const loaded = await loadThemeFrom(VALID);
    expect(info).toHaveBeenCalledTimes(1);
    const line = String(info.mock.calls[0]![0]);
    expect(line).toContain(`Hades ${manifest.version}`);
    expect(line).toContain('theme written for Hades 1.0.0');
    expect(line).toContain(loaded.theme!.hash);
    expect(line).toContain('token --hades-raw-not-a-token');
    expect(line).toContain('slot not-a-slot');
    expect(line).toContain('favicon.light');
  });

  it('emits an @font-face and a preload per theme font, and a favicon per mode', async () => {
    const { theme, brand, css, fontPreloads } = await loadThemeFrom(WITH_FONT);
    const url = `/theme/${theme!.hash}/assets/fonts/sans.woff2`;
    expect(fontPreloads).toEqual([url]);
    expect(css).toContain(`@font-face{font-family:"hades-theme-sans";src:url("${url}")`);
    expect(css).toContain('font-display:swap');
    expect(css).toContain('--hades-raw-font-sans:"hades-theme-sans"');
    expect(brand.favicon).toEqual({
      dark: `/theme/${theme!.hash}/assets/favicon-dark.svg`,
      light: `/theme/${theme!.hash}/assets/favicon-light.svg`,
    });
    // No logo slot: the kora logo stays.
    expect(brand.logo).toEqual(defaultTheme.logo);
    expect(brand.productName).toBe('Fontic');
  });

  it('throws with every fault listed when the bundle is malformed', async () => {
    const attempt = loadThemeFrom(MALFORMED);
    await expect(attempt).rejects.toBeInstanceOf(ThemeLoadError);
    const err = (await attempt.catch((e) => e)) as ThemeLoadError;
    expect(err.errors).toHaveLength(3);
    expect(err.message).toContain('brand: unknown top-level key');
    expect(err.message).toContain(
      'tokens.--hades-raw-100.dark: "red" is not a valid hex-or-oklch value',
    );
    expect(err.message).toContain('slots.logo.dark: "missing.svg" is not in the bundle');
    expect(error).toHaveBeenCalledTimes(1);
  });

  it('serves the kora default when there is no bundle', async () => {
    const loaded = await loadThemeFrom(ABSENT);
    expect(loaded.theme).toBeNull();
    expect(loaded.brand).toBe(defaultTheme);
    expect(loaded.css).toBe('');
    expect(loaded.fontPreloads).toEqual([]);
    expect(loaded.report).toEqual({ ignored: [], fallThrough: [] });
    expect(loaded.hadesVersion).toBe(manifest.version);
    expect(String(info.mock.calls[0]![0])).toContain('no bundle');
  });
});

describe('themeAssetUrl', () => {
  it('builds the versioned asset URL and encodes each segment', () => {
    expect(themeAssetUrl('abc', 'fonts/sans.woff2')).toBe('/theme/abc/assets/fonts/sans.woff2');
    expect(themeAssetUrl('abc', 'odd name.svg')).toBe('/theme/abc/assets/odd%20name.svg');
  });
});

describe('getTheme, getBrand and getThemeAsset', () => {
  const originalDir = process.env.ORBIT_THEME_DIR;

  beforeEach(() => {
    vi.spyOn(console, 'info').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
    if (originalDir === undefined) delete process.env.ORBIT_THEME_DIR;
    else process.env.ORBIT_THEME_DIR = originalDir;
  });

  it('reads ORBIT_THEME_DIR, loads once and caches the result', async () => {
    const mod = await freshModule(VALID);
    expect(mod.themeDir()).toBe(VALID);
    const first = mod.getTheme();
    const second = mod.getTheme();
    expect(second).toBe(first);
    const loaded = await first;
    expect(loaded.theme?.hash).toBeDefined();
    expect((await mod.getBrand()).productName).toBe('Acme Agents');
    expect(console.info).toHaveBeenCalledTimes(1);
  });

  it('rejects every call when the bundle is malformed', async () => {
    const mod = await freshModule(MALFORMED);
    await expect(mod.getTheme()).rejects.toBeInstanceOf(mod.ThemeLoadError);
    await expect(mod.getBrand()).rejects.toBeInstanceOf(mod.ThemeLoadError);
  });

  it('falls back to the default directory when ORBIT_THEME_DIR is unset', async () => {
    vi.resetModules();
    delete process.env.ORBIT_THEME_DIR;
    const mod = await import('./index');
    expect(mod.themeDir()).toBe(mod.DEFAULT_THEME_DIR);
  });

  it('serves only referenced assets under the loaded hash', async () => {
    const mod = await freshModule(VALID);
    const { theme } = await mod.getTheme();
    const hash = theme!.hash;

    const logo = await mod.getThemeAsset(hash, 'logo-dark.svg');
    expect(logo).not.toBeNull();
    expect(logo!.contentType).toBe('image/svg+xml');
    expect(new TextDecoder().decode(logo!.bytes)).toContain('<svg');

    expect(await mod.getThemeAsset('0'.repeat(64), 'logo-dark.svg')).toBeNull();
    // In the bundle directory, but not referenced by the document.
    expect(await mod.getThemeAsset(hash, 'nothing.svg')).toBeNull();
    expect(await mod.getThemeAsset(hash, '../theme.yaml')).toBeNull();
    expect(await mod.getThemeAsset(hash, 'theme.yaml')).toBeNull();
  });

  it('types a font asset as woff2', async () => {
    const mod = await freshModule(WITH_FONT);
    const { theme } = await mod.getTheme();
    const font = await mod.getThemeAsset(theme!.hash, 'fonts/sans.woff2');
    expect(font?.contentType).toBe('font/woff2');
  });

  it('serves no asset without a theme', async () => {
    const mod = await freshModule(ABSENT);
    expect(await mod.getThemeAsset('any', 'logo-dark.svg')).toBeNull();
  });
});
