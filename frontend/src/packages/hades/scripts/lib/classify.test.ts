import { describe, expect, it } from 'vitest';

import {
  classify,
  floorMet,
  formatSemver,
  isDocumentationOnly,
  parseSemver,
  requiredVersion,
  semanticPatterns,
  slotPattern,
  tokenPattern,
  type ClassifyInput,
} from './classify.ts';
import type { ExportSurface } from './exports.ts';
import type { Manifest } from './manifest.ts';

const MANIFEST: Manifest = {
  version: '1.0.0',
  modes: ['dark', 'light'],
  tokens: [
    {
      name: '--hades-raw-accent-400',
      kind: 'color',
      format: 'hex-or-oklch',
      modes: ['dark', 'light'],
    },
    { name: '--hades-raw-black', kind: 'color', format: 'hex-or-oklch', modes: ['dark', 'light'] },
  ],
  mapping: {
    '--color-accent': { tokens: ['--hades-raw-accent-400'], derived: false },
    '--color-background': { tokens: ['--hades-raw-black'], derived: false },
  },
  slots: [
    { name: 'favicon', kind: 'image', formats: ['svg'], maxBytes: 65536, modes: ['dark', 'light'] },
  ],
  productName: { minLength: 1, maxLength: 40, pattern: '^.+$' },
};

const EXPORTS: ExportSurface = {
  exports: ['Button', 'ButtonProps', 'cn'],
  props: { ButtonProps: ['asChild', 'dim'] },
};

function input(overrides: Partial<ClassifyInput>): ClassifyInput {
  return {
    baseManifest: MANIFEST,
    headManifest: MANIFEST,
    baseExports: EXPORTS,
    headExports: EXPORTS,
    basePeerDependencies: { react: '>=19' },
    headPeerDependencies: { react: '>=19' },
    changedFiles: [],
    findReads: () => [],
    ...overrides,
  };
}

function withTokens(
  tokens: Manifest['tokens'],
  mapping: Manifest['mapping'] = MANIFEST.mapping,
): Manifest {
  return { ...MANIFEST, tokens, mapping };
}

