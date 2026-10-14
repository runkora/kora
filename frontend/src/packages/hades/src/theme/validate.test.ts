// @vitest-environment node
import { describe, expect, it } from 'vitest';

import manifestJson from '../../manifest.json';
import { CONTRACT_VERSION } from '../version';
import {
  FULL_BUNDLE,
  FULL_DOCUMENT,
  MANIFEST,
  NOT_WOFF2,
  SVG,
  SVG_DATA_IMAGE,
  SVG_DOCTYPE,
  SVG_EXTERNAL_HREF,
  SVG_EXTERNAL_URL,
  SVG_ONLOAD,
  SVG_SCRIPT,
  WOFF2,
  bundle,
} from './fixtures.test-support';
import type { ThemeManifest } from './manifest';
import { validateTheme } from './validate';

const CONTRACT = '1.3';

async function run(
  text: string,
  files: Record<string, Uint8Array> = FULL_BUNDLE,
  manifest = MANIFEST,
) {
  return validateTheme(text, { manifest, contractVersion: CONTRACT, readAsset: bundle(files) });
}

function errorPaths(result: Awaited<ReturnType<typeof run>>): string[] {
  return result.ok ? [] : result.errors.map((e) => e.path);
}

describe('validateTheme: a full theme', () => {
  it('passes, carries every value unchanged and reports no fall-through', async () => {
    const result = await run(FULL_DOCUMENT);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme.contractVersion).toBe('1.2');
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
      paths: { dark: 'assets/logo-dark.svg', light: 'assets/logo-light.svg' },
    });
    expect(result.theme.slots['font.sans']).toEqual({ kind: 'font', path: 'assets/sans.woff2' });
    expect([...result.theme.assets.keys()].sort()).toEqual(Object.keys(FULL_BUNDLE).sort());
    expect(result.theme.assets.get('assets/sans.woff2')).toBe(WOFF2);
    expect(result.theme.hash).toMatch(/^[0-9a-f]{64}$/);
    expect(result.report).toEqual({ ignored: [], fallThrough: [] });
  });

  it('accepts JSON, which is YAML', async () => {
    const result = await run(
      JSON.stringify({ contractVersion: '1.0', tokens: { '--hades-raw-black': { dark: '#000' } } }),
    );
    expect(result.ok).toBe(true);
  });
});

describe('validateTheme: partial themes and the report', () => {
  it('passes a partial theme and reports every identity value left to fall through', async () => {
    const result = await run(
      `contractVersion: "1.0"\nslots:\n  logo:\n    dark: assets/logo-dark.svg\n`,
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
    const result = await run('contractVersion: "1.9"\n', {});
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme.tokens).toEqual({});
    expect(result.theme.slots).toEqual({});
    expect(result.theme.assets.size).toBe(0);
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
      `contractVersion: "1.0"\ntokens:\n  --hades-raw-future:\n    dark: "#fff"\n  --hades-raw-black:\n    dark: "#000"\nslots:\n  logo.mark:\n    dark: assets/logo-dark.svg\n`,
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.report.ignored).toEqual([
      { kind: 'token', name: '--hades-raw-future' },
      { kind: 'slot', name: 'logo.mark' },
    ]);
    expect(Object.keys(result.theme.tokens)).toEqual(['--hades-raw-black']);
    expect(result.theme.assets.size).toBe(0);
  });

  it('still reports ignored entries when validation fails', async () => {
    const result = await run(
      `contractVersion: "1.0"\nextra: 1\ntokens:\n  --hades-raw-future:\n    dark: "#fff"\n`,
    );
    expect(result.ok).toBe(false);
    expect(result.report.ignored).toEqual([{ kind: 'token', name: '--hades-raw-future' }]);
  });
});

