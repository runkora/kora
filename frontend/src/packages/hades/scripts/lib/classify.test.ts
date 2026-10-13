import { describe, expect, it } from 'vitest';

import {
  classify,
  contractFloorMet,
  floorMet,
  formatContractVersion,
  formatSemver,
  isDocumentationOnly,
  parseContractVersion,
  parseSemver,
  requiredContractVersion,
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
  contractVersion: '1.0',
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
  required: { ButtonProps: [] },
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

const token = (name: string) => ({
  name,
  kind: 'color' as const,
  format: 'hex-or-oklch' as const,
  modes: ['dark', 'light'] as ['dark', 'light'],
});

/** The two levels a finding moved, for table-driven assertions. */
function levels(c: ReturnType<typeof classify>): { contract: string; package: string } {
  return { contract: c.contract, package: c.package };
}

describe('classify', () => {
  it('is none on both axes when nothing changed', () => {
    expect(classify(input({}))).toEqual({ package: 'none', contract: 'none', findings: [] });
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
    expect(levels(c)).toEqual({ contract: 'none', package: 'none' });
  });

  it('is a package patch and no contract change when sources changed, citing rule 5', () => {
    const c = classify(
      input({ changedFiles: ['src/components/button/button.tsx', 'scripts/manifest.ts'] }),
    );
    expect(levels(c)).toEqual({ contract: 'none', package: 'patch' });
    expect(c.findings[0]).toMatchObject({ package: 'patch', rule: 'boundary rule 5' });
  });

  describe('the contract rows, which move both axes together', () => {
    it('is major on both when a token is removed, citing rule 1', () => {
      const head = withTokens([MANIFEST.tokens[1]!], {
        '--color-background': MANIFEST.mapping['--color-background']!,
      });
      const c = classify(input({ headManifest: head }));
      expect(levels(c)).toEqual({ contract: 'major', package: 'major' });
      expect(c.findings).toContainEqual({
        contract: 'major',
        package: 'major',
        rule: 'boundary rule 1',
        subject: '--hades-raw-accent-400',
        detail: 'token removed or renamed',
      });
    });

    it('is major on both when a token changes kind or format', () => {
      const head = withTokens([
        { ...MANIFEST.tokens[0]!, kind: 'shadow', format: 'css-shadow-list' },
        MANIFEST.tokens[1]!,
      ]);
      const c = classify(input({ headManifest: head }));
      expect(levels(c)).toEqual({ contract: 'major', package: 'major' });
      expect(c.findings.filter((f) => f.rule === 'boundary rule 1')).toHaveLength(2);
    });

    it('is major on both when modes change', () => {
      const c = classify(input({ headManifest: { ...MANIFEST, modes: ['dark'] } }));
      expect(c.findings).toContainEqual(
        expect.objectContaining({ contract: 'major', package: 'major', subject: 'modes' }),
      );
    });

    it('is minor on both when a token is added that only new components read, citing rule 4', () => {
      const head = withTokens([...MANIFEST.tokens, token('--hades-raw-focus')]);
      const c = classify(input({ headManifest: head }));
      expect(levels(c)).toEqual({ contract: 'minor', package: 'minor' });
      expect(c.findings[0]).toMatchObject({
        contract: 'minor',
        package: 'minor',
        rule: 'boundary rule 4',
        subject: '--hades-raw-focus',
      });
    });

    it('is major on both when an added token is read by a pre-existing component, with the match shown', () => {
      const head = withTokens([...MANIFEST.tokens, token('--hades-raw-focus')], {
        ...MANIFEST.mapping,
        '--color-focus': { tokens: ['--hades-raw-focus'], derived: false },
      });
      const findReads = (patterns: readonly RegExp[]) =>
        patterns.some((p) => p.test('className="ring-focus"'))
          ? ['src/components/button/button.tsx:12: ring-focus']
          : [];
      const c = classify(input({ headManifest: head, findReads }));
      expect(levels(c)).toEqual({ contract: 'major', package: 'major' });
      expect(c.findings[0]).toMatchObject({
        contract: 'major',
        package: 'major',
        rule: 'boundary rule 4',
        subject: '--hades-raw-focus',
        matches: ['src/components/button/button.tsx:12: ring-focus'],
      });
    });

    it('is major on both when a semantic name is remapped, citing rule 4', () => {
      const head = withTokens(MANIFEST.tokens, {
        ...MANIFEST.mapping,
        '--color-accent': { tokens: ['--hades-raw-black'], derived: false },
      });
      const c = classify(input({ headManifest: head }));
      expect(c.findings).toContainEqual(
        expect.objectContaining({
          contract: 'major',
          package: 'major',
          rule: 'boundary rule 4',
          subject: '--color-accent',
        }),
      );
    });

    it('classifies slot limits in both directions', () => {
      const shrink: Manifest = { ...MANIFEST, slots: [{ ...MANIFEST.slots[0]!, maxBytes: 1024 }] };
      expect(levels(classify(input({ headManifest: shrink })))).toEqual({
        contract: 'major',
        package: 'major',
      });
      const grow: Manifest = {
        ...MANIFEST,
        slots: [{ ...MANIFEST.slots[0]!, maxBytes: 1 << 20, formats: ['svg', 'png'] }],
      };
      const c = classify(input({ headManifest: grow }));
      expect(levels(c)).toEqual({ contract: 'minor', package: 'minor' });
      expect(c.findings).toHaveLength(2);
    });

    it('is major when a slot is removed and minor when one is added unread', () => {
      expect(levels(classify(input({ headManifest: { ...MANIFEST, slots: [] } })))).toEqual({
        contract: 'major',
        package: 'major',
      });
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
      expect(levels(classify(input({ headManifest: added })))).toEqual({
        contract: 'minor',
        package: 'minor',
      });
    });

    it('classifies product-name bounds', () => {
      const tighter: Manifest = {
        ...MANIFEST,
        productName: { ...MANIFEST.productName, maxLength: 20 },
      };
      expect(classify(input({ headManifest: tighter })).contract).toBe('major');
      const looser: Manifest = {
        ...MANIFEST,
        productName: { ...MANIFEST.productName, maxLength: 60 },
      };
      expect(levels(classify(input({ headManifest: looser })))).toEqual({
        contract: 'minor',
        package: 'minor',
      });
      const pattern: Manifest = {
        ...MANIFEST,
        productName: { ...MANIFEST.productName, pattern: '^[a-z]+$' },
      };
      expect(classify(input({ headManifest: pattern })).contract).toBe('major');
    });
  });

  describe('the package rows, which leave the contract alone', () => {
    it('is a package major with no contract change when a semantic name is removed', () => {
      const removed = withTokens(MANIFEST.tokens, {
        '--color-accent': MANIFEST.mapping['--color-accent']!,
      });
      const c = classify(input({ headManifest: removed }));
      expect(levels(c)).toEqual({ contract: 'none', package: 'major' });
      expect(c.findings[0]).toMatchObject({
        rule: 'Semantic Versioning',
        detail: 'semantic name removed',
      });
    });

    it('is a package minor with no contract change when a semantic name is added over existing tokens', () => {
      const added = withTokens(MANIFEST.tokens, {
        ...MANIFEST.mapping,
        '--color-ink': { tokens: ['--hades-raw-black'], derived: false },
      });
      expect(levels(classify(input({ headManifest: added })))).toEqual({
        contract: 'none',
        package: 'minor',
      });
    });

    it('is a package major when an export is removed or renamed', () => {
      const noButton: ExportSurface = { exports: ['cn'], props: {}, required: {} };
      const c = classify(input({ headExports: noButton }));
      expect(levels(c)).toEqual({ contract: 'none', package: 'major' });
      expect(c.findings).toContainEqual(
        expect.objectContaining({
          package: 'major',
          subject: 'Button',
          detail: 'export removed or renamed',
        }),
      );
    });

    it('is a package major when a prop is removed', () => {
      const fewer: ExportSurface = {
        ...EXPORTS,
        props: { ButtonProps: ['asChild'] },
      };
      const c = classify(input({ headExports: fewer }));
      expect(levels(c)).toEqual({ contract: 'none', package: 'major' });
      expect(c.findings).toContainEqual(
        expect.objectContaining({ subject: 'ButtonProps', detail: 'prop removed: dim' }),
      );
    });

    it('is a package major when a required prop is added or an optional prop becomes required', () => {
      const addedRequired: ExportSurface = {
        ...EXPORTS,
        props: { ButtonProps: ['asChild', 'dim', 'label'] },
        required: { ButtonProps: ['label'] },
      };
      const a = classify(input({ headExports: addedRequired }));
      expect(levels(a)).toEqual({ contract: 'none', package: 'major' });
      expect(a.findings[0]).toMatchObject({ detail: 'required prop added: label' });

      const nowRequired: ExportSurface = { ...EXPORTS, required: { ButtonProps: ['dim'] } };
      const b = classify(input({ headExports: nowRequired }));
      expect(levels(b)).toEqual({ contract: 'none', package: 'major' });
      expect(b.findings[0]).toMatchObject({ detail: 'prop made required: dim' });
    });

    it('is a package minor when an export or an optional prop is added', () => {
      const addedExport: ExportSurface = {
        exports: ['Button', 'ButtonProps', 'Card', 'cn'],
        props: EXPORTS.props,
        required: EXPORTS.required,
      };
      const a = classify(input({ headExports: addedExport }));
      expect(levels(a)).toEqual({ contract: 'none', package: 'minor' });
      expect(a.findings[0]).toMatchObject({ subject: 'Card', detail: 'export added' });

      const addedOptional: ExportSurface = {
        ...EXPORTS,
        props: { ButtonProps: ['asChild', 'dim', 'tone'] },
      };
      const b = classify(input({ headExports: addedOptional }));
      expect(levels(b)).toEqual({ contract: 'none', package: 'minor' });
      expect(b.findings[0]).toMatchObject({ detail: 'optional prop added: tone' });
    });

    it('is a package major when a peer dependency is added or its range narrowed', () => {
      expect(levels(classify(input({ headPeerDependencies: { react: '>=19.2' } })))).toEqual({
        contract: 'none',
        package: 'major',
      });
      expect(
        levels(classify(input({ headPeerDependencies: { react: '>=19', zod: '>=3' } }))),
      ).toEqual({ contract: 'none', package: 'major' });
    });

    it('is a package minor when a peer dependency is removed', () => {
      const c = classify(input({ headPeerDependencies: {} }));
      expect(levels(c)).toEqual({ contract: 'none', package: 'minor' });
      expect(c.findings[0]).toMatchObject({
        subject: 'react',
        detail: 'peer dependency removed (was >=19)',
      });
    });

    it('is a package minor when a peer dependency range is widened', () => {
      const c = classify(
        input({
          basePeerDependencies: { react: '>=19.2' },
          headPeerDependencies: { react: '>=19' },
        }),
      );
      expect(levels(c)).toEqual({ contract: 'none', package: 'minor' });
    });
  });

  it('never lets the package level fall below the contract level', () => {
    const head = withTokens([MANIFEST.tokens[1]!], {
      '--color-background': MANIFEST.mapping['--color-background']!,
    });
    const c = classify(input({ headManifest: head }));
    for (const f of c.findings) {
      const order = ['none', 'patch', 'minor', 'major'];
      expect(order.indexOf(f.package)).toBeGreaterThanOrEqual(order.indexOf(f.contract));
    }
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
    expect(levels(c)).toEqual({ contract: 'none', package: 'patch' });
  });

  it('orders findings most severe first', () => {
    const head = withTokens([MANIFEST.tokens[1]!, token('--hades-raw-new')], {
      '--color-background': MANIFEST.mapping['--color-background']!,
    });
    const c = classify(input({ headManifest: head, changedFiles: ['src/x.ts'] }));
    expect(c.findings.map((f) => f.package)).toEqual(['major', 'major', 'minor', 'patch']);
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

  it('parses and formats the contract version, which has no patch', () => {
    expect(parseContractVersion('1.2')).toEqual({ major: 1, minor: 2 });
    expect(parseContractVersion('1.2.3')).toBeNull();
    expect(parseContractVersion('1')).toBeNull();
    expect(formatContractVersion({ major: 1, minor: 0 })).toBe('1.0');
  });

  it('computes the required contract version, a major resetting the minor', () => {
    const base = { major: 1, minor: 4 };
    expect(requiredContractVersion('major', base)).toEqual({ major: 2, minor: 0 });
    expect(requiredContractVersion('minor', base)).toEqual({ major: 1, minor: 5 });
    expect(requiredContractVersion('none', base)).toEqual(base);
  });

  it('holds the contract floor independently of the package floor', () => {
    const base = { major: 1, minor: 4 };
    expect(contractFloorMet('major', base, { major: 2, minor: 0 })).toBe(true);
    expect(contractFloorMet('major', base, { major: 1, minor: 9 })).toBe(false);
    expect(contractFloorMet('minor', base, { major: 1, minor: 5 })).toBe(true);
    expect(contractFloorMet('minor', base, { major: 2, minor: 0 })).toBe(true);
    expect(contractFloorMet('minor', base, { major: 1, minor: 4 })).toBe(false);
    expect(contractFloorMet('none', base, base)).toBe(true);
    expect(contractFloorMet('none', base, { major: 1, minor: 3 })).toBe(false);
  });
});
