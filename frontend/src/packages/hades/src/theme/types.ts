/** The validated theme and the validation result. */
import type { Mode } from '../tokens/surface';

/** A token's values per mode; a mode the theme leaves unset is absent. */
export type ThemeTokenValues = Readonly<Partial<Record<Mode, string>>>;

export type ThemeSlot =
  | { readonly kind: 'image'; readonly urls: Readonly<Partial<Record<Mode, string>>> }
  | { readonly kind: 'font'; readonly url: string };

export interface Theme {
  /** The kora release the document records, `MAJOR.MINOR.PATCH`. */
  readonly koraVersion: string;
  readonly productName?: string;
  /** Known tokens only, keyed by custom property name, each with the modes the theme sets. */
  readonly tokens: Readonly<Record<string, ThemeTokenValues>>;
  /** Known slots only, keyed by manifest slot name. */
  readonly slots: Readonly<Record<string, ThemeSlot>>;
  /** SHA-256 over the document text, hex. */
  readonly hash: string;
}

export interface ThemeError {
  /** Dotted path into the document, e.g. `tokens.--hades-raw-black.dark`; empty for the document itself. */
  readonly path: string;
  readonly message: string;
}

export interface IgnoredEntry {
  readonly kind: 'token' | 'slot';
  readonly name: string;
}

export interface ThemeReport {
  /** Entries under `tokens` or `slots` the running manifest does not know. */
  readonly ignored: readonly IgnoredEntry[];
  /** Identity values the theme leaves to the kora default: `productName`, `logo.dark`, ... */
  readonly fallThrough: readonly string[];
  /** The `fallThrough` list when the theme sets some identity values but not all, else empty. Never affects `ok`. */
  readonly mixedIdentity: readonly string[];
  /** True for a development build: `koraVersion` was checked for shape only. */
  readonly versionCheckSkipped: boolean;
}

export type ValidationResult =
  | { readonly ok: true; readonly theme: Theme; readonly report: ThemeReport }
  | { readonly ok: false; readonly errors: readonly ThemeError[]; readonly report: ThemeReport };
