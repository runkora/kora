// @vitest-environment node
import { describe, expect, it } from 'vitest';

import { hasAssetExtension, isAssetUrl, isKoraVersion, koraMajor, matchesFormat } from './index';
import { fontFamilyFault, fontFamilyList } from './formats';

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
    'Source Sans 3',
    '3270',
    'Cafe\u0301',
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

describe('kora versions and asset URLs', () => {
  it('recognises MAJOR.MINOR.PATCH and nothing else', () => {
    for (const v of ['1.0.0', '0.0.0', '1.10.2', '10.20.30']) expect(isKoraVersion(v)).toBe(true);
    for (const v of [
      '1',
      '1.0',
      '01.0.0',
      '1.00.0',
      'v1.0.0',
      '1.0.0 ',
      '1.0.0.0',
      '1.0.0-rc.1',
      '',
    ])
      expect(isKoraVersion(v)).toBe(false);
  });

  it('reads the major of an instance version, tolerating a v and a pre-release suffix', () => {
    expect(koraMajor('1.4.0')).toBe(1);
    expect(koraMajor('v2.0.1')).toBe(2);
    expect(koraMajor('0.8.0-rc.2')).toBe(0);
    expect(koraMajor('0.0.0-dev')).toBe(0);
    for (const v of ['1.4', 'dev', '', 'vX.Y.Z']) expect(koraMajor(v)).toBeNull();
  });

  it('accepts https URLs and rejects every other form', () => {
    for (const u of [
      'https://cdn.example/logo.svg',
      'https://a/b',
      'https://cdn.example:443/a/b.svg?v=1&w=2',
      'https://xn--caf-dma.example/a.svg',
    ])
      expect(isAssetUrl(u), u).toBe(true);
    for (const u of [
      'http://cdn.example/a.svg',
      'HTTPS://cdn.example/a.svg',
      'https://',
      'https:///a.svg',
      'https://user@cdn.example/a.svg',
      'https://cdn.example/a.svg#x',
      'https://cdn.example/a b.svg',
      "https://cdn.example/a'b.svg",
      'https://cdn.example/<a>.svg',
      'https://cdn.example/\u0000.svg',
      'https://[::1]/a.svg',
      '',
    ])
      expect(isAssetUrl(u), u).toBe(false);
  });

  it('reads the extension from the path, whatever its case, and never from the query', () => {
    expect(hasAssetExtension('https://cdn.example/a/logo.SVG?v=1', ['svg'])).toBe(true);
    expect(hasAssetExtension('https://cdn.example/f.woff2', ['woff2'])).toBe(true);
    expect(hasAssetExtension('https://cdn.example/f.woff2', ['svg'])).toBe(false);
    expect(hasAssetExtension('https://cdn.example/a?f=/b.svg', ['svg'])).toBe(false);
    expect(hasAssetExtension('https://cdn.example/.svg', ['svg'])).toBe(false);
  });
});

describe('font-family-name values', () => {
  it.each([
    ',',
    ',Inter',
    'Inter,',
    'Inter,,Mono',
    'Inter, ,Mono',
    ' , ',
    'inherit',
    'Inherit',
    'initial',
    'unset',
    'revert',
    'revert-layer',
    'default',
    'Inter, inherit',
  ])('refuses %j at validation time', (v) => {
    expect(matchesFormat('font-family-name', v) && fontFamilyFault(v) === null).toBe(false);
  });
  it.each(['Inter', 'Source Sans 3', 'Inter, sans-serif', '3270'])('allows %j', (v) =>
    expect(fontFamilyFault(v)).toBeNull(),
  );
  it('quotes names and leaves generic keywords bare', () => {
    const quote = (n: string) => n;
    expect(fontFamilyList('Source Sans 3', quote)).toBe('"Source Sans 3"');
    expect(fontFamilyList('3270', quote)).toBe('"3270"');
    expect(fontFamilyList('Inter,sans-serif', quote)).toBe('"Inter", sans-serif');
    expect(fontFamilyList(' Cafe\u0301 , System-UI , monospace', quote)).toBe(
      '"Cafe\u0301", system-ui, monospace',
    );
  });
});
