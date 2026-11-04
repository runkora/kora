/** Fixtures for the theme module's tests: a small manifest and a full theme document. */
import type { ThemeManifest } from './manifest';

export const MANIFEST: ThemeManifest = {
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
    { name: 'favicon', kind: 'image', formats: ['svg'], modes: ['dark', 'light'] },
    { name: 'font.mono', kind: 'font', formats: ['woff2'], modes: [] },
    { name: 'font.sans', kind: 'font', formats: ['woff2'], modes: [] },
    { name: 'logo', kind: 'image', formats: ['svg'], modes: ['dark', 'light'] },
  ],
  productName: { minLength: 1, maxLength: 40, pattern: "^[\\p{L}\\p{N} .\\-&']+$" },
};

export const FULL_DOCUMENT = `
koraVersion: "1.2.0"
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
    dark: https://cdn.example/logo-dark.svg
    light: https://cdn.example/logo-light.svg
  favicon:
    dark: https://cdn.example/favicon.svg
    light: https://cdn.example/favicon.svg
  font.sans: https://cdn.example/sans.woff2
  font.mono: https://cdn.example/mono.woff2
`;
