// @vitest-environment node
import { describe, expect, it } from 'vitest';

import manifestJson from '../../manifest.json';
import { DARK_SELECTOR, LIGHT_SELECTOR, fontPreloads, generateThemeCss } from './css';
import { FULL_DOCUMENT, MANIFEST } from './fixtures.test-support';
import type { ThemeManifest } from './manifest';
import type { Theme } from './types';
import { validateTheme } from './validate';

async function load(
  text: string,
  manifest: ThemeManifest = MANIFEST,
  koraVersion: string | null = '1.3.0',
): Promise<Theme> {
  const result = await validateTheme(text, { manifest, koraVersion });
  if (!result.ok) throw new Error(result.errors.map((e) => `${e.path}: ${e.message}`).join('\n'));
  return result.theme;
}

describe('generateThemeCss', () => {
  it('puts a dark-only token in the dark block only, and omits an empty light block', async () => {
    const theme = await load(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dark: "#0d0f11"\n',
    );
    const css = generateThemeCss(theme);
    expect(css).toBe(`${DARK_SELECTOR}{--hades-raw-black:#0d0f11}`);
    expect(css).not.toContain(LIGHT_SELECTOR);
  });

  it('puts a light-only token in the light block only', async () => {
    const theme = await load(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    light: "#fff"\n',
    );
    expect(generateThemeCss(theme)).toBe(`${LIGHT_SELECTOR}{--hades-raw-black:#fff}`);
  });

  it('returns an empty string for a theme that sets no token and no font', async () => {
    const theme = await load('koraVersion: "1.0.0"\nproductName: Acme\n');
    expect(generateThemeCss(theme)).toBe('');
    expect(fontPreloads(theme)).toEqual([]);
  });

  it('declares a face per theme font and sets the family token in both blocks', async () => {
    const theme = await load(FULL_DOCUMENT);
    const css = generateThemeCss(theme);
    const lines = css.split('\n');
    expect(lines).toHaveLength(4);
    expect(lines[0]).toBe(
      '@font-face{font-family:"hades-theme-mono";src:url("https://cdn.example/mono.woff2") format("woff2");font-display:swap}',
    );
    expect(lines[1]).toBe(
      '@font-face{font-family:"hades-theme-sans";src:url("https://cdn.example/sans.woff2") format("woff2");font-display:swap}',
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
    expect(fontPreloads(theme)).toEqual([
      'https://cdn.example/mono.woff2',
      'https://cdn.example/sans.woff2',
    ]);
  });

  // The validator refuses a quote, a backslash or `<` in a URL, so these build
  // the theme by hand: the generator escapes what it writes whatever reached it.
  const withFontUrl = (url: string): Theme => ({
    koraVersion: '1.0.0',
    tokens: {},
    slots: { 'font.sans': { kind: 'font', url } },
    hash: '',
  });

  it('escapes the URL inside the CSS string', () => {
    const css = generateThemeCss(withFontUrl('https://cdn.example/a"b\\c.woff2'));
    expect(css).toContain('src:url("https://cdn.example/a\\"b\\\\c.woff2")');
  });

  it('escapes < so a URL cannot close the inline <style> element', () => {
    const css = generateThemeCss(withFontUrl('https://cdn.example/x</style><script>.woff2'));
    expect(css).not.toContain('<');
    expect(css).toContain('\\3c /style>');
  });

  it('is deterministic whatever the document order', async () => {
    const a = await load(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dark: "#000"\n  --hades-raw-accent-400:\n    dark: "#111"\n',
    );
    const b = await load(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-accent-400:\n    dark: "#111"\n  --hades-raw-black:\n    dark: "#000"\n',
    );
    expect(generateThemeCss(a)).toBe(generateThemeCss(b));
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
      koraVersion: '1.0.0',
      tokens,
      slots: {
        'font.sans': 'https://cdn.example/sans.woff2',
        'font.mono': 'https://cdn.example/mono.woff2',
      },
    });
    const theme = await load(doc, manifest, '1.0.0');
    const css = generateThemeCss(theme);
    const preloads = fontPreloads(theme)
      .map((url) => `<link rel="preload" as="font" type="font/woff2" crossorigin href="${url}">`)
      .join('');
    const bytes = new TextEncoder().encode(`<style>${css}</style>${preloads}`).length;
    expect(bytes).toBeLessThan(16 * 1024);
  });
});

describe('generateThemeCss: font family tokens', () => {
  const fontCss = async (value: string) => {
    const theme = await load(
      `koraVersion: "1.0.0"\ntokens:\n  --hades-raw-font-sans:\n    dark: ${JSON.stringify(value)}\n`,
    );
    return generateThemeCss(theme);
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