describe('classify', () => {
  it('is none when nothing changed', () => {
    expect(classify(input({}))).toEqual({ level: 'none', findings: [] });
  });

  it('is none when only documentation, stories or tests changed', () => {
    const c = classify(
      input({
        changedFiles: [
          'README.md',
          'src/components/button/button.stories.tsx',
          'src/docs/x.mdx',
          'src/utils.test.ts',
        ],
      }),
    );
    expect(c.level).toBe('none');
  });

  it('is patch when sources changed without touching the contract, citing rule 5', () => {
    const c = classify(
      input({ changedFiles: ['src/components/button/button.tsx', 'scripts/manifest.ts'] }),
    );
    expect(c.level).toBe('patch');
    expect(c.findings[0]).toMatchObject({ level: 'patch', rule: 'boundary rule 5' });
  });

  it('is major when a token is removed, citing rule 1', () => {
    const head = withTokens([MANIFEST.tokens[1]!], {
      '--color-background': MANIFEST.mapping['--color-background']!,
    });
    const c = classify(input({ headManifest: head }));
    expect(c.level).toBe('major');
    expect(c.findings).toContainEqual({
      level: 'major',
      rule: 'boundary rule 1',
      subject: '--hades-raw-accent-400',
      detail: 'token removed or renamed',
    });
  });

  it('is major when a token changes kind or format', () => {
    const head = withTokens([
      { ...MANIFEST.tokens[0]!, kind: 'shadow', format: 'css-shadow-list' },
      MANIFEST.tokens[1]!,
    ]);
    const c = classify(input({ headManifest: head }));
    expect(c.level).toBe('major');
    expect(c.findings.filter((f) => f.rule === 'boundary rule 1')).toHaveLength(2);
  });

  it('is major when modes change', () => {
    const c = classify(input({ headManifest: { ...MANIFEST, modes: ['dark'] } }));
    expect(c.findings).toContainEqual(
      expect.objectContaining({ level: 'major', subject: 'modes' }),
    );
  });

  it('is minor when a token is added that only new components read, citing rule 4', () => {
    const head = withTokens([
      ...MANIFEST.tokens,
      {
        name: '--hades-raw-focus',
        kind: 'color',
        format: 'hex-or-oklch',
        modes: ['dark', 'light'],
      },
    ]);
    const c = classify(input({ headManifest: head }));
    expect(c.level).toBe('minor');
    expect(c.findings[0]).toMatchObject({
      level: 'minor',
      rule: 'boundary rule 4',
      subject: '--hades-raw-focus',
    });
  });

  it('is major when an added token is read by a pre-existing component, with the match shown', () => {
    const head = withTokens(
      [
        ...MANIFEST.tokens,
        {
          name: '--hades-raw-focus',
          kind: 'color',
          format: 'hex-or-oklch',
          modes: ['dark', 'light'],
        },
      ],
      { ...MANIFEST.mapping, '--color-focus': { tokens: ['--hades-raw-focus'], derived: false } },
    );
    const findReads = (patterns: readonly RegExp[]) =>
      patterns.some((p) => p.test('className="ring-focus"'))
        ? ['src/components/button/button.tsx:12: ring-focus']
        : [];
    const c = classify(input({ headManifest: head, findReads }));
    expect(c.level).toBe('major');
    expect(c.findings[0]).toMatchObject({
      level: 'major',
      rule: 'boundary rule 4',
      subject: '--hades-raw-focus',
      matches: ['src/components/button/button.tsx:12: ring-focus'],
    });
  });

  it('is major when a semantic name is remapped, citing rule 4', () => {
    const head = withTokens(MANIFEST.tokens, {
      ...MANIFEST.mapping,
      '--color-accent': { tokens: ['--hades-raw-black'], derived: false },
    });
    const c = classify(input({ headManifest: head }));
    expect(c.findings).toContainEqual(
      expect.objectContaining({
        level: 'major',
        rule: 'boundary rule 4',
        subject: '--color-accent',
      }),
    );
  });

  it('treats a new semantic name over existing tokens as patch and a removed one as minor', () => {
    const added = withTokens(MANIFEST.tokens, {
      ...MANIFEST.mapping,
      '--color-ink': { tokens: ['--hades-raw-black'], derived: false },
    });
    expect(classify(input({ headManifest: added })).level).toBe('patch');
    const removed = withTokens(MANIFEST.tokens, {
      '--color-accent': MANIFEST.mapping['--color-accent']!,
    });
    expect(classify(input({ headManifest: removed })).level).toBe('minor');
  });

  it('classifies slot limits in both directions', () => {
    const shrink: Manifest = { ...MANIFEST, slots: [{ ...MANIFEST.slots[0]!, maxBytes: 1024 }] };
    expect(classify(input({ headManifest: shrink })).level).toBe('major');
    const grow: Manifest = {
      ...MANIFEST,
      slots: [{ ...MANIFEST.slots[0]!, maxBytes: 1 << 20, formats: ['svg', 'png'] }],
    };
    const c = classify(input({ headManifest: grow }));
    expect(c.level).toBe('minor');
    expect(c.findings).toHaveLength(2);
  });

  it('is major when a slot is removed and minor when one is added unread', () => {
    expect(classify(input({ headManifest: { ...MANIFEST, slots: [] } })).level).toBe('major');
    const added: Manifest = {
      ...MANIFEST,
      slots: [
        ...MANIFEST.slots,
        {
          name: 'logo.mark',
          kind: 'image',
          formats: ['svg'],
          maxBytes: 1,
          modes: ['dark', 'light'],
        },
      ],
    };
    expect(classify(input({ headManifest: added })).level).toBe('minor');
  });

  it('classifies product-name bounds', () => {
    const tighter: Manifest = {
      ...MANIFEST,
      productName: { ...MANIFEST.productName, maxLength: 20 },
    };
    expect(classify(input({ headManifest: tighter })).level).toBe('major');
    const looser: Manifest = {
      ...MANIFEST,
      productName: { ...MANIFEST.productName, maxLength: 60 },
    };
    expect(classify(input({ headManifest: looser })).level).toBe('minor');
    const pattern: Manifest = {
      ...MANIFEST,
      productName: { ...MANIFEST.productName, pattern: '^[a-z]+$' },
    };
    expect(classify(input({ headManifest: pattern })).level).toBe('major');
  });

  it('is minor when an export or a prop is removed, or a peer range changes', () => {
    const noButton: ExportSurface = { exports: ['cn'], props: {} };
    expect(classify(input({ headExports: noButton })).findings).toContainEqual(
      expect.objectContaining({ level: 'minor', subject: 'Button' }),
    );
    const fewerProps: ExportSurface = { ...EXPORTS, props: { ButtonProps: ['asChild'] } };
    expect(classify(input({ headExports: fewerProps })).findings).toContainEqual(
      expect.objectContaining({
        level: 'minor',
        subject: 'ButtonProps',
        detail: 'prop removed: dim',
      }),
    );
    expect(classify(input({ headPeerDependencies: { react: '>=19.2' } })).level).toBe('minor');
    expect(classify(input({ headPeerDependencies: { react: '>=19', zod: '>=3' } })).level).toBe(
      'minor',
    );
  });

  it('skips manifest and export rules when the baseline has none', () => {
    const head = withTokens([MANIFEST.tokens[1]!]);
    const c = classify(
      input({
        baseManifest: null,
        baseExports: null,
        basePeerDependencies: {},
        headManifest: head,
        changedFiles: ['manifest.json'],
      }),
    );
    expect(c.level).toBe('patch');
  });

  it('orders findings most severe first', () => {
    const head = withTokens(
      [
        MANIFEST.tokens[1]!,
        {
          name: '--hades-raw-new',
          kind: 'color',
          format: 'hex-or-oklch',
          modes: ['dark', 'light'],
        },
      ],
      { '--color-background': MANIFEST.mapping['--color-background']! },
    );
    const c = classify(input({ headManifest: head, changedFiles: ['src/x.ts'] }));
    expect(c.findings.map((f) => f.level)).toEqual(['major', 'minor', 'minor', 'patch']);
  });
});

