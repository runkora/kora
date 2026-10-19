// @vitest-environment node
import { describe, expect, it } from 'vitest';

import manifestJson from '../../manifest.json';
import { FULL_DOCUMENT, MANIFEST } from './fixtures.test-support';
import type { ThemeManifest } from './manifest';
import { validateTheme } from './validate';

const RUNNING = '1.3.0';

async function run(text: string, manifest = MANIFEST) {
  return validateTheme(text, { manifest, koraVersion: RUNNING });
}

function errorPaths(result: Awaited<ReturnType<typeof run>>): string[] {
  return result.ok ? [] : result.errors.map((e) => e.path);
}

describe('validateTheme: a full theme', () => {
  it('passes, carries every value unchanged and reports no fall-through', async () => {
    const result = await run(FULL_DOCUMENT);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme.koraVersion).toBe('1.2.0');
    expect(result.theme.productName).toBe("Acme & Co.'s Agents");
    expect(result.theme.tokens['--hades-raw-black']).toEqual({
      dark: '#0d0f11',
      light: 'oklch(97% 0.01 90)',
    });
    expect(result.theme.tokens['--hades-raw-accent-400']).toEqual({
      dark: 'oklch(70% 0.12 240 / 0.9)',
    });
    expect(result.theme.tokens['--hades-raw-shadow-sm']).toEqual({
      dark: '0 2px 8px oklch(0% 0 0 / 0.25), inset 0 1px 0 #ffffff10',
      light: '0 1px 2px rgba(0, 0, 0, 0.1)',
    });
    expect(result.theme.slots.logo).toEqual({
      kind: 'image',
      urls: {
        dark: 'https://cdn.example/logo-dark.svg',
        light: 'https://cdn.example/logo-light.svg',
      },
    });
    expect(result.theme.slots['font.sans']).toEqual({
      kind: 'font',
      url: 'https://cdn.example/sans.woff2',
    });
    expect(result.theme.hash).toMatch(/^[0-9a-f]{64}$/);
    expect(result.report).toEqual({
      ignored: [],
      fallThrough: [],
      mixedIdentity: [],
      versionCheckSkipped: false,
    });
  });

  it('accepts JSON, which is YAML', async () => {
    const result = await run(
      JSON.stringify({ koraVersion: '1.0.0', tokens: { '--hades-raw-black': { dark: '#000' } } }),
    );
    expect(result.ok).toBe(true);
  });
});

