import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { MODES } from '../../src/tokens/surface.ts';
import {
  buildManifest,
  parseManifest,
  serializeManifest,
  SURFACE,
  SurfaceMismatchError,
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
  tokenKinds: {
    '--hades-raw-black': 'color',
    '--hades-raw-accent-400': 'color',
    '--hades-raw-shadow-sm': 'shadow',
  },
  slots: [{ name: 'favicon', kind: 'image', formats: ['svg'], maxBytes: 1024, modes: MODES }],
  productName: { minLength: 1, maxLength: 40, pattern: '^.+$' },
};

describe('buildManifest', () => {
  it('lists every raw token with its kind, format and both modes, sorted', () => {
    const m = buildManifest(SMALL_CSS, '1.2.3', SMALL_SURFACE);
    expect(m.version).toBe('1.2.3');
    expect(m.modes).toEqual(['dark', 'light']);
    expect(m.tokens).toEqual([
      {
        name: '--hades-raw-accent-400',
        kind: 'color',
        format: 'hex-or-oklch',
        modes: ['dark', 'light'],
      },
      {
        name: '--hades-raw-black',
        kind: 'color',
        format: 'hex-or-oklch',
        modes: ['dark', 'light'],
      },
      {
        name: '--hades-raw-shadow-sm',
        kind: 'shadow',
        format: 'css-shadow-list',
        modes: ['dark', 'light'],
      },
    ]);
  });

  it('records direct, derived and literal mappings', () => {
    const { mapping } = buildManifest(SMALL_CSS, '1.0.0', SMALL_SURFACE);
    expect(mapping['--color-background']).toEqual({
      tokens: ['--hades-raw-black'],
      derived: false,
    });
    expect(mapping['--color-accent-subtle']).toEqual({
      tokens: ['--hades-raw-accent-400'],
      derived: true,
    });
    expect(mapping['--font-sans']).toEqual({ tokens: [], derived: false });
    expect(mapping['--radius-full']).toEqual({ tokens: [], derived: false });
  });

  it('carries the slots and product-name bounds through', () => {
    const m = buildManifest(SMALL_CSS, '1.0.0', SMALL_SURFACE);
    expect(m.slots).toEqual([
      {
        name: 'favicon',
        kind: 'image',
        formats: ['svg'],
        maxBytes: 1024,
        modes: ['dark', 'light'],
      },
    ]);
    expect(m.productName).toEqual(SMALL_SURFACE.productName);
  });

  it('fails naming a raw token missing from either side', () => {
    const missingKind: SurfaceInput = {
      ...SMALL_SURFACE,
      tokenKinds: { '--hades-raw-black': 'color', '--hades-raw-shadow-sm': 'shadow' },
    };
    expect(() => buildManifest(SMALL_CSS, '1.0.0', missingKind)).toThrow(SurfaceMismatchError);
    expect(() => buildManifest(SMALL_CSS, '1.0.0', missingKind)).toThrow(
      /--hades-raw-accent-400 is in tokens.css/,
    );

    const extraKind: SurfaceInput = {
      ...SMALL_SURFACE,
      tokenKinds: { ...SMALL_SURFACE.tokenKinds, '--hades-raw-ghost': 'color' },
    };
    expect(() => buildManifest(SMALL_CSS, '1.0.0', extraKind)).toThrow(
      /--hades-raw-ghost has a kind in surface.ts/,
    );
  });

  it('builds from the real token sheet and surface declaration', () => {
    const m = buildManifest(tokensCss, '0.0.0', SURFACE);
    expect(m.tokens.length).toBeGreaterThan(30);
    expect(m.tokens.every((t) => t.name.startsWith('--hades-raw-'))).toBe(true);
    expect(m.slots.map((s) => s.name)).toEqual(['favicon', 'font.mono', 'font.sans', 'logo']);
    expect(new RegExp(m.productName.pattern, 'u').test("Acme & Co.'s Agents")).toBe(true);
    expect(new RegExp(m.productName.pattern, 'u').test('<b>x</b>')).toBe(false);
  });
});

describe('serializeManifest and parseManifest', () => {
  it('round-trips with a trailing newline', () => {
    const m = buildManifest(SMALL_CSS, '1.0.0', SMALL_SURFACE);
    const text = serializeManifest(m);
    expect(text.endsWith('}\n')).toBe(true);
    expect(parseManifest(text)).toEqual(m);
  });

  it('rejects a manifest missing a top-level field', () => {
    expect(() => parseManifest('{"version":"1.0.0"}')).toThrow(/missing "modes"/);
  });
});
