/**
 * Shared fixtures for the theme module's tests: a small manifest, acceptable
 * and faulty assets, and a bundle reader over an in-memory map.
 */
import type { AssetReader } from './validate';
import type { ThemeManifest } from './manifest';

export const MANIFEST: ThemeManifest = {
  version: '1.4.2',
  modes: ['dark', 'light'],
  tokens: [
    { name: '--hades-raw-black', kind: 'color', format: 'hex-or-oklch', modes: ['dark', 'light'] },
    {
      name: '--hades-raw-accent-400',
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
    {
      name: '--hades-raw-radius-md',
      kind: 'length',
      format: 'css-length',
      modes: ['dark', 'light'],
    },
    {
      name: '--hades-raw-font-sans',
      kind: 'font',
      format: 'font-family-name',
      modes: ['dark', 'light'],
    },
    {
      name: '--hades-raw-font-mono',
      kind: 'font',
      format: 'font-family-name',
      modes: ['dark', 'light'],
    },
  ],
  slots: [
    { name: 'favicon', kind: 'image', formats: ['svg'], maxBytes: 1024, modes: ['dark', 'light'] },
    { name: 'font.mono', kind: 'font', formats: ['woff2'], maxBytes: 4096, modes: [] },
    { name: 'font.sans', kind: 'font', formats: ['woff2'], maxBytes: 4096, modes: [] },
    { name: 'logo', kind: 'image', formats: ['svg'], maxBytes: 2048, modes: ['dark', 'light'] },
  ],
  productName: { minLength: 1, maxLength: 40, pattern: "^[\\p{L}\\p{N} .\\-&']+$" },
};

const encoder = new TextEncoder();

export const SVG = encoder.encode(
  '<?xml version="1.0" encoding="UTF-8"?>\n<!-- brand -->\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><use href="#a"/><rect fill="url(#g)" width="10" height="10"/></svg>\n',
);
export const SVG_WITH_BOM = new Uint8Array([
  0xef,
  0xbb,
  0xbf,
  ...encoder.encode('<svg xmlns="http://www.w3.org/2000/svg"/>'),
]);
export const SVG_SCRIPT = encoder.encode(
  '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>',
);
export const SVG_ONLOAD = encoder.encode(
  '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>',
);
export const SVG_EXTERNAL_HREF = encoder.encode(
  '<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="https://evil.example/x.png"/></svg>',
);
export const SVG_EXTERNAL_URL = encoder.encode(
  '<svg xmlns="http://www.w3.org/2000/svg"><style>rect{fill:url(https://evil.example/p.png)}</style></svg>',
);
export const SVG_DATA_IMAGE = encoder.encode(
  '<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AAAA"/></svg>',
);
export const SVG_DOCTYPE = encoder.encode(
  '<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg"/>',
);
export const NOT_SVG = encoder.encode('<html><body>no</body></html>');

export const WOFF2 = new Uint8Array([0x77, 0x4f, 0x46, 0x32, 0, 1, 2, 3, 4, 5, 6, 7]);
export const NOT_WOFF2 = new Uint8Array([0x77, 0x4f, 0x46, 0x46, 0, 1, 2, 3]); // "wOFF", WOFF1

export function bundle(files: Record<string, Uint8Array>): AssetReader {
  return async (path) => files[path] ?? null;
}

export const FULL_BUNDLE = {
  'assets/logo-dark.svg': SVG,
  'assets/logo-light.svg': SVG_WITH_BOM,
  'assets/favicon.svg': SVG,
  'assets/sans.woff2': WOFF2,
  'assets/mono.woff2': WOFF2,
};

export const FULL_DOCUMENT = `
contractVersion: "1.2"
productName: "  Acme & Co.'s Agents "
tokens:
  --hades-raw-black:
    dark: "#0d0f11"
    light: oklch(97% 0.01 90)
  --hades-raw-accent-400:
    dark: oklch(70% 0.12 240 / 0.9)
  --hades-raw-shadow-sm:
    dark: "0 2px 8px oklch(0% 0 0 / 0.25), inset 0 1px 0 #ffffff10"
    light: 0 1px 2px rgba(0, 0, 0, 0.1)
  --hades-raw-radius-md:
    dark: 6px
    light: 0.375rem
slots:
  logo:
    dark: assets/logo-dark.svg
    light: assets/logo-light.svg
  favicon:
    dark: assets/favicon.svg
    light: assets/favicon.svg
  font.sans: assets/sans.woff2
  font.mono: assets/mono.woff2
`;
