/** Unit tests for the server theme module, against the fixtures beside it. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { defaultTheme } from '@orbit/theme/default';
import { ThemeLoadError, loadThemeFrom } from './index';

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

  it('loads a valid document and derives the brand, the CSS and the report', async () => {
    const loaded = await loadThemeFrom(VALID);
    expect(loaded.koraVersion).toBeNull();
    expect(loaded.theme).not.toBeNull();
    const { theme, brand, css, report } = loaded;
    const hash = theme!.hash;
    expect(hash).toMatch(/^[0-9a-f]{64}$/);

    expect(brand.productName).toBe('Acme Agents');
    expect(brand.logo).toEqual({
      dark: 'https://assets.acme.example/brand/logo-dark.svg',
      light: 'https://assets.acme.example/brand/logo-light.svg',
    });
    // Favicon set for dark only: one entry, light falls through.
    expect(brand.favicon).toEqual({ dark: 'https://assets.acme.example/brand/favicon.svg' });

    // Regex assertions: the hardcoded-colour rule reads string literals, not patterns.
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
    expect(line).toContain('kora development build');
    expect(line).toContain('theme written for kora 1.0.0 (not compared: not a release build)');
    expect(line).toContain(loaded.theme!.hash);
    expect(line).toContain('token --hades-raw-not-a-token');
    expect(line).toContain('slot not-a-slot');
    expect(line).toContain('favicon.light');
  });

  it('accepts a theme of the same major and compares it on a release build', async () => {
    vi.stubEnv('NEXT_PUBLIC_ORBIT_VERSION', 'v1.4.0');
    const loaded = await loadThemeFrom(VALID);
    expect(loaded.koraVersion).toBe('1.4.0');
    expect(loaded.report.versionCheckSkipped).toBe(false);
    expect(String(info.mock.calls[0]![0])).toContain('kora 1.4.0, theme written for kora 1.0.0');
    vi.unstubAllEnvs();
  });

  it('refuses a theme written against another major, naming both releases', async () => {
    vi.stubEnv('NEXT_PUBLIC_ORBIT_VERSION', 'v2.0.0');
    const failure = await loadThemeFrom(VALID).catch((e: unknown) => e);
    expect(failure).toBeInstanceOf(ThemeLoadError);
    expect(String((failure as Error).message)).toContain('kora 1.0.0 (major 1)');
    expect(String((failure as Error).message)).toContain('kora 2.0.0');
    expect(error).toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  it('warns at startup when the theme mixes its identity with kora defaults', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    await loadThemeFrom(VALID);
    expect(warn).toHaveBeenCalledTimes(1);
    const line = String(warn.mock.calls[0]![0]);
    expect(line).toContain('mixes its own identity with kora defaults');
    expect(line).toContain('still showing kora: favicon.light');
    warn.mockClear();
    await loadThemeFrom(ABSENT);
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it('emits an @font-face and a preload per theme font, and a favicon per mode', async () => {
    const { brand, css, fontPreloads } = await loadThemeFrom(WITH_FONT);
    const url = 'https://fonts.acme.example/sans.woff2';
    expect(fontPreloads).toEqual([url]);
    expect(css).toContain(`@font-face{font-family:"hades-theme-sans";src:url("${url}")`);
    expect(css).toContain('font-display:swap');
    expect(css).toContain('--hades-raw-font-sans:"hades-theme-sans"');
    expect(brand.favicon).toEqual({
      dark: 'https://assets.acme.example/favicon-dark.svg',
      light: 'https://assets.acme.example/favicon-light.svg',
    });
    // No logo slot: the kora logo stays.
    expect(brand.logo).toEqual(defaultTheme.logo);
    expect(brand.productName).toBe('Fontic');
  });

  it('throws with every fault listed when the document is malformed', async () => {
    const attempt = loadThemeFrom(MALFORMED);
    await expect(attempt).rejects.toBeInstanceOf(ThemeLoadError);
    const err = (await attempt.catch((e) => e)) as ThemeLoadError;
    expect(err.errors).toHaveLength(3);
    expect(err.message).toContain('brand: unknown top-level key');
    expect(err.message).toContain(
      'tokens.--hades-raw-100.dark: "red" is not a valid hex-or-oklch value',
    );
    expect(err.message).toContain(
      'slots.logo.dark: "http://assets.acme.example/logo-dark.svg" is not an https:// URL',
    );
    expect(error).toHaveBeenCalledTimes(1);
  });

  it('fails when the theme directory exists but has no theme.yaml', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    const err = await loadThemeFrom(FIXTURES).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ThemeLoadError);
    expect((err as ThemeLoadError).message).toContain('has no theme.yaml');
    error.mockRestore();
  });

  it('serves the kora default when there is no theme', async () => {
    const loaded = await loadThemeFrom(ABSENT);
    expect(loaded.theme).toBeNull();
    expect(loaded.brand).toBe(defaultTheme);
    expect(loaded.css).toBe('');
    expect(loaded.fontPreloads).toEqual([]);
    expect(loaded.report).toEqual({
      ignored: [],
      fallThrough: [],
      mixedIdentity: [],
      versionCheckSkipped: false,
      contrastWarnings: [],
    });
    expect(loaded.koraVersion).toBeNull();
    expect(String(info.mock.calls[0]![0])).toContain('no theme');
  });
});

describe('getTheme and getBrand', () => {
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

  it('rejects every call when the document is malformed', async () => {
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
});

describe('loadThemeFrom: oversize and unusual documents', () => {
  let tmp: string;

  beforeEach(async () => {
    vi.spyOn(console, 'info').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
    tmp = await mkdtemp(path.join(os.tmpdir(), 'theme-load-'));
  });
  afterEach(async () => {
    vi.restoreAllMocks();
    await rm(tmp, { recursive: true, force: true });
  });

  it('refuses a theme.yaml over the size cap before parsing it', async () => {
    await writeFile(path.join(tmp, 'theme.yaml'), '#'.repeat(70 * 1024));
    const err = await loadThemeFrom(tmp).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ThemeLoadError);
    expect((err as ThemeLoadError).errors).toEqual([
      expect.objectContaining({ path: 'theme.yaml', message: expect.stringContaining('65536') }),
    ]);
  });

  it('uses the slot URLs exactly as the document gives them, query included', async () => {
    await writeFile(
      path.join(tmp, 'theme.yaml'),
      'koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: "https://cdn.acme.example/logo.svg?v=3&sig=a"\n',
    );
    const { brand } = await loadThemeFrom(tmp);
    expect(brand.logo.dark).toBe('https://cdn.acme.example/logo.svg?v=3&sig=a');
    expect(brand.logo.light).toBe(defaultTheme.logo.light);
  });
});
