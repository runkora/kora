/**
 * Turns a validated theme into the CSS the root layout inlines: dark and light blocks, plus an `@font-face` per font.
 * Both selectors out-rank the token sheet, so stylesheet order does not matter.
 */
import type { Mode } from '../tokens/surface';
import { fontFamilyList } from './formats';
import { familyNameForSlot } from './manifest';
import type { Theme } from './types';

export const DARK_SELECTOR = "html:root:not([data-theme='light'])";
export const LIGHT_SELECTOR = "html[data-theme='light']";

/** Family tokens start with this prefix: slot `font.<name>` sets `--hades-raw-font-<name>`. */
const FONT_TOKEN_PREFIX = '--hades-raw-font-';

/** The generic stack appended after a theme font's family, by slot. */
const FALLBACK_STACK: Readonly<Record<string, string>> = {
  'font.sans': 'ui-sans-serif, system-ui, sans-serif',
  'font.mono': "ui-monospace, 'Cascadia Code', monospace",
};

/** The family token a font slot sets, with the stack it is set to. */
function fontDeclarations(theme: Theme): [token: string, value: string][] {
  return fontSlots(theme).map(([name]) => [
    `${FONT_TOKEN_PREFIX}${name.slice('font.'.length)}`,
    `"${familyNameForSlot(name)}", ${FALLBACK_STACK[name] ?? 'sans-serif'}`,
  ]);
}

function fontSlots(theme: Theme): [name: string, url: string][] {
  return Object.entries(theme.slots)
    .flatMap(([name, slot]): [string, string][] => (slot.kind === 'font' ? [[name, slot.url]] : []))
    .sort(([a], [b]) => a.localeCompare(b));
}

function block(selector: string, declarations: readonly [string, string][]): string {
  if (declarations.length === 0) return '';
  return `${selector}{${declarations.map(([name, value]) => `${name}:${value}`).join(';')}}`;
}

function modeDeclarations(theme: Theme, mode: Mode): [string, string][] {
  return Object.entries(theme.tokens)
    .sort(([a], [b]) => a.localeCompare(b))
    .flatMap(([name, values]): [string, string][] => {
      const value = values[mode];
      if (value === undefined) return [];
      return [
        [name, name.startsWith(FONT_TOKEN_PREFIX) ? fontFamilyList(value, cssString) : value],
      ];
    });
}

/** Escapes a value for a double-quoted CSS string, and `<` so an inlined `</style>` cannot end the element. */
function cssString(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/</g, '\\3c ')
    .replace(/[\n\r\f]/g, '\\a ');
}

export function generateThemeCss(theme: Theme): string {
  const fonts = fontDeclarations(theme);
  const faces = fontSlots(theme).map(
    ([name, url]) =>
      `@font-face{font-family:"${familyNameForSlot(name)}";src:url("${cssString(url)}") format("woff2");font-display:swap}`,
  );
  const dark = block(DARK_SELECTOR, [...modeDeclarations(theme, 'dark'), ...fonts]);
  const light = block(LIGHT_SELECTOR, [...modeDeclarations(theme, 'light'), ...fonts]);
  return [...faces, dark, light].filter(Boolean).join('\n');
}

/** The URL of every theme font, for `<link rel="preload" as="font" crossorigin>` in the head. */
export function fontPreloads(theme: Theme): string[] {
  return fontSlots(theme).map(([, url]) => url);
}
