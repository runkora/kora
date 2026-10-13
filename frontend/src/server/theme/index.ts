/**
 * The loaded theme: the frontend's side of the white-labelling theme ADR,
 * decision 4.
 *
 * The bundle is a directory, `/etc/orbit/theme` by default, with `theme.yaml`
 * at its root and `assets/` beside it. The first call reads it, runs the Hades
 * validator, derives everything a page needs (the inline CSS, the font preloads,
 * the effective brand) and holds the result for the life of the process: the
 * theme changes only on redeploy, so nothing re-reads it. A bundle that fails
 * validation throws with every fault listed, so no page serves; the init
 * container running `scripts/validate-theme.ts` on the same bundle is what
 * normally keeps that from happening. No bundle means the kora default.
 *
 * `ORBIT_THEME_DIR` overrides the directory. It exists for tests, which point it
 * at the fixtures beside this module.
 */
import 'server-only';

import { readFile } from 'node:fs/promises';
import path from 'node:path';
import manifest from '@clear-route/hades/manifest.json';
import {
  fontPreloads,
  generateThemeCss,
  validateTheme,
  type Theme,
  type ThemeError,
  type ThemeManifest,
  type ThemeReport,
} from '@clear-route/hades/theme';
import { THEME_ASSET_PREFIX } from '@orbit/lib/routing';
import { defaultTheme, type IdentityTheme } from '@orbit/theme/default';

export const DEFAULT_THEME_DIR = '/etc/orbit/theme';

// The JSON import widens the manifest's mode and kind unions to string; the
// file is generated from the same types, so the cast only restores them.
const hadesManifest = manifest as ThemeManifest;

/** What the process holds after loading: the theme, or the default when there is none. */
export interface LoadedTheme {
  /** The running Hades version, which the theme's major must equal. */
  readonly hadesVersion: string;
  /** The validated theme, or null when no bundle is mounted. */
  readonly theme: Theme | null;
  /** The validator's report; empty when no bundle is mounted. */
  readonly report: ThemeReport;
  /** The CSS the root layout inlines; empty when no bundle is mounted. */
  readonly css: string;
  /** Resolved URLs of the theme's fonts, one `<link rel="preload">` each. */
  readonly fontPreloads: readonly string[];
  /** The effective brand: the kora default overlaid by the theme. */
  readonly brand: IdentityTheme;
}

/** A served asset: its bytes and the media type its format implies. */
export interface ThemeAsset {
  readonly bytes: Uint8Array;
  readonly contentType: string;
}

/** Thrown when the bundle fails validation. Every fault is in `errors` and in the message. */
export class ThemeLoadError extends Error {
  readonly errors: readonly ThemeError[];

  constructor(dir: string, errors: readonly ThemeError[]) {
    const lines = errors.map((e) => `  ${e.path || '(document)'}: ${e.message}`);
    super(`theme bundle at ${dir} is invalid:\n${lines.join('\n')}`);
    this.name = 'ThemeLoadError';
    this.errors = errors;
  }
}

const EMPTY_REPORT: ThemeReport = { ignored: [], fallThrough: [] };

const CONTENT_TYPES: Readonly<Record<string, string>> = {
  svg: 'image/svg+xml',
  woff2: 'font/woff2',
};

/** The directory the bundle is read from. */
export function themeDir(): string {
  return process.env.ORBIT_THEME_DIR || DEFAULT_THEME_DIR;
}

/** The URL the asset route serves a bundle path from, for the loaded theme's hash. */
export function themeAssetUrl(hash: string, assetPath: string): string {
  const encoded = assetPath.split('/').map(encodeURIComponent).join('/');
  return `${THEME_ASSET_PREFIX}${hash}/assets/${encoded}`;
}

function isMissing(err: unknown): boolean {
  return typeof err === 'object' && err !== null && (err as { code?: string }).code === 'ENOENT';
}

/**
 * Reads one asset by bundle-relative path; null when the bundle has no such
 * file. The validator has already refused paths that leave the bundle; the
 * resolved-path check here is the belt to that brace.
 */
async function readBundleAsset(dir: string, assetPath: string): Promise<Uint8Array | null> {
  const assetsDir = path.resolve(dir, 'assets');
  const resolved = path.resolve(assetsDir, assetPath);
  if (!resolved.startsWith(assetsDir + path.sep)) return null;
  try {
    return new Uint8Array(await readFile(resolved));
  } catch (err) {
    if (isMissing(err)) return null;
    throw err;
  }
}