describe('validateTheme: partial themes and the report', () => {
  it('passes a partial theme and reports every identity value left to fall through', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: https://cdn.example/logo-dark.svg\n`,
    );
    expect(result.ok).toBe(true);
    expect(result.report.fallThrough).toEqual([
      'productName',
      'favicon.dark',
      'favicon.light',
      'logo.light',
    ]);
  });

  it('passes a version-only document and reports everything falling through', async () => {
    const result = await run('koraVersion: "1.9.0"\n');
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme.tokens).toEqual({});
    expect(result.theme.slots).toEqual({});
    expect(result.report.fallThrough).toEqual([
      'productName',
      'favicon.dark',
      'favicon.light',
      'logo.dark',
      'logo.light',
    ]);
  });

  it('ignores and reports unknown tokens and slots without failing', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\ntokens:\n  --hades-raw-future:\n    dark: "#fff"\n  --hades-raw-black:\n    dark: "#000"\nslots:\n  logo.mark:\n    dark: https://cdn.example/logo-dark.svg\n`,
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.report.ignored).toEqual([
      { kind: 'token', name: '--hades-raw-future' },
      { kind: 'slot', name: 'logo.mark' },
    ]);
    expect(Object.keys(result.theme.tokens)).toEqual(['--hades-raw-black']);
  });

  it('still reports ignored entries when validation fails', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\nextra: 1\ntokens:\n  --hades-raw-future:\n    dark: "#fff"\n`,
    );
    expect(result.ok).toBe(false);
    expect(result.report.ignored).toEqual([{ kind: 'token', name: '--hades-raw-future' }]);
  });
});

describe('validateTheme: mixed identity', () => {
  const ALL = ['productName', 'favicon.dark', 'favicon.light', 'logo.dark', 'logo.light'];

  it('is empty when no identity value is set', async () => {
    const result = await run('koraVersion: "1.0.0"\n');
    expect(result.ok).toBe(true);
    expect(result.report.fallThrough).toEqual(ALL);
    expect(result.report.mixedIdentity).toEqual([]);
  });

  it('is empty when every identity value is set', async () => {
    const result = await run(FULL_DOCUMENT);
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([]);
  });

  it('lists the fall-through values when only the product name is set', async () => {
    const result = await run('koraVersion: "1.0.0"\nproductName: Meridian\n');
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([
      'favicon.dark',
      'favicon.light',
      'logo.dark',
      'logo.light',
    ]);
    expect(result.report.mixedIdentity).toEqual(result.report.fallThrough);
  });

  it('lists the fall-through values when only the dark logo is set', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: https://cdn.example/logo-dark.svg\n`,
    );
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([
      'productName',
      'favicon.dark',
      'favicon.light',
      'logo.light',
    ]);
  });

  it('lists the fall-through values when only the dark favicon is set', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\nslots:\n  favicon:\n    dark: https://cdn.example/favicon.svg\n`,
    );
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([
      'productName',
      'favicon.light',
      'logo.dark',
      'logo.light',
    ]);
  });

  it('is empty for a colours-only theme', async () => {
    const result = await run(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dark: "#000"\n',
    );
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([]);
  });

  it('is empty for a fonts-only theme', async () => {
    const result = await run(
      'koraVersion: "1.0.0"\nslots:\n  font.sans: https://cdn.example/sans.woff2\n',
    );
    expect(result.ok).toBe(true);
    expect(result.report.mixedIdentity).toEqual([]);
  });
});

