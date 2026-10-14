// @vitest-environment node
import { describe, expect, it } from 'vitest';

import { isBundlePath, isContractVersion, matchesFormat, svgFaults } from './index';
import { NOT_SVG, SVG, SVG_WITH_BOM } from './fixtures.test-support';

describe('hex-or-oklch', () => {
  it.each([
    '#abc',
    '#abcd',
    '#0D0F11',
    '#0d0f11ff',
    'oklch(50% 0.1 200)',
    'oklch(0.5 0.1 200 / 0.5)',
    'oklch(none 0 0)',
    'oklch( 1 .2 30 / 50% )',
  ])('accepts %s', (v) => expect(matchesFormat('hex-or-oklch', v)).toBe(true));
  it.each([
    'red',
    '#12345',
    '#ggg',
    'rgb(0 0 0)',
    'oklch(50% 0.1)',
    'oklch(50% 0.1 200 / )',
    ' #abc',
    'var(--x)',
    'oklch(50% 0.1 200deg)',
    'oklch(1 .2 3e1)',
  ])('rejects %s', (v) => expect(matchesFormat('hex-or-oklch', v)).toBe(false));
});

describe('css-shadow-list', () => {
  it.each([
    '0 1px 2px #000',
    '0 2px 8px oklch(0% 0 0 / 0.25)',
    'inset 0 1px 0 rgba(255, 255, 255, 0.1)',
    '#000 0 1px 2px',
    '0 0 0 1px #0000, 0 1px 2px #0003, 0 2px 4px #0003, 0 4px 8px #0003',
    '-1px -2.5px 0.5rem 1em oklch(0 0 0)',
    '0 0',
  ])('accepts %s', (v) => expect(matchesFormat('css-shadow-list', v)).toBe(true));
  it.each([
    '0',
    'none',
    '0 1px 2px red',
    '0 1px 2px 3px 4px #000',
    '0 1px #000, 0 1px #000, 0 1px #000, 0 1px #000, 0 1px #000',
    '0 1px 2px url(x)',
    '0 1px 2px #000;',
    '0 1px 2px #000 }',
    '0 1px 2px var(--x)',
    '0 1px 2px #000 <',
    '0 1% 2px #000',
  ])('rejects %s', (v) => expect(matchesFormat('css-shadow-list', v)).toBe(false));
});

describe('css-length', () => {
  it.each(['0', '6px', '0.375rem', '.5em', '100%', '12.5px'])('accepts %s', (v) =>
    expect(matchesFormat('css-length', v)).toBe(true),
  );
  it.each(['6', '-6px', '6 px', '6vw', 'auto', '1px 2px', 'calc(1px)'])('rejects %s', (v) =>
    expect(matchesFormat('css-length', v)).toBe(false),
  );
});

describe('font-family-name', () => {
  it.each([
    'Inter',
    'Inter Variable',
    'Inter, sans-serif',
    'Cascadia Code, monospace',
    'Noto Sans 日本語',
    'IBM_Plex-Mono.v2',
  ])('accepts %s', (v) => expect(matchesFormat('font-family-name', v)).toBe(true));
  it.each([
    '',
    'a'.repeat(65),
    'Inter;',
    'Inter {',
    'url(x)',
    'a<b',
    'Inter\n',
    'Inter\\',
    'Inter(1)',
    'Inter"',
    "'Inter",
    '"Inter", sans-serif',
  ])('rejects %j', (v) => expect(matchesFormat('font-family-name', v)).toBe(false));
});

describe('contract versions and bundle paths', () => {
  it('recognises MAJOR.MINOR and nothing else', () => {
    for (const v of ['1.0', '0.0', '1.10', '10.20']) expect(isContractVersion(v)).toBe(true);
    for (const v of ['1', '1.0.0', '01.0', '1.00', 'v1.0', '1.0 ', '1.0.0.0', ''])
      expect(isContractVersion(v)).toBe(false);
  });

  it('accepts bundle-relative paths and rejects escapes', () => {
    for (const p of [
      'assets/logo.svg',
      'logo.svg',
      'assets/brand/2026/logo.dark.svg',
      'assets/...x/y',
    ])
      expect(isBundlePath(p)).toBe(true);
    for (const p of [
      '/assets/logo.svg',
      '../logo.svg',
      'assets/../logo.svg',
      'assets/..',
      'assets/./logo.svg',
      '.',
      'assets//logo.svg',
      'assets/',
      'assets\\logo.svg',
      '',
    ])
      expect(isBundlePath(p), p).toBe(false);
  });
});

describe('svgFaults', () => {
  it('accepts a prolog, comments, whitespace and a BOM before <svg', () => {
    expect(svgFaults(SVG)).toEqual([]);
    expect(svgFaults(SVG_WITH_BOM)).toEqual([]);
    expect(svgFaults(new TextEncoder().encode('\n\n<!-- a --><!-- b -->\n<svg/>'))).toEqual([]);
  });

  it('names every fault it finds', () => {
    expect(svgFaults(NOT_SVG)).toEqual(['does not begin with <svg']);
    const faults = svgFaults(
      new TextEncoder().encode(
        '<svg onclick="x"><script/><a href="http://x"/><style>a{fill:url(http://y)}</style></svg>',
      ),
    );
    expect(faults).toHaveLength(4);
    expect(svgFaults(new TextEncoder().encode('<!-- open'))).toEqual(['unterminated comment']);
  });

  it('catches an event attribute after a slash and an @import with no url()', () => {
    const encode = (text: string) => new TextEncoder().encode(text);
    expect(svgFaults(encode('<svg/onload=alert(1)/>'))).toEqual([
      'contains an event attribute (on*=)',
    ]);
    expect(
      svgFaults(encode('<svg><style>@import "https://x.example/a.css";</style></svg>')),
    ).toEqual(['references an external stylesheet through @import']);
  });

  it('does not backtrack on long runs of whitespace', () => {
    const spaces = ' '.repeat(100_000);
    const started = performance.now();
    svgFaults(new TextEncoder().encode(`<svg><a href=${spaces}</svg>`));
    svgFaults(new TextEncoder().encode(`<svg><a style="fill:url(${spaces}"/></svg>`));
    expect(performance.now() - started).toBeLessThan(500);
  });
});
