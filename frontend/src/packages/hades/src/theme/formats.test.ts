// @vitest-environment node
import { describe, expect, it } from 'vitest';

import { isBundlePath, isKoraVersion, koraMajor, matchesFormat, svgFaults } from './index';
import { fontFamilyFault, fontFamilyList } from './formats';
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

describe('kora versions and bundle paths', () => {
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

describe('svgFaults: attributes versus text', () => {
  const faults = (svg: string) => svgFaults(new TextEncoder().encode(svg));

  it.each([
    '<svg><text>go online = fast</text></svg>',
    '<svg><title>the onion = good</title></svg>',
    '<svg><text>see href=x</text></svg>',
    '<svg><text>a xlink:href=http://x b</text></svg>',
    '<svg><!-- onload=x href=http://x --><rect/></svg>',
    '<svg><![CDATA[ onclick=1 href=http://x ]]></svg>',
    '<svg><text title="a > b">on = off</text></svg>',
    '<svg><a href="#frag"/><image xlink:href="data:image/png;base64,AAAA"/></svg>',
  ])('accepts %s', (svg) => expect(faults(svg)).toEqual([]));

  it.each([
    ['<svg onload="x"/>', 'event attribute'],
    ['<svg\tonload="x"/>', 'event attribute'],
    ['<svg\nonload="x"/>', 'event attribute'],
    ['<svg\fonload="x"/>', 'event attribute'],
    ['<svg\vonload="x"/>', 'event attribute'],
    ['<svg/onload="x"/>', 'event attribute'],
    ['<svg a="1"onload="x"/>', 'event attribute'],
    ["<svg a='1'onclick='x'/>", 'event attribute'],
    ['<svg ONLOAD="x"/>', 'event attribute'],
    ['<svg onload = "x"/>', 'event attribute'],
    ['<svg><a title="a > b" onclick="x"/></svg>', 'event attribute'],
    ['<svg><a href="http://x"/></svg>', 'href'],
    ['<svg><a href="https://x"/></svg>', 'href'],
    ['<svg><a href="//x"/></svg>', 'href'],
    ['<svg><a href="javascript:alert(1)"/></svg>', 'href'],
    ['<svg><a href="data:text/html,x"/></svg>', 'href'],
    ['<svg><a href="&#104;ttp://x"/></svg>', 'href'],
    ['<svg><a href=" http://x"/></svg>', 'href'],
    ['<svg><a href="a.svg"/></svg>', 'href'],
    ['<svg><a HREF="http://x"/></svg>', 'href'],
    ['<svg><a href = "http://x"/></svg>', 'href'],
    ['<svg><image foo:href="http://evil"/></svg>', 'href'],
    ['<svg><image xlink:href="http://evil"/></svg>', 'href'],
    ['<svg><script/></svg>', '<script>'],
    ['<svg:script/>', '<script>'],
    ['<svg><x:script xmlns:x="http://www.w3.org/2000/svg"/></svg>', '<script>'],
    ['<svg><SVG:SCRIPT/></svg>', '<script>'],
    ['<svg><!-- <script> --></svg>', '<script>'],
    ['<svg><foreignObject/></svg>', 'foreignObject'],
    ['<svg><FOREIGNOBJECT/></svg>', 'foreignObject'],
    ['<svg><svg:foreignObject/></svg>', 'foreignObject'],
  ])('rejects %j', (svg, fragment) => {
    const found = faults(svg);
    expect(found.length).toBeGreaterThan(0);
    expect(found.join(';')).toContain(fragment);
  });

  it('keeps fragment and image data hrefs with a prefix', () => {
    expect(faults('<svg><image foo:href="#a"/></svg>')).toEqual([]);
  });
});