describe('validateTheme: document faults', () => {
  it('fails on text that is not YAML or not a map', async () => {
    expect(errorPaths(await run('koraVersion: [1.0'))).toEqual(['']);
    expect(errorPaths(await run('- a\n- b\n'))).toEqual(['']);
    expect(errorPaths(await run('just a string'))).toEqual(['']);
  });

  it('fails on an unknown top-level key', async () => {
    const result = await run('koraVersion: "1.0.0"\nfonts: {}\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([{ path: 'fonts', message: 'unknown top-level key' }]);
  });

  it('requires koraVersion as a quoted MAJOR.MINOR.PATCH string', async () => {
    expect(errorPaths(await run('productName: Acme\n'))).toEqual(['koraVersion']);
    expect(errorPaths(await run('koraVersion: "1.0"\n'))).toEqual(['koraVersion']);
    expect(errorPaths(await run('koraVersion: "1"\n'))).toEqual(['koraVersion']);
    expect(errorPaths(await run('koraVersion: "1.0.0-rc.1"\n'))).toEqual(['koraVersion']);
  });

  it('refuses an unquoted version, which YAML reads as a number, and says to quote it', async () => {
    const result = await run('koraVersion: 1.3\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ path: 'koraVersion' });
    expect(result.errors[0]!.message).toContain('quoted string');
    // 1.10 would silently read as 1.1, which is why a number is never accepted.
    expect((await run('koraVersion: 1.10\n')).ok).toBe(false);
  });

  it('fails on a theme written against another major and names both releases', async () => {
    const result = await run('koraVersion: "2.0.0"\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ path: 'koraVersion' });
    expect(result.errors[0]!.message).toContain('major 2');
    expect(result.errors[0]!.message).toContain('1.3.0');
    expect((await run('koraVersion: "0.9.0"\n')).ok).toBe(false);
  });

  it('accepts a newer and an older release in the same major', async () => {
    expect((await run('koraVersion: "1.99.7"\n')).ok).toBe(true);
    expect((await run('koraVersion: "1.0.0"\n')).ok).toBe(true);
  });

  it('compares majors against an instance running a release candidate', async () => {
    const options = (koraVersion: string | null) => ({
      manifest: MANIFEST,
      koraVersion,
    });
    expect((await validateTheme('koraVersion: "0.8.0"\n', options('0.8.1-rc.2'))).ok).toBe(true);
    expect((await validateTheme('koraVersion: "1.0.0"\n', options('0.8.1-rc.2'))).ok).toBe(false);
  });

  it('checks the shape but skips the comparison on a development build, and says so', async () => {
    const result = await validateTheme('koraVersion: "7.0.0"\n', {
      manifest: MANIFEST,
      koraVersion: null,
    });
    expect(result.ok).toBe(true);
    expect(result.report.versionCheckSkipped).toBe(true);
    const bad = await validateTheme('koraVersion: "7.0"\n', {
      manifest: MANIFEST,
      koraVersion: null,
    });
    expect(bad.ok).toBe(false);
    expect((await run('koraVersion: "1.0.0"\n')).report.versionCheckSkipped).toBe(false);
  });

  it('refuses an instance version it cannot read rather than skipping silently', async () => {
    const result = await validateTheme('koraVersion: "1.0.0"\n', {
      manifest: MANIFEST,
      koraVersion: 'latest',
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]!.message).toContain('cannot tell which kora release it is');
  });

  it('bounds the product name after trimming and refuses its excluded characters', async () => {
    expect((await run('koraVersion: "1.0.0"\nproductName: "   "\n')).ok).toBe(false);
    expect((await run(`koraVersion: "1.0.0"\nproductName: ${'a'.repeat(41)}\n`)).ok).toBe(false);
    expect((await run(`koraVersion: "1.0.0"\nproductName: " ${'a'.repeat(40)} "\n`)).ok).toBe(true);
    expect(errorPaths(await run('koraVersion: "1.0.0"\nproductName: "<b>Acme</b>"\n'))).toEqual([
      'productName',
    ]);
    expect(errorPaths(await run('koraVersion: "1.0.0"\nproductName: 42\n'))).toEqual([
      'productName',
    ]);
  });

  it('refuses an identity value set to nothing', async () => {
    expect(errorPaths(await run('koraVersion: "1.0.0"\nproductName: null\n'))).toEqual([
      'productName',
    ]);
    expect(
      errorPaths(await run('koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: null\n')),
    ).toEqual(['slots.logo.dark']);
    expect(
      errorPaths(await run('koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dark:\n')),
    ).toEqual(['tokens.--hades-raw-black.dark']);
  });
});

describe('validateTheme: token faults', () => {
  it('fails a malformed colour naming its path, and collects every fault', async () => {
    const result = await run(
      `koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dark: "#12345"\n    light: red\n  --hades-raw-radius-md:\n    dark: 6\n`,
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors.map((e) => e.path)).toEqual([
      'tokens.--hades-raw-black.dark',
      'tokens.--hades-raw-black.light',
      'tokens.--hades-raw-radius-md.dark',
    ]);
    expect(result.errors[0]!.message).toContain('#12345');
    expect(result.errors[0]!.message).toContain('hex-or-oklch');
  });

  it('fails an unknown mode, a non-map token and an injected value', async () => {
    expect(
      errorPaths(
        await run('koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black:\n    dim: "#000"\n'),
      ),
    ).toEqual(['tokens.--hades-raw-black.dim']);
    expect(
      errorPaths(await run('koraVersion: "1.0.0"\ntokens:\n  --hades-raw-black: "#000"\n')),
    ).toEqual(['tokens.--hades-raw-black']);
    expect(errorPaths(await run('koraVersion: "1.0.0"\ntokens: []\n'))).toEqual(['tokens']);
    const injected = await run(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-shadow-sm:\n    dark: "0 0 0 #000; } html { display: none"\n',
    );
    expect(injected.ok).toBe(false);
    if (injected.ok) return;
    expect(injected.errors[0]!.message).toContain('no token value may carry');
  });

  it('fails a theme that sets a font slot and the same slot family token', async () => {
    const result = await run(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-font-sans:\n    dark: Inter\nslots:\n  font.sans: https://cdn.example/sans.woff2\n',
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([
      expect.objectContaining({ path: 'tokens.--hades-raw-font-sans' }),
    ]);
    expect(result.errors[0]!.message).toContain('font.sans');
    const other = await run(
      'koraVersion: "1.0.0"\ntokens:\n  --hades-raw-font-mono:\n    dark: Menlo\nslots:\n  font.sans: https://cdn.example/sans.woff2\n',
    );
    expect(other.ok).toBe(true);
  });
});

describe('validateTheme: slot URL faults', () => {
  const logo = (url: string) =>
    `koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: ${JSON.stringify(url)}\n`;

  it('accepts an https URL with a port, a path and a query', async () => {
    for (const url of [
      'https://cdn.example/logo.svg',
      'https://cdn.example:8443/brand/2026/logo.dark.svg',
      'https://cdn.example/logo.SVG?v=3&sig=abc',
      'https://xn--caf-dma.example/logo.svg',
    ]) {
      expect((await run(logo(url))).ok, url).toBe(true);
    }
  });

  it('fails a URL that is not a plain https URL', async () => {
    for (const url of [
      'http://cdn.example/logo.svg',
      '//cdn.example/logo.svg',
      '/assets/logo.svg',
      'assets/logo.svg',
      'data:image/svg+xml;base64,AAAA',
      'javascript:alert(1)//.svg',
      'https://user:pass@cdn.example/logo.svg',
      'https://cdn.example/logo.svg#frag',
      'https://cdn.example/a b.svg',
      'https://cdn.example/a".svg',
      'https://cdn.example/a(1).svg',
      'https://cdn.example/a\\b.svg',
      'https://cdn.example/x?q=a b',
      'https://.example/logo.svg',
      `https://cdn.example/${'a'.repeat(2048)}.svg`,
      '',
    ]) {
      const result = await run(logo(url));
      expect(result.ok, url).toBe(false);
      expect(errorPaths(result), url).toEqual(['slots.logo.dark']);
    }
  });

  it('fails a wrong or missing extension, looking at the path and not the query', async () => {
    for (const url of [
      'https://cdn.example/logo.png',
      'https://cdn.example/logo',
      'https://cdn.example',
      'https://cdn.example/',
      'https://cdn.example/.svg',
      'https://cdn.example/logo.svg/',
      'https://cdn.example/logo?name=a.svg',
      'https://cdn.example/?x=/a.svg',
    ]) {
      const result = await run(logo(url));
      expect(result.ok, url).toBe(false);
      expect(errorPaths(result), url).toEqual(['slots.logo.dark']);
    }
    const result = await run(logo('https://cdn.example/logo.png'));
    if (result.ok) return;
    expect(result.errors[0]!.message).toContain('svg');
  });

  it('holds a font to .woff2', async () => {
    expect(
      (await run('koraVersion: "1.0.0"\nslots:\n  font.mono: https://cdn.example/m.woff2\n')).ok,
    ).toBe(true);
    const result = await run(
      'koraVersion: "1.0.0"\nslots:\n  font.mono: https://cdn.example/m.woff\n',
    );
    expect(errorPaths(result)).toEqual(['slots.font.mono']);
  });

  it('fails an image slot given a string and a font slot given a map', async () => {
    expect(
      errorPaths(await run('koraVersion: "1.0.0"\nslots:\n  logo: https://cdn.example/l.svg\n')),
    ).toEqual(['slots.logo']);
    expect(
      errorPaths(
        await run(
          'koraVersion: "1.0.0"\nslots:\n  font.sans:\n    dark: https://cdn.example/s.woff2\n',
        ),
      ),
    ).toEqual(['slots.font.sans']);
    expect(
      errorPaths(
        await run('koraVersion: "1.0.0"\nslots:\n  logo:\n    dim: https://cdn.example/l.svg\n'),
      ),
    ).toEqual(['slots.logo.dim']);
  });

  it('refuses a number, a list and null where a URL belongs', async () => {
    for (const value of ['5', '[a]', 'null']) {
      const result = await run(`koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: ${value}\n`);
      expect(errorPaths(result), value).toEqual(['slots.logo.dark']);
    }
  });
});

describe('validateTheme: the hash', () => {
  it('is stable for the same document and changes when the document does', async () => {
    const a = await run(FULL_DOCUMENT);
    const b = await run(FULL_DOCUMENT);
    expect(a.ok && b.ok && a.theme.hash === b.theme.hash).toBe(true);

    const c = await run(`${FULL_DOCUMENT}\n# a comment\n`);
    expect(c.ok && a.ok && c.theme.hash !== a.theme.hash).toBe(true);
  });
});

describe('validateTheme: against the real manifest', () => {
  const manifest = manifestJson as ThemeManifest;

  it('accepts a theme that sets every token in both modes and every slot', async () => {
    const sample: Record<string, string> = {
      'hex-or-oklch': 'oklch(62.5% 0.15 250.5 / 0.85)',
      'css-shadow-list': '0 2px 8px oklch(0% 0 0 / 0.25), 0 1px 2px #00000040',
      'css-length': '0.75rem',
      'font-family-name': 'Inter Variable',
    };
    const tokens: Record<string, { dark: string; light: string }> = {};
    for (const token of manifest.tokens) {
      tokens[token.name] = { dark: sample[token.format]!, light: sample[token.format]! };
    }
    const doc = JSON.stringify({ koraVersion: '1.0.0', productName: 'Acme', tokens });
    const result = await validateTheme(doc, {
      manifest,
      koraVersion: '1.0.0',
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(Object.keys(result.theme.tokens)).toHaveLength(manifest.tokens.length);
    expect(result.report.fallThrough).toEqual([
      'favicon.dark',
      'favicon.light',
      'logo.dark',
      'logo.light',
    ]);
  });
});

describe('validateTheme: empty maps, code points and unreadable assets', () => {
  it('treats an empty tokens or slots value as empty', async () => {
    for (const doc of [
      'koraVersion: "1.0.0"\ntokens:\n',
      'koraVersion: "1.0.0"\nslots:\n',
      'koraVersion: "1.0.0"\ntokens:\nslots:\n',
    ]) {
      expect((await run(doc)).ok).toBe(true);
    }
    const nullName = await run('koraVersion: "1.0.0"\nproductName:\n');
    expect(errorPaths(nullName)).toEqual(['productName']);
  });

  it('counts code points in the product name, not UTF-16 units', async () => {
    const letter = String.fromCodePoint(0x1d49c);
    expect((await run(`koraVersion: "1.0.0"\nproductName: ${letter.repeat(21)}\n`)).ok).toBe(true);
    expect((await run(`koraVersion: "1.0.0"\nproductName: ${letter.repeat(40)}\n`)).ok).toBe(true);
    const over = await run(`koraVersion: "1.0.0"\nproductName: ${letter.repeat(41)}\n`);
    expect(errorPaths(over)).toEqual(['productName']);
  });

  it('refuses font family tokens that would emit invalid CSS', async () => {
    for (const value of [',', 'Inter,', 'inherit', 'Inter, unset']) {
      const result = await run(
        `koraVersion: "1.0.0"\ntokens:\n  --hades-raw-font-sans:\n    dark: ${JSON.stringify(value)}\n`,
      );
      expect(errorPaths(result)).toEqual(['tokens.--hades-raw-font-sans.dark']);
    }
  });
});
