/**
 * The loaded theme: the frontend's side of the white-labelling theme ADR,
 * decision 4.
 *
 * The theme is one document, `theme.yaml` in `/etc/orbit/theme` by default,
 * which the chart mounts from the frontend's ConfigMap. Its slots hold the
 * `https://` URLs of the logo, the favicon and the fonts, which the browser
 * loads directly; the instance serves no asset. The first call reads the
 * document, runs the Hades validator, derives everything a page needs (the
 * inline CSS, the font preloads, the effective brand) and holds the result for
 * the life of the process: the theme changes only on redeploy, so nothing
 * re-reads it. A document that fails validation throws with every fault
 * listed, so no page serves; the init container running
 * `scripts/validate-theme.ts` on the same document is what normally keeps that
 * from happening. An absent directory means the kora default; a directory
 * without `theme.yaml` is an error.
 *
 * `ORBIT_THEME_DIR` overrides the directory. It exists for tests, which point it
 * at the fixtures beside this module.
 */
import 'server-only';

import { stat } from 'node:fs/promises';
import path from 'node:path';
import manifest from '@runkora/hades/manifest.json';
import {
  fontPreloads,
  generateThemeCss,
  validateTheme,
  type Theme,
  type ThemeError,
  type ThemeManifest,
  type ThemeReport,
} from '@runkora/hades/theme';
import { instanceKoraVersion } from '@orbit/lib/kora-version';
import { defaultTheme, type IdentityTheme } from '@orbit/theme/default';
import { ThemeYamlTooLargeError, isMissing, readThemeDocument } from '@orbit/lib/theme-document';

export const DEFAULT_THEME_DIR = '/etc/orbit/theme';

// The JSON import widens the manifest's mode and kind unions to string; the
// file is generated from the same types, so the cast only restores them.
const hadesManifest = manifest as ThemeManifest;

/** What the process holds after loading: the theme, or the default when there is none. */
export interface LoadedTheme {
  /**
   * The kora release this instance is, whose major a theme's `koraVersion` must
   * equal; null for a build that is not from a release tag, where the
   * comparison is skipped.
   */
  readonly koraVersion: string | null;
  /** The validated theme, or null when no theme is mounted. */
  readonly theme: Theme | null;
  /** The validator's report; empty when no theme is mounted. */
  readonly report: ThemeReport;
  /** The CSS the root layout inlines; empty when no theme is mounted. */
  readonly css: string;
  /** Resolved URLs of the theme's fonts, one `<link rel="preload">` each. */
  readonly fontPreloads: readonly string[];
  /** The effective brand: the kora default overlaid by the theme. */
  readonly brand: IdentityTheme;
}

/** Thrown when the document fails validation. Every fault is in `errors` and in the message. */
export class ThemeLoadError extends Error {
  readonly errors: readonly ThemeError[];

  constructor(dir: string, errors: readonly ThemeError[]) {
    const lines = errors.map((e) => `  ${e.path || '(document)'}: ${e.message}`);
    super(`theme at ${dir} is invalid:\n${lines.join('\n')}`);
    this.name = 'ThemeLoadError';
    this.errors = errors;
  }
}

const EMPTY_REPORT: ThemeReport = {
  ignored: [],
  fallThrough: [],
  mixedIdentity: [],
  versionCheckSkipped: false,
};

/** The directory `theme.yaml` is read from. */
export function themeDir(): string {
  return process.env.ORBIT_THEME_DIR || DEFAULT_THEME_DIR;
}

async function directoryExists(dir: string): Promise<boolean> {
  try {
    return (await stat(dir)).isDirectory();
  } catch (err) {
    if (isMissing(err)) return false;
    throw err;
  }
}

