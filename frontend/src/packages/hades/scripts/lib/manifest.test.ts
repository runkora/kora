import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { MODES, type Placement } from '../../src/tokens/surface.ts';
import {
  buildManifest,
  buildTokenDefaults,
  serializeManifest,
  SURFACE,
  SurfaceMismatchError,
  type Manifest,
  type SurfaceInput,
} from './manifest.ts';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const tokensCss = fs.readFileSync(path.join(packageDir, 'src', 'tokens', 'tokens.css'), 'utf8');

const SMALL_CSS = `
:root {
  --hades-raw-black: #0d0f11;
  --hades-raw-accent-400: #8aa7bb;
  --hades-raw-shadow-sm: 0 2px 8px oklch(0% 0 0 / 0.25);
}
[data-theme='light'] {
  --hades-raw-black: #ece9e2;
}
@theme inline {
  --color-background: var(--hades-raw-black);
  --color-accent: var(--hades-raw-accent-400);
  --color-accent-subtle: color-mix(in oklch, var(--color-accent) 10%, transparent);
  --shadow-sm: var(--hades-raw-shadow-sm);
  --font-sans: var(--font-inter, 'Inter', sans-serif);
  --radius-full: 9999px;
}
`;

const SMALL_SURFACE: SurfaceInput = {
  tokens: {
    '--hades-raw-black': { kind: 'color', group: 'neutral', label: 'black' },
    '--hades-raw-accent-400': { kind: 'color', group: 'branding', label: 'accent 400' },
    '--hades-raw-shadow-sm': { kind: 'shadow', group: 'shadows', label: 'small' },
  },
  semantic: {
    '--color-background': { group: 'neutral', label: 'background' },
    '--color-accent': { group: 'branding', label: 'accent' },
    '--color-accent-subtle': { group: 'branding', label: 'accent subtle' },
    '--shadow-sm': { group: 'shadows', label: 'small' },
    '--font-sans': { group: 'neutral', label: 'sans' },
    '--radius-full': { group: 'neutral', label: 'full' },
  },
  groups: [
    { id: 'shadows', label: 'Shadows', order: 3 },
    { id: 'branding', label: 'Branding', order: 1 },
    { id: 'neutral', label: 'Neutral', order: 2 },
  ],
  contrast: [
    {
      id: 'accent-on-background',
      text: '--color-accent',
      on: ['--color-background'],
      min: 4.5,
      severity: 'error',
    },
  ],
  slots: [{ name: 'favicon', kind: 'image', formats: ['svg'], modes: MODES }],
  productName: { minLength: 1, maxLength: 40, pattern: '^.+$' },
};

