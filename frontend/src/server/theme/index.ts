/**
 * The loaded theme, read once from `theme.yaml` in the mounted theme directory and held per process.
 * An invalid document throws, so no page serves. `KORA_THEME_DIR` overrides the directory for tests.
 */
import 'server-only';

import { stat } from 'node:fs/promises';
import path from 'node:path';
import defaults from '@runkora/hades/defaults.json';
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
import { instanceKoraVersion } from '@runkora/lib/kora-version';
import { defaultTheme, type IdentityTheme } from '@runkora/theme/default';
import { ThemeYamlTooLargeError, isMissing, readThemeDocument } from '@runkora/lib/theme-document';

export const DEFAULT_THEME_DIR = '/etc/kora/theme';

// The JSON import widens the manifest's unions to string; the cast restores them.
const hadesManifest = manifest as ThemeManifest;

/** What the process holds after loading: the theme, or the default when there is none. */
export interface LoadedTheme {
  /** The kora release of this instance, or null for a non-release build, which skips the version check. */
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
  contrastWarnings: [],
};

/** The directory `theme.yaml` is read from. */
export function themeDir(): string {
  return process.env.KORA_THEME_DIR || DEFAULT_THEME_DIR;
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
  // There is no server-side logger here, so log one line at startup.
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
  for (const w of report.contrastWarnings) {
    console.warn(`[theme] contrast warning at ${w.path}: ${w.message}`);
  }
}

/** Loads the theme document in `dir`. Exported for tests; the app uses `getTheme()`. */
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
    // Only an absent directory means no theme. A directory without theme.yaml is a
    // mis-mounted ConfigMap, which the default would hide.
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
    defaults,
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