function brandFromTheme(theme: Theme): IdentityTheme {
  const logo = theme.slots.logo;
  const favicon = theme.slots.favicon;
  const logoUrls = logo?.kind === 'image' ? logo.urls : {};
  const faviconUrls = favicon?.kind === 'image' ? favicon.urls : {};
  const faviconDark = faviconUrls.dark;
  const faviconLight = faviconUrls.light;
  return {
    productName: theme.productName ?? defaultTheme.productName,
    logo: {
      dark: logoUrls.dark ?? defaultTheme.logo.dark,
      light: logoUrls.light ?? defaultTheme.logo.light,
    },
    ...(faviconDark || faviconLight
      ? {
          favicon: {
            ...(faviconDark ? { dark: faviconDark } : {}),
            ...(faviconLight ? { light: faviconLight } : {}),
          },
        }
      : {}),
  };
}

function describeVersion(koraVersion: string | null): string {
  return koraVersion ? `kora ${koraVersion}` : 'kora development build';
}

function logLoaded(dir: string, loaded: LoadedTheme): void {
  // The frontend has no server-side logger; one line at startup is the
  // monitoring the ADR asks for (Validation, Monitoring).
  if (!loaded.theme) {
    console.info(
      `[theme] no theme at ${dir}; serving the default (${describeVersion(loaded.koraVersion)})`,
    );
    return;
  }
  const { theme, report } = loaded;
  const ignored = report.ignored.map((e) => `${e.kind} ${e.name}`);
  console.info(
    `[theme] loaded theme at ${dir}: ${describeVersion(loaded.koraVersion)}, theme written for kora ${theme.koraVersion}${report.versionCheckSkipped ? ' (not compared: not a release build)' : ''}, hash ${theme.hash}` +
      `; ignored: ${ignored.length ? ignored.join(', ') : 'none'}` +
      `; falls through to the default: ${report.fallThrough.length ? report.fallThrough.join(', ') : 'nothing'}`,
  );
  const mixed = report.mixedIdentity;
  if (mixed.length) {
    console.warn(
      `[theme] the theme at ${dir} mixes its own identity with kora defaults; still showing kora: ${mixed.join(', ')}`,
    );
  }
}

/**
 * Loads the theme document in `dir`. Exported for tests; the app goes through
 * `getTheme()`, which loads once.
 */
export async function loadThemeFrom(dir: string): Promise<LoadedTheme> {
  const koraVersion = instanceKoraVersion();
  let text: string;
  try {
    text = await readThemeDocument(path.join(dir, 'theme.yaml'));
  } catch (err) {
    if (err instanceof ThemeYamlTooLargeError) {
      const error = new ThemeLoadError(dir, [
        { path: 'theme.yaml', message: `theme.yaml ${err.message}` },
      ]);
      console.error(`[theme] ${error.message}`);
      throw error;
    }
    if (!isMissing(err)) throw err;
    // Only an absent directory means "no theme". A directory with no
    // theme.yaml is a mis-mounted ConfigMap, and serving the default would
    // hide that.
    if (await directoryExists(dir)) {
      const error = new ThemeLoadError(dir, [
        { path: 'theme.yaml', message: 'the theme directory exists but has no theme.yaml' },
      ]);
      console.error(`[theme] ${error.message}`);
      throw error;
    }
    const loaded: LoadedTheme = {
      koraVersion,
      theme: null,
      report: EMPTY_REPORT,
      css: '',
      fontPreloads: [],
      brand: defaultTheme,
    };
    logLoaded(dir, loaded);
    return loaded;
  }

  const result = await validateTheme(text, {
    manifest: hadesManifest,
    koraVersion,
  });
  if (!result.ok) {
    const error = new ThemeLoadError(dir, result.errors);
    console.error(`[theme] ${error.message}`);
    throw error;
  }

  const { theme, report } = result;
  const loaded: LoadedTheme = {
    koraVersion,
    theme,
    report,
    css: generateThemeCss(theme),
    fontPreloads: fontPreloads(theme),
    brand: brandFromTheme(theme),
  };
  logLoaded(dir, loaded);
  return loaded;
}

let loading: Promise<LoadedTheme> | null = null;

/** The loaded theme, read once per process on first call. Rejects when the document is invalid. */
export function getTheme(): Promise<LoadedTheme> {
  loading ??= loadThemeFrom(themeDir());
  return loading;
}

/** The effective brand: the kora default overlaid by the loaded theme. */
export async function getBrand(): Promise<IdentityTheme> {
  return (await getTheme()).brand;
}