function brandFromTheme(theme: Theme): IdentityTheme {
  const resolve = (assetPath: string) => themeAssetUrl(theme.hash, assetPath);
  const logo = theme.slots.logo;
  const favicon = theme.slots.favicon;
  const logoPaths = logo?.kind === 'image' ? logo.paths : {};
  const faviconPaths = favicon?.kind === 'image' ? favicon.paths : {};
  const faviconDark = faviconPaths.dark ? resolve(faviconPaths.dark) : undefined;
  const faviconLight = faviconPaths.light ? resolve(faviconPaths.light) : undefined;
  return {
    productName: theme.productName ?? defaultTheme.productName,
    logo: {
      dark: logoPaths.dark ? resolve(logoPaths.dark) : defaultTheme.logo.dark,
      light: logoPaths.light ? resolve(logoPaths.light) : defaultTheme.logo.light,
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

function logLoaded(dir: string, loaded: LoadedTheme): void {
  // The frontend has no server-side logger; one line at startup is the
  // monitoring the ADR asks for (Validation, Monitoring).
  if (!loaded.theme) {
    console.info(`[theme] no bundle at ${dir}; serving the default (Hades ${loaded.hadesVersion})`);
    return;
  }
  const { theme, report } = loaded;
  const ignored = report.ignored.map((e) => `${e.kind} ${e.name}`);
  console.info(
    `[theme] loaded bundle at ${dir}: Hades ${loaded.hadesVersion}, theme written for Hades ${theme.hadesVersion}, hash ${theme.hash}` +
      `; ignored: ${ignored.length ? ignored.join(', ') : 'none'}` +
      `; falls through to the default: ${report.fallThrough.length ? report.fallThrough.join(', ') : 'nothing'}`,
  );
}

/**
 * Loads the bundle at `dir`. Exported for tests; the app goes through
 * `getTheme()`, which loads once.
 */
export async function loadThemeFrom(dir: string): Promise<LoadedTheme> {
  const hadesVersion = hadesManifest.version;
  let text: string;
  try {
    text = await readFile(path.join(dir, 'theme.yaml'), 'utf8');
  } catch (err) {
    if (!isMissing(err)) throw err;
    const loaded: LoadedTheme = {
      hadesVersion,
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
    hadesVersion,
    readAsset: (assetPath) => readBundleAsset(dir, assetPath),
  });
  if (!result.ok) {
    const error = new ThemeLoadError(dir, result.errors);
    console.error(`[theme] ${error.message}`);
    throw error;
  }

  const { theme, report } = result;
  const resolveAsset = (assetPath: string) => themeAssetUrl(theme.hash, assetPath);
  const loaded: LoadedTheme = {
    hadesVersion,
    theme,
    report,
    css: generateThemeCss(theme, { resolveAsset }),
    fontPreloads: fontPreloads(theme, { resolveAsset }),
    brand: brandFromTheme(theme),
  };
  logLoaded(dir, loaded);
  return loaded;
}

let loading: Promise<LoadedTheme> | null = null;

/** The loaded theme, read once per process on first call. Rejects when the bundle is invalid. */
export function getTheme(): Promise<LoadedTheme> {
  loading ??= loadThemeFrom(themeDir());
  return loading;
}

/** The effective brand: the kora default overlaid by the loaded theme. */
export async function getBrand(): Promise<IdentityTheme> {
  return (await getTheme()).brand;
}

/**
 * One asset of the loaded theme, for the asset route. Null unless `hash` is
 * the loaded theme's hash and `assetPath` is a path the theme references, so
 * the route can serve nothing else from the bundle.
 */
export async function getThemeAsset(hash: string, assetPath: string): Promise<ThemeAsset | null> {
  const { theme } = await getTheme();
  if (!theme || hash !== theme.hash) return null;
  const bytes = theme.assets.get(assetPath);
  if (!bytes) return null;
  const extension = assetPath.slice(assetPath.lastIndexOf('.') + 1).toLowerCase();
  return { bytes, contentType: CONTENT_TYPES[extension] ?? 'application/octet-stream' };
}