describe('validateTheme: document faults', () => {
  it('fails on text that is not YAML or not a map', async () => {
    expect(errorPaths(await run('contractVersion: [1.0'))).toEqual(['']);
    expect(errorPaths(await run('- a\n- b\n'))).toEqual(['']);
    expect(errorPaths(await run('just a string'))).toEqual(['']);
  });

  it('fails on an unknown top-level key', async () => {
    const result = await run('contractVersion: "1.0"\nfonts: {}\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([{ path: 'fonts', message: 'unknown top-level key' }]);
  });

  it('requires contractVersion as a quoted MAJOR.MINOR string', async () => {
    expect(errorPaths(await run('productName: Acme\n'))).toEqual(['contractVersion']);
    expect(errorPaths(await run('contractVersion: "1.0.0"\n'))).toEqual(['contractVersion']);
    expect(errorPaths(await run('contractVersion: "1"\n'))).toEqual(['contractVersion']);
  });

  it('refuses an unquoted version, which YAML reads as a number, and says to quote it', async () => {
    const result = await run('contractVersion: 1.3\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ path: 'contractVersion' });
    expect(result.errors[0]!.message).toContain('quoted string');
    // 1.10 would silently read as 1.1, which is why a number is never accepted.
    expect((await run('contractVersion: 1.10\n')).ok).toBe(false);
  });

  it('fails on a contract in another major and names both majors', async () => {
    const result = await run('contractVersion: "2.0"\n');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ path: 'contractVersion' });
    expect(result.errors[0]!.message).toContain('major 2');
    expect(result.errors[0]!.message).toContain('1.3');
    expect((await run('contractVersion: "0.9"\n')).ok).toBe(false);
  });

  it('accepts a newer minor and an older minor in the same major', async () => {
    expect((await run('contractVersion: "1.99"\n')).ok).toBe(true);
    expect((await run('contractVersion: "1.0"\n')).ok).toBe(true);
  });

  it('bounds the product name after trimming and refuses its excluded characters', async () => {
    expect((await run('contractVersion: "1.0"\nproductName: "   "\n')).ok).toBe(false);
    expect((await run(`contractVersion: "1.0"\nproductName: ${'a'.repeat(41)}\n`)).ok).toBe(false);
    expect((await run(`contractVersion: "1.0"\nproductName: " ${'a'.repeat(40)} "\n`)).ok).toBe(
      true,
    );
    expect(errorPaths(await run('contractVersion: "1.0"\nproductName: "<b>Acme</b>"\n'))).toEqual([
      'productName',
    ]);
    expect(errorPaths(await run('contractVersion: "1.0"\nproductName: 42\n'))).toEqual([
      'productName',
    ]);
  });

  it('refuses an identity value set to nothing', async () => {
    expect(errorPaths(await run('contractVersion: "1.0"\nproductName: null\n'))).toEqual([
      'productName',
    ]);
    expect(
      errorPaths(await run('contractVersion: "1.0"\nslots:\n  logo:\n    dark: null\n')),
    ).toEqual(['slots.logo.dark']);
    expect(
      errorPaths(await run('contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    dark:\n')),
    ).toEqual(['tokens.--hades-raw-black.dark']);
  });
});

describe('validateTheme: token faults', () => {
  it('fails a malformed colour naming its path, and collects every fault', async () => {
    const result = await run(
      `contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    dark: "#12345"\n    light: red\n  --hades-raw-radius-md:\n    dark: 6\n`,
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
        await run('contractVersion: "1.0"\ntokens:\n  --hades-raw-black:\n    dim: "#000"\n'),
      ),
    ).toEqual(['tokens.--hades-raw-black.dim']);
    expect(
      errorPaths(await run('contractVersion: "1.0"\ntokens:\n  --hades-raw-black: "#000"\n')),
    ).toEqual(['tokens.--hades-raw-black']);
    expect(errorPaths(await run('contractVersion: "1.0"\ntokens: []\n'))).toEqual(['tokens']);
    const injected = await run(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-shadow-sm:\n    dark: "0 0 0 #000; } html { display: none"\n',
    );
    expect(injected.ok).toBe(false);
    if (injected.ok) return;
    expect(injected.errors[0]!.message).toContain('no token value may carry');
  });

  it('fails a theme that sets a font slot and the same slot family token', async () => {
    const result = await run(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-font-sans:\n    dark: Inter\nslots:\n  font.sans: assets/sans.woff2\n',
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([
      expect.objectContaining({ path: 'tokens.--hades-raw-font-sans' }),
    ]);
    expect(result.errors[0]!.message).toContain('font.sans');
    const other = await run(
      'contractVersion: "1.0"\ntokens:\n  --hades-raw-font-mono:\n    dark: Menlo\nslots:\n  font.sans: assets/sans.woff2\n',
    );
    expect(other.ok).toBe(true);
  });
});

