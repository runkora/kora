/**
 * The Hades theme module: the validator and loader, the CSS generator and
 * the value formats behind `theme.schema.json`. Exported as
 * `@runkora/hades/theme`, apart from the component entry, because it
 * runs in a Node init container, a Next server module and a browser preview:
 * no React, no DOM, no Node built-ins. See the white-labelling theme ADR.
 */
export { validateTheme, type AssetReader, type ValidateOptions } from './validate';
export {
  generateThemeCss,
  fontPreloads,
  DARK_SELECTOR,
  LIGHT_SELECTOR,
  type CssOptions,
} from './css';
export { familyNameForSlot, familyTokenForSlot } from './manifest';
export type { ThemeManifest, ThemeManifestSlot, ThemeManifestToken } from './manifest';
export {
  BUNDLE_PATH_PATTERN,
  KORA_VERSION_PATTERN,
  FORMAT_PATTERNS,
  isBundlePath,
  isKoraVersion,
  koraMajor,
  matchesFormat,
} from './formats';
export { svgFaults, isWoff2 } from './assets';
export { themeHash } from './hash';
export type {
  IgnoredEntry,
  Theme,
  ThemeError,
  ThemeReport,
  ThemeSlot,
  ThemeTokenValues,
  ValidationResult,
} from './types';