describe('patterns', () => {
  it('matches direct raw reads', () => {
    expect(tokenPattern('--hades-raw-black').test('var(--hades-raw-black)')).toBe(true);
    expect(tokenPattern('--hades-raw-black').test('var(--hades-raw-black-2)')).toBe(false);
  });

  it('matches semantic utilities with variants and modifiers but not longer names', () => {
    const [, utility] = semanticPatterns('--color-accent');
    expect(utility!.test('className="bg-accent text-sm"')).toBe(true);
    expect(utility!.test('hover:text-accent/80')).toBe(true);
    expect(utility!.test('border-x-accent')).toBe(true);
    expect(utility!.test('bg-accent-subtle')).toBe(false);
    expect(utility!.test('my-accent')).toBe(false);
    expect(semanticPatterns('--shadow-elevated')[1]!.test('shadow-elevated')).toBe(true);
    expect(semanticPatterns('--radius-md')[1]!.test('rounded-tl-md')).toBe(true);
    expect(semanticPatterns('--font-mono')[1]!.test("'font-mono'")).toBe(true);
    expect(semanticPatterns('--color-accent')[0]!.test('var(--color-accent)')).toBe(true);
  });

  it('matches a slot by its quoted name', () => {
    expect(slotPattern('logo.mark').test("slot='logo.mark'")).toBe(true);
    expect(slotPattern('logo.mark').test('logo.marker')).toBe(false);
  });
});

describe('documentation-only paths', () => {
  it('classifies the exempt paths', () => {
    for (const p of [
      'README.md',
      'CONTRACT.md',
      'src/docs/a.mdx',
      '.storybook/main.ts',
      'src/test-setup.ts',
      'src/a/a.stories.tsx',
      'src/a/a.test.tsx',
      'scripts/lib/x.test.ts',
    ]) {
      expect(isDocumentationOnly(p)).toBe(true);
    }
    for (const p of [
      'src/index.ts',
      'manifest.json',
      'package.json',
      'scripts/classify.ts',
      'src/tokens/tokens.css',
    ]) {
      expect(isDocumentationOnly(p)).toBe(false);
    }
  });
});

describe('versions and the floor', () => {
  it('parses and formats semver', () => {
    expect(parseSemver('1.2.3')).toEqual({ major: 1, minor: 2, patch: 3 });
    expect(parseSemver('v1.2.3-rc.1')).toEqual({ major: 1, minor: 2, patch: 3 });
    expect(parseSemver('1.2')).toBeNull();
    expect(formatSemver({ major: 1, minor: 0, patch: 0 })).toBe('1.0.0');
  });

  it('computes the required version per level', () => {
    const base = { major: 1, minor: 2, patch: 3 };
    expect(requiredVersion('major', base)).toEqual({ major: 2, minor: 0, patch: 0 });
    expect(requiredVersion('minor', base)).toEqual({ major: 1, minor: 3, patch: 0 });
    expect(requiredVersion('patch', base)).toEqual({ major: 1, minor: 2, patch: 4 });
    expect(requiredVersion('none', base)).toEqual(base);
  });

  it('accepts a bump above the floor and rejects one below it', () => {
    const base = { major: 1, minor: 2, patch: 3 };
    expect(floorMet('patch', base, { major: 1, minor: 2, patch: 4 })).toBe(true);
    expect(floorMet('patch', base, { major: 1, minor: 2, patch: 3 })).toBe(false);
    expect(floorMet('minor', base, { major: 1, minor: 3, patch: 0 })).toBe(true);
    expect(floorMet('minor', base, { major: 2, minor: 0, patch: 0 })).toBe(true);
    expect(floorMet('minor', base, { major: 1, minor: 2, patch: 9 })).toBe(false);
    expect(floorMet('major', base, { major: 2, minor: 0, patch: 0 })).toBe(true);
    expect(floorMet('major', base, { major: 1, minor: 9, patch: 0 })).toBe(false);
    expect(floorMet('none', base, base)).toBe(true);
  });
});
