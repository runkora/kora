/**
 * The CSS generator: turns a validated theme into the one string the root
 * layout inlines in the document head. A dark block under
 * `html:root:not([data-theme='light'])`, a light block under
 * `html[data-theme='light']`, each carrying only what the theme sets, and an
 * `@font-face` per theme font with the family token pointed at it. Both
 * selectors out-rank the token sheet's `:root` and `[data-theme='light']`, so
 * stylesheet order does not matter. See the white-labelling theme ADR,
 * Decision 4.
 */
import type { Mode } from '../tokens/surface';
import { familyNameForSlot } from './manifest';
import type { Theme } from './types';

export interface CssOptions {
  /** Maps a bundle-relative asset path to the URL the page serves it from. */
  readonly resolveAsset: (path: string) => string;
}

export const DARK_SELECTOR = "html:root:not([data-theme='light'])";
export const LIGHT_SELECTOR = "html[data-theme='light']";

/** The generic stack appended after a theme font's family, by slot. */
const FALLBACK_STACK: Readonly<Record<string, string>> = {
  'font.sans': 'ui-sans-serif, system-ui, sans-serif',
  'font.mono': "ui-monospace, 'Cascadia Code', monospace",
};

/** The family token a font slot sets, with the stack it is set to. */
function fontDeclarations(theme: Theme): [token: string, value: string][] {
  return fontSlots(theme).map(([name]) => [
    `--hades-raw-font-${name.slice('font.'.length)}`,
    `"${familyNameForSlot(name)}", ${FALLBACK_STACK[name] ?? 'sans-serif'}`,
  ]);
}

function fontSlots(theme: Theme): [name: string, path: string][] {
  return Object.entries(theme.slots)
    .flatMap(([name, slot]): [string, string][] =>
      slot.kind === 'font' ? [[name, slot.path]] : [],
    )
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
      return value === undefined ? [] : [[name, value]];
    });
}

/**
 * Escapes a value for use inside a double-quoted CSS string. `<` is escaped
 * because the CSS lands in an inline `<style>`, which the HTML parser closes at
 * `</style>` whatever the CSS quoting says.
 */
function cssString(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/</g, '\\3c ')
    .replace(/[\n\r\f]/g, '\\a ');
}

export function generateThemeCss(theme: Theme, options: CssOptions): string {
  const fonts = fontDeclarations(theme);
  const faces = fontSlots(theme).map(
    ([name, path]) =>
      `@font-face{font-family:"${familyNameForSlot(name)}";src:url("${cssString(options.resolveAsset(path))}") format("woff2");font-display:swap}`,
  );
  const dark = block(DARK_SELECTOR, [...modeDeclarations(theme, 'dark'), ...fonts]);
  const light = block(LIGHT_SELECTOR, [...modeDeclarations(theme, 'light'), ...fonts]);
  return [...faces, dark, light].filter(Boolean).join('\n');
}

/** The resolved URL of every theme font, for `<link rel="preload" as="font">` in the head. */
export function fontPreloads(theme: Theme, options: CssOptions): string[] {
  return fontSlots(theme).map(([, path]) => options.resolveAsset(path));
}
