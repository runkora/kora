/**
 * The theme-preview embedding protocol, version 1. Pure: no React, no DOM.
 * The shape is documented in PROTOCOL.md beside this file. Its version is
 * independent of kora's and Hades'; bump it only when the shape changes.
 * See the white-labelling theme ADR, Decision 6.
 */

export const PROTOCOL_VERSION = 1 as const;
export const REQUEST_TYPE = 'kora-theme-preview' as const;
export const RESULT_TYPE = 'kora-theme-preview-result' as const;

/** Sent by the embedding page: a theme document and the URLs of its assets. */
export interface ThemeRequest {
  type: typeof REQUEST_TYPE;
  version: typeof PROTOCOL_VERSION;
  /** The text of theme.yaml. */
  document: string;
  /** Bundle-relative asset path to a URL the consumer serves it from. */
  assets: Record<string, string>;
}

/** Sent by the embedding page: drop the injected theme. */
export interface ThemeClear {
  type: typeof REQUEST_TYPE;
  version: typeof PROTOCOL_VERSION;
  clear: true;
}

export interface ThemeResultError {
  path: string;
  message: string;
}

export interface ThemeResultReport {
  ignored: { kind: 'token' | 'slot'; name: string }[];
  fallThrough: string[];
}

/** The iframe's reply to every well-formed or version-mismatched message. */
export interface ThemeResult {
  type: typeof RESULT_TYPE;
  version: typeof PROTOCOL_VERSION;
  ok: boolean;
  errors?: ThemeResultError[];
  report?: ThemeResultReport;
}

export type ThemeMessage = ThemeRequest | ThemeClear;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** True for any object that claims to be a theme-preview message, of any version. */
export function isThemeMessageEnvelope(value: unknown): value is Record<string, unknown> {
  return isRecord(value) && value.type === REQUEST_TYPE;
}

/** True for a well-formed version 1 request or clear message. */
export function isThemeMessage(value: unknown): value is ThemeMessage {
  if (!isThemeMessageEnvelope(value) || value.version !== PROTOCOL_VERSION) return false;
  if (value.clear === true) return true;
  if (typeof value.document !== 'string' || !isRecord(value.assets)) return false;
  return Object.values(value.assets).every((url) => typeof url === 'string');
}

export function isClear(message: ThemeMessage): message is ThemeClear {
  return 'clear' in message;
}

export function errorResult(path: string, message: string): ThemeResult {
  return {
    type: RESULT_TYPE,
    version: PROTOCOL_VERSION,
    ok: false,
    errors: [{ path, message }],
  };
}

/**
 * The reply for a message that claims to be ours but is not acceptable: an
 * unknown version or a malformed body. Never throws.
 */
export function rejectionFor(envelope: Record<string, unknown>): ThemeResult {
  if (envelope.version !== PROTOCOL_VERSION) {
    return errorResult(
      'version',
      `unsupported protocol version ${JSON.stringify(envelope.version)}; this preview speaks version ${PROTOCOL_VERSION}`,
    );
  }
  return errorResult(
    '',
    'malformed message: expected { document: string, assets: Record<string, string> } or { clear: true }',
  );
}