describe('validateTheme: slot and asset faults', () => {
  const logo = (path: string) =>
    `contractVersion: "1.0"\nslots:\n  logo:\n    dark: ${JSON.stringify(path)}\n`;

  it('fails a path that escapes the bundle or is absolute', async () => {
    for (const path of [
      '../theme.yaml',
      'assets/../../etc/passwd',
      '/etc/passwd',
      'assets\\logo.svg',
      'assets//logo.svg',
      '',
    ]) {
      const result = await run(logo(path));
      expect(result.ok, path).toBe(false);
      expect(errorPaths(result)).toEqual(['slots.logo.dark']);
    }
  });

  it('fails a missing asset', async () => {
    const result = await run(logo('assets/missing.svg'));
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]!.message).toContain('not in the bundle');
  });

  it('fails an oversized asset naming the limit', async () => {
    const big = new Uint8Array(2049);
    big.set(SVG);
    const result = await run(logo('assets/logo-dark.svg'), { 'assets/logo-dark.svg': big });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]!.message).toContain('larger than');
    expect(result.errors[0]!.message).toContain('2048');
  });

  it('asks the reader for no more than the largest limit of the slots that use the path', async () => {
    const limits: number[] = [];
    await validateTheme(
      'contractVersion: "1.0"\nslots:\n  favicon:\n    dark: assets/favicon.svg\n    light: assets/favicon.svg\n',
      {
        manifest: MANIFEST,
        contractVersion: CONTRACT,
        readAsset: async (_path, maxBytes) => {
          limits.push(maxBytes);
          return SVG;
        },
      },
    );
    expect(limits).toEqual([MANIFEST.slots.find((s) => s.name === 'favicon')!.maxBytes]);
  });

  it('fails a wrong extension before reading content', async () => {
    const result = await run(logo('assets/logo.png'), { 'assets/logo.png': SVG });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]!.message).toContain('svg');
    expect(errorPaths(await run(logo('assets/logo'), { 'assets/logo': SVG }))).toEqual([
      'slots.logo.dark',
    ]);
  });

  it('fails an SVG with a script, an event attribute, an external href, an external url() or a DOCTYPE', async () => {
    const cases: [Uint8Array, string][] = [
      [SVG_SCRIPT, '<script>'],
      [SVG_ONLOAD, 'event attribute'],
      [SVG_EXTERNAL_HREF, 'href'],
      [SVG_EXTERNAL_URL, 'url()'],
      [SVG_DOCTYPE, 'DOCTYPE'],
    ];
    for (const [bytes, expected] of cases) {
      const result = await run(logo('assets/logo-dark.svg'), { 'assets/logo-dark.svg': bytes });
      expect(result.ok, expected).toBe(false);
      if (result.ok) return;
      expect(result.errors[0]!.message, expected).toContain(expected);
    }
  });

  it('accepts an SVG with fragment references and inline data images', async () => {
    expect(
      (await run(logo('assets/logo-dark.svg'), { 'assets/logo-dark.svg': SVG_DATA_IMAGE })).ok,
    ).toBe(true);
    expect((await run(logo('assets/logo-dark.svg'), { 'assets/logo-dark.svg': SVG })).ok).toBe(
      true,
    );
  });

  it('fails a font that is not WOFF2', async () => {
    const result = await run('contractVersion: "1.0"\nslots:\n  font.mono: assets/mono.woff2\n', {
      'assets/mono.woff2': NOT_WOFF2,
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([expect.objectContaining({ path: 'slots.font.mono' })]);
    expect(result.errors[0]!.message).toContain('WOFF2');
  });

  it('fails an image slot given a string and a font slot given a map', async () => {
    expect(
      errorPaths(await run('contractVersion: "1.0"\nslots:\n  logo: assets/logo-dark.svg\n')),
    ).toEqual(['slots.logo']);
    expect(
      errorPaths(
        await run('contractVersion: "1.0"\nslots:\n  font.sans:\n    dark: assets/sans.woff2\n'),
      ),
    ).toEqual(['slots.font.sans']);
    expect(
      errorPaths(
        await run('contractVersion: "1.0"\nslots:\n  logo:\n    dim: assets/logo-dark.svg\n'),
      ),
    ).toEqual(['slots.logo.dim']);
  });

  it('reads an asset referenced twice once', async () => {
    let reads = 0;
    const result = await validateTheme(
      'contractVersion: "1.0"\nslots:\n  favicon:\n    dark: assets/favicon.svg\n    light: assets/favicon.svg\n',
      {
        manifest: MANIFEST,
        contractVersion: CONTRACT,
        readAsset: async () => {
          reads++;
          return SVG;
        },
      },
    );
    expect(result.ok).toBe(true);
    expect(reads).toBe(1);
  });
});

describe('validateTheme: the hash', () => {
  it('is stable for the same bundle and changes when an asset byte or the document changes', async () => {
    const a = await run(FULL_DOCUMENT);
    const b = await run(FULL_DOCUMENT);
    expect(a.ok && b.ok && a.theme.hash === b.theme.hash).toBe(true);

    const changed = new Uint8Array(WOFF2);
    changed[changed.length - 1] = 0xff;
    const c = await run(FULL_DOCUMENT, { ...FULL_BUNDLE, 'assets/mono.woff2': changed });
    expect(c.ok && a.ok && c.theme.hash !== a.theme.hash).toBe(true);

    const d = await run(`${FULL_DOCUMENT}\n# a comment\n`);
    expect(d.ok && a.ok && d.theme.hash !== a.theme.hash).toBe(true);
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
    const doc = JSON.stringify({ contractVersion: CONTRACT_VERSION, productName: 'Acme', tokens });
    const result = await validateTheme(doc, {
      manifest,
      contractVersion: CONTRACT_VERSION,
      readAsset: bundle({}),
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
