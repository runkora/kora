// @vitest-environment node
import { describe, expect, it } from 'vitest';

import manifestJson from '../../manifest.json';
import { CONTRACT_VERSION } from '../version';
import { DARK_SELECTOR, LIGHT_SELECTOR, fontPreloads, generateThemeCss } from './css';
import { FULL_BUNDLE, FULL_DOCUMENT, MANIFEST, bundle } from './fixtures.test-support';
import type { ThemeManifest } from './manifest';
import type { Theme } from './types';
import { validateTheme } from './validate';

const resolveAsset = (path: string) => `/theme/abc123/assets/${path.replace(/^assets\//, '')}`;

async function load(
  text: string,
  files: Record<string, Uint8Array> = FULL_BUNDLE,
  manifest: ThemeManifest = MANIFEST,
  contractVersion = '1.3',
): Promise<Theme> {
  const result = await validateTheme(text, { manifest, contractVersion, readAsset: bundle(files) });
  if (!result.ok) throw new Error(result.errors.map((e) => `${e.path}: ${e.message}`).join('\n'));
  return result.theme;
}

describe('generateThemeCss', () => {
  it('puts a dark-only token in the dark block only, and omits an empty light block', async () => {
    const theme = await load(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    dark: "#0d0f11"\n',
      {},
    );
    const css = generateThemeCss(theme, { resolveAsset });
    expect(css).toBe(`${DARK_SELECTOR}{--hades-raw-black:#0d0f11}`);
    expect(css).not.toContain(LIGHT_SELECTOR);
  });

  it('puts a light-only token in the light block only', async () => {
    const theme = await load(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    light: "#fff"\n',
      {},
    );
    expect(generateThemeCss(theme, { resolveAsset })).toBe(
      `${LIGHT_SELECTOR}{--hades-raw-black:#fff}`,
    );
  });

  it('returns an empty string for a theme that sets no token and no font', async () => {
    const theme = await load('contractVersion: "1.0"\nproductName: Acme\n', {});
    expect(generateThemeCss(theme, { resolveAsset })).toBe('');
    expect(fontPreloads(theme, { resolveAsset })).toEqual([]);
  });

  it('declares a face per theme font and sets the family token in both blocks', async () => {
    const theme = await load(FULL_DOCUMENT);
    const css = generateThemeCss(theme, { resolveAsset });
    const lines = css.split('\n');
    expect(lines).toHaveLength(4);
    expect(lines[0]).toBe(
      '@font-face{font-family:"hades-theme-mono";src:url("/theme/abc123/assets/mono.woff2") format("woff2");font-display:swap}',
    );
    expect(lines[1]).toBe(
      '@font-face{font-family:"hades-theme-sans";src:url("/theme/abc123/assets/sans.woff2") format("woff2");font-display:swap}',
    );
    expect(lines[2]!.startsWith(`${DARK_SELECTOR}{`)).toBe(true);
    expect(lines[3]!.startsWith(`${LIGHT_SELECTOR}{`)).toBe(true);
    for (const block of [lines[2]!, lines[3]!]) {
      expect(block).toContain(
        '--hades-raw-font-sans:"hades-theme-sans", ui-sans-serif, system-ui, sans-serif',
      );
      expect(block).toContain(
        '--hades-raw-font-mono:"hades-theme-mono", ui-monospace, \'Cascadia Code\', monospace',
      );
    }
    expect(lines[2]).toContain('--hades-raw-accent-400:oklch(70% 0.12 240 / 0.9)');
    expect(lines[3]).not.toContain('--hades-raw-accent-400');
    expect(lines[2]).toContain(
      '--hades-raw-shadow-sm:0 2px 8px oklch(0% 0 0 / 0.25), inset 0 1px 0 #ffffff10',
    );
  });

  it('lists the resolved font URLs for preloading', async () => {
    const theme = await load(FULL_DOCUMENT);
    expect(fontPreloads(theme, { resolveAsset })).toEqual([
      '/theme/abc123/assets/mono.woff2',
      '/theme/abc123/assets/sans.woff2',
    ]);
  });

  it('escapes the resolved URL inside the CSS string', async () => {
    const theme = await load('contractVersion: "1.0"\nslots:\n  font.sans: assets/sans.woff2\n');
    const css = generateThemeCss(theme, { resolveAsset: () => 'https://cdn.example/a"b\\c.woff2' });
    expect(css).toContain('src:url("https://cdn.example/a\\"b\\\\c.woff2")');
  });

  it('escapes < so a path cannot close the inline <style> element', async () => {
    const theme = await load('contractVersion: "1.0"\nslots:\n  font.sans: assets/sans.woff2\n');
    const css = generateThemeCss(theme, { resolveAsset: () => '/x</style><script>.woff2' });
    expect(css).not.toContain('<');
    expect(css).toContain('\\3c /style>');
  });

  it('is deterministic whatever the document order', async () => {
    const a = await load(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    dark: "#000"\n  --hades-raw-accent-400:\n    dark: "#111"\n',
      {},
    );
    const b = await load(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-accent-400:\n    dark: "#111"\n  --hades-raw-black:\n    dark: "#000"\n',
      {},
    );
    expect(generateThemeCss(a, { resolveAsset })).toBe(generateThemeCss(b, { resolveAsset }));
  });

  it('stays under the 16 KiB first-paint budget for a theme that sets every token and both fonts', async () => {
    const manifest = manifestJson as ThemeManifest;
    // The longest value each format accepts in practice.
    const sample: Record<string, string> = {
      'hex-or-oklch': 'oklch(62.55% 0.155 250.55 / 0.85)',
      'css-shadow-list':
        '0 2px 8px oklch(0% 0 0 / 0.25), 0 1px 2px oklch(0% 0 0 / 0.15), inset 0 1px 0 oklch(100% 0 0 / 0.05), 0 0 0 1px oklch(0% 0 0 / 0.1)',
      'css-length': '0.375rem',
      'font-family-name': 'Some Rather Long Brand Typeface Name, Fallback Name',
    };
    const tokens: Record<string, { dark: string; light: string }> = {};
    for (const token of manifest.tokens) {
      if (token.kind === 'font') continue; // set through the font slots below
      tokens[token.name] = { dark: sample[token.format]!, light: sample[token.format]! };
    }
    const doc = JSON.stringify({
      contractVersion: CONTRACT_VERSION,
      tokens,
      slots: { 'font.sans': 'assets/sans.woff2', 'font.mono': 'assets/mono.woff2' },
    });
    const theme = await load(doc, FULL_BUNDLE, manifest, CONTRACT_VERSION);
    const css = generateThemeCss(theme, { resolveAsset: (p) => `/theme/${theme.hash}/${p}` });
    const preloads = fontPreloads(theme, { resolveAsset: (p) => `/theme/${theme.hash}/${p}` })
      .map((url) => `<link rel="preload" as="font" type="font/woff2" crossorigin href="${url}">`)
      .join('');
    const bytes = new TextEncoder().encode(`<style>${css}</style>${preloads}`).length;
    expect(bytes).toBeLessThan(16 * 1024);
  });
});

describe('generateThemeCss: font family tokens', () => {
  const fontCss = async (value: string) => {
    const theme = await load(
      `contractVersion: "1.0"\ntokens:\n  --hades-raw-font-sans:\n    dark: ${JSON.stringify(value)}\n`,
      {},
    );
    return generateThemeCss(theme, { resolveAsset });
  };

  it.each([
    ['Source Sans 3', '"Source Sans 3"'],
    ['3270', '"3270"'],
    ['Inter, sans-serif', '"Inter", sans-serif'],
    ['IBM Plex Sans 2,monospace', '"IBM Plex Sans 2", monospace'],
    ['Cafe\u0301', '"Cafe\u0301"'],
  ])('emits %j as %s', async (value, expected) => {
    expect(await fontCss(value)).toBe(`${DARK_SELECTOR}{--hades-raw-font-sans:${expected}}`);
  });

  it('refuses a family list that cannot be valid CSS', async () => {
    for (const value of [',', 'Inter,', ',Inter', 'a,,b', 'inherit']) {
      await expect(fontCss(value)).rejects.toThrow('--hades-raw-font-sans');
    }
  });
});
