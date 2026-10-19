import { describe, expect, it } from 'vitest';

import {
  isDirectReference,
  parseBlocks,
  parseDeclarations,
  readTokenSheet,
  varReferences,
} from './css.ts';

const SHEET = `
/* comment with { braces } and ; */
@import 'tailwindcss';

:root {
  color-scheme: dark;
  --hades-radius-unit: 0.375rem;
}

:root {
  --hades-raw-black: #0d0f11; /* trailing */
  --hades-raw-border-subtle: rgb(236 233 226 / 10%);
  --hades-raw-shadow-elevated: 0 8px 28px oklch(0% 0 0 / 0.3), 0 2px 8px oklch(0% 0 0 / 0.15);
}

[data-theme='light'] {
  color-scheme: light;
  --hades-raw-black: #ece9e2;
}

@theme inline {
  --color-background: var(--hades-raw-black);
  --color-accent-subtle: color-mix(in oklch, var(--color-accent) 10%, transparent);
  --font-sans: var(--font-inter, 'Inter', ui-sans-serif, system-ui, sans-serif);
  --radius-full: 9999px;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
`;

describe('parseBlocks', () => {
  it('returns top-level blocks with their selectors and skips nested bodies', () => {
    const blocks = parseBlocks(SHEET);
    expect(blocks.map((b) => b.selector)).toEqual([
      ':root',
      ':root',
      "[data-theme='light']",
      '@theme inline',
      '@keyframes spin',
    ]);
    expect(blocks[4]!.declarations).toEqual([]);
  });
});

describe('parseDeclarations', () => {
  it('splits on semicolons outside parentheses and keeps custom properties only', () => {
    const decls = parseDeclarations(
      "color-scheme: dark; --a: rgb(1 2 3 / 10%); --b: 0 8px oklch(0% 0 0 / 0.3), 0 2px red; --c: 'x;y'",
    );
    expect(decls).toEqual([
      { name: '--a', value: 'rgb(1 2 3 / 10%)' },
      { name: '--b', value: '0 8px oklch(0% 0 0 / 0.3), 0 2px red' },
      { name: '--c', value: "'x;y'" },
    ]);
  });
});

describe('readTokenSheet', () => {
  it('collects raw tokens from every :root block and the semantic mapping', () => {
    const sheet = readTokenSheet(SHEET);
    expect(sheet.rawTokens).toEqual([
      '--hades-raw-black',
      '--hades-raw-border-subtle',
      '--hades-raw-shadow-elevated',
    ]);
    expect(sheet.semantic.map((d) => d.name)).toEqual([
      '--color-background',
      '--color-accent-subtle',
      '--font-sans',
      '--radius-full',
    ]);
  });

  it('does not treat the radius seed as a raw token', () => {
    expect(readTokenSheet(SHEET).rawTokens).not.toContain('--hades-radius-unit');
  });
});

describe('varReferences and isDirectReference', () => {
  it('lists every var() reference in order', () => {
    expect(varReferences('calc(var(--a) + var( --b ))')).toEqual(['--a', '--b']);
    expect(varReferences('9999px')).toEqual([]);
  });

  it('recognises a lone var() without a fallback as direct', () => {
    expect(isDirectReference('var(--hades-raw-black)')).toBe(true);
    expect(isDirectReference("var(--font-inter, 'Inter')")).toBe(false);
    expect(isDirectReference('color-mix(in oklch, var(--a) 10%, transparent)')).toBe(false);
  });
});