describe('buildManifest', () => {
  it('lists every raw token with its kind, group, label, format and both modes, sorted', () => {
    const m = buildManifest(SMALL_CSS, SMALL_SURFACE);
    expect(m).not.toHaveProperty('version');
    expect(m).not.toHaveProperty('contractVersion');
    expect(m.modes).toEqual(['dark', 'light']);
    expect(m.tokens).toEqual([
      {
        name: '--hades-raw-accent-400',
        kind: 'color',
        group: 'branding',
        label: 'accent 400',
        format: 'hex-or-oklch',
        modes: ['dark', 'light'],
      },
      {
        name: '--hades-raw-black',
        kind: 'color',
        group: 'neutral',
        label: 'black',
        format: 'hex-or-oklch',
        modes: ['dark', 'light'],
      },
      {
        name: '--hades-raw-shadow-sm',
        kind: 'shadow',
        group: 'shadows',
        label: 'small',
        format: 'css-shadow-list',
        modes: ['dark', 'light'],
      },
    ]);
  });

  it('records direct, derived and literal mappings', () => {
    const { mapping } = buildManifest(SMALL_CSS, SMALL_SURFACE);
    expect(mapping['--color-background']).toEqual({
      group: 'neutral',
      label: 'background',
      tokens: ['--hades-raw-black'],
      derived: false,
    });
    expect(mapping['--color-accent-subtle']).toEqual({
      group: 'branding',
      label: 'accent subtle',
      tokens: ['--hades-raw-accent-400'],
      derived: true,
    });
    expect(mapping['--font-sans']).toEqual({
      group: 'neutral',
      label: 'sans',
      tokens: [],
      derived: false,
    });
    expect(mapping['--radius-full']).toMatchObject({ tokens: [], derived: false });
  });

  it('marks an alias of a derived name as derived', () => {
    const css = SMALL_CSS.replace(
      '--shadow-sm:',
      '--color-alias: var(--color-accent-subtle);\n  --shadow-sm:',
    );
    const surface: SurfaceInput = {
      ...SMALL_SURFACE,
      semantic: {
        ...SMALL_SURFACE.semantic,
        '--color-alias': { group: 'branding', label: 'alias' },
      },
    };
    expect(buildManifest(css, surface).mapping['--color-alias']).toMatchObject({
      tokens: ['--hades-raw-accent-400'],
      derived: true,
    });
    expect(() =>
      buildManifest(css, {
        ...surface,
        contrast: [
          {
            id: 'p',
            text: '--color-alias',
            on: ['--color-background'],
            min: 4.5,
            severity: 'error',
          },
        ],
      }),
    ).toThrow(/text --color-alias, which is derived/);
  });

  it('lists the groups in display order', () => {
    const m = buildManifest(SMALL_CSS, SMALL_SURFACE);
    expect(m.groups).toEqual([
      { id: 'branding', label: 'Branding', order: 1 },
      { id: 'neutral', label: 'Neutral', order: 2 },
      { id: 'shadows', label: 'Shadows', order: 3 },
    ]);
  });

  it('carries the slots and product-name bounds through', () => {
    const m = buildManifest(SMALL_CSS, SMALL_SURFACE);
    expect(m.slots).toEqual([
      {
        name: 'favicon',
        kind: 'image',
        formats: ['svg'],
        modes: ['dark', 'light'],
      },
    ]);
    expect(m.productName).toEqual(SMALL_SURFACE.productName);
  });

  it('fails naming a raw token missing from either side', () => {
    const missingKind: SurfaceInput = {
      ...SMALL_SURFACE,
      tokens: {
        '--hades-raw-black': SMALL_SURFACE.tokens['--hades-raw-black']!,
        '--hades-raw-shadow-sm': SMALL_SURFACE.tokens['--hades-raw-shadow-sm']!,
      },
    };
    expect(() => buildManifest(SMALL_CSS, missingKind)).toThrow(SurfaceMismatchError);
    expect(() => buildManifest(SMALL_CSS, missingKind)).toThrow(
      /--hades-raw-accent-400 is in tokens.css/,
    );

    const extraKind: SurfaceInput = {
      ...SMALL_SURFACE,
      tokens: {
        ...SMALL_SURFACE.tokens,
        '--hades-raw-ghost': { kind: 'color', group: 'neutral', label: 'ghost' },
      },
    };
    expect(() => buildManifest(SMALL_CSS, extraKind)).toThrow(
      /--hades-raw-ghost has a kind in surface.ts/,
    );
  });

  describe('groups', () => {
    const build = (surface: SurfaceInput) => () => buildManifest(SMALL_CSS, surface);

    it('fails when a semantic name has no entry, or an entry has no name', () => {
      const rest: Record<string, Placement> = { ...SMALL_SURFACE.semantic };
      delete rest['--color-accent'];
      expect(build({ ...SMALL_SURFACE, semantic: rest })).toThrow(
        /--color-accent is in tokens.css but has no group in surface.ts/,
      );
      expect(
        build({
          ...SMALL_SURFACE,
          semantic: {
            ...SMALL_SURFACE.semantic,
            '--color-ghost': { group: 'neutral', label: 'g' },
          },
        }),
      ).toThrow(/--color-ghost has a group in surface.ts but is not declared in tokens.css/);
    });

    it('fails when a token or semantic name declares no group', () => {
      const noGroup = {
        ...SMALL_SURFACE,
        tokens: {
          ...SMALL_SURFACE.tokens,
          '--hades-raw-black': { kind: 'color', label: 'black' },
        },
      } as unknown as SurfaceInput;
      expect(build(noGroup)).toThrow(/--hades-raw-black declares no group in surface.ts/);
    });

    it('fails when a group id is not declared', () => {
      const unknown: SurfaceInput = {
        ...SMALL_SURFACE,
        semantic: { ...SMALL_SURFACE.semantic, '--shadow-sm': { group: 'elevation', label: 'x' } },
      };
      expect(build(unknown)).toThrow(/--shadow-sm declares unknown group "elevation"/);
    });

    it('fails when a group has no member, or a label is empty', () => {
      const empty: SurfaceInput = {
        ...SMALL_SURFACE,
        groups: [...SMALL_SURFACE.groups, { id: 'motion', label: 'Motion', order: 4 }],
      };
      expect(build(empty)).toThrow(/group "motion" has no token or semantic name/);
      const unlabelled: SurfaceInput = {
        ...SMALL_SURFACE,
        tokens: {
          ...SMALL_SURFACE.tokens,
          '--hades-raw-black': { kind: 'color', group: 'neutral', label: '' },
        },
      };
      expect(build(unlabelled)).toThrow(/--hades-raw-black declares no label/);
    });

    it('fails when a group id or an order is repeated', () => {
      const dup: SurfaceInput = {
        ...SMALL_SURFACE,
        groups: [...SMALL_SURFACE.groups, { id: 'neutral', label: 'Again', order: 1 }],
      };
      expect(build(dup)).toThrow(/group "neutral" is declared twice/);
      expect(build(dup)).toThrow(/group "neutral" repeats order 1/);
    });
  });

  describe('contrast pairs', () => {
    const build = (surface: SurfaceInput) => () => buildManifest(SMALL_CSS, surface);
    const pair = SMALL_SURFACE.contrast[0]!;
    const withPairs = (...pairs: SurfaceInput['contrast'][number][]): SurfaceInput => ({
      ...SMALL_SURFACE,
      contrast: pairs,
    });

    it('lists the pairs sorted by id, with their names, minimum and severity', () => {
      const m = buildManifest(
        SMALL_CSS,
        withPairs(
          { ...pair, id: 'z-last', severity: 'warn' },
          { ...pair, id: 'a-first', on: ['--color-background'] },
        ),
      );
      expect(m.contrast).toEqual([
        {
          id: 'a-first',
          text: '--color-accent',
          on: ['--color-background'],
          min: 4.5,
          severity: 'error',
        },
        {
          id: 'z-last',
          text: '--color-accent',
          on: ['--color-background'],
          min: 4.5,
          severity: 'warn',
        },
      ]);
    });

    it('fails on an unknown, derived or non-colour name as text or on', () => {
      expect(build(withPairs({ ...pair, text: '--color-nope' }))).toThrow(
        /"accent-on-background" text --color-nope, which is not a semantic name/,
      );
      expect(build(withPairs({ ...pair, text: '--color-accent-subtle' }))).toThrow(
        /text --color-accent-subtle, which is derived/,
      );
      expect(build(withPairs({ ...pair, on: ['--color-accent-subtle'] }))).toThrow(
        /is on --color-accent-subtle, which is derived/,
      );
      expect(build(withPairs({ ...pair, on: ['--shadow-sm'] }))).toThrow(
        /is on --shadow-sm, which is not a colour/,
      );
    });

    it('fails on an empty or repeated id and an empty or repeated on', () => {
      expect(build(withPairs({ ...pair, id: '' }))).toThrow(/has an empty id/);
      expect(build(withPairs(pair, pair))).toThrow(/"accent-on-background" is declared twice/);
      expect(build(withPairs({ ...pair, on: [] }))).toThrow(/has an empty "on"/);
      expect(
        build(withPairs({ ...pair, on: ['--color-background', '--color-background'] })),
      ).toThrow(/lists --color-background twice/);
    });

    it('fails when the text is listed in its own on', () => {
      expect(build(withPairs({ ...pair, on: ['--color-accent'] }))).toThrow(
        /lists its own text --color-accent in "on"/,
      );
    });

    it('fails on a minimum outside 1 to 21 and a bad severity', () => {
      expect(build(withPairs({ ...pair, min: 0.5 }))).toThrow(
        /min 0.5; it must be between 1 and 21/,
      );
      expect(build(withPairs({ ...pair, min: 22 }))).toThrow(/min 22/);
      expect(build(withPairs({ ...pair, min: Number.NaN }))).toThrow(/min NaN/);
      expect(build(withPairs({ ...pair, severity: 'fatal' as unknown as 'error' }))).toThrow(
        /severity "fatal"; use error or warn/,
      );
    });

    it('is part of the real surface: every pair is valid and ids are unique', () => {
      const m = buildManifest(tokensCss, SURFACE);
      expect(m.contrast.map((p) => p.id)).toEqual(m.contrast.map((p) => p.id).sort());
      expect(m.contrast.find((p) => p.id === 'accent-foreground-on-accent')).toMatchObject({
        text: '--color-accent-foreground',
        on: ['--color-accent'],
        min: 4.5,
        severity: 'warn',
      });
      // Contrast must never fail a deployment until a pair is deliberately raised to error.
      expect(m.contrast.every((p) => p.severity === 'warn')).toBe(true);
      expect(m.contrast.filter((p) => p.id.startsWith('status-')).every((p) => p.min === 3)).toBe(
        true,
      );
    });
  });

  it('builds from the real token sheet and surface declaration', () => {
    const m = buildManifest(tokensCss, SURFACE);
    expect(m.tokens.length).toBeGreaterThan(30);
    expect(m.tokens.every((t) => t.name.startsWith('--hades-raw-'))).toBe(true);
    // Every token and mapping entry sits in a declared group, and no group is empty.
    const ids = m.groups.map((g) => g.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(new Set(m.groups.map((g) => g.order)).size).toBe(ids.length);
    const members = [...m.tokens, ...Object.values(m.mapping)];
    for (const member of members) expect(ids).toContain(member.group);
    for (const id of ids) expect(members.some((member) => member.group === id)).toBe(true);
    expect(m.groups.find((g) => g.id === 'branding')).toBeDefined();
    expect(m.slots.map((s) => s.name)).toEqual(['favicon', 'font.mono', 'font.sans', 'logo']);
    expect(new RegExp(m.productName.pattern, 'u').test("Acme & Co.'s Agents")).toBe(true);
    expect(new RegExp(m.productName.pattern, 'u').test('<b>x</b>')).toBe(false);
  });
});

describe('buildTokenDefaults', () => {
  it('reads every settable token as written, with the light overrides laid over the dark values', () => {
    const d = buildTokenDefaults(SMALL_CSS, SMALL_SURFACE);
    expect(d.dark).toEqual({
      '--hades-raw-accent-400': '#8aa7bb',
      '--hades-raw-black': '#0d0f11',
      '--hades-raw-shadow-sm': '0 2px 8px oklch(0% 0 0 / 0.25)',
    });
    expect(d.light).toEqual({
      '--hades-raw-accent-400': '#8aa7bb',
      '--hades-raw-black': '#ece9e2',
      '--hades-raw-shadow-sm': '0 2px 8px oklch(0% 0 0 / 0.25)',
    });
  });

  it('lists the colours first, so the non-colour entries only append', () => {
    const d = buildTokenDefaults(tokensCss, SURFACE);
    for (const mode of MODES) {
      const kinds = Object.keys(d[mode]).map((name) => SURFACE.tokens[name]!.kind === 'color');
      expect(kinds).toEqual([...kinds].sort((x, y) => Number(y) - Number(x)));
    }
  });

  it('writes a default for every token of the manifest in each mode it lists', () => {
    const manifest = buildManifest(tokensCss, SURFACE);
    const d = buildTokenDefaults(tokensCss, SURFACE);
    for (const token of manifest.tokens)
      for (const mode of token.modes)
        expect(d[mode][token.name], `${token.name} ${mode}`).toBeDefined();
  });
});

describe('defaults.json', () => {
  const read = (file: string) =>
    JSON.parse(fs.readFileSync(path.join(packageDir, file), 'utf8')) as unknown;
  const manifest = read('manifest.json') as Manifest;
  const defaults = read('defaults.json') as Record<string, Record<string, string>>;

  // Fails when a token is added to the surface (or a mode to a token) and `make hades-manifest` is not run.
  it('has an entry for every settable token in the manifest, in each mode it lists', () => {
    const missing: string[] = [];
    for (const token of manifest.tokens)
      for (const mode of token.modes)
        if (typeof defaults[mode]?.[token.name] !== 'string')
          missing.push(`${token.name} (${mode})`);
    expect(missing).toEqual([]);
  });

  it('has no entry for a token the manifest does not list', () => {
    const known = new Set(manifest.tokens.map((t) => t.name));
    const extra = MODES.flatMap((mode) =>
      Object.keys(defaults[mode] ?? {}).filter((name) => !known.has(name)),
    );
    expect(extra).toEqual([]);
  });

  it('carries the values of tokens.css as written, and the elevated shadow per mode', () => {
    expect(defaults.dark!['--hades-raw-radius-sm']).toBe('0.375rem');
    expect(defaults.light!['--hades-raw-radius-sm']).toBe('0.375rem');
    expect(defaults.dark!['--hades-raw-font-sans']).toBe('ui-sans-serif, system-ui, sans-serif');
    expect(defaults.dark!['--hades-raw-shadow-elevated']).not.toBe(
      defaults.light!['--hades-raw-shadow-elevated'],
    );
  });
});

describe('serializeManifest', () => {
  it('round-trips with a trailing newline', () => {
    const m = buildManifest(SMALL_CSS, SMALL_SURFACE);
    const text = serializeManifest(m);
    expect(text.endsWith('}\n')).toBe(true);
    expect(JSON.parse(text)).toEqual(m);
  });
});
