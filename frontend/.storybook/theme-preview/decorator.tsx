/**
 * The theme-preview decorator: listens for the protocol on `window`, validates
 * the document with the Hades validator at the pinned version, builds CSS with
 * the Hades generator and injects it in the preview head. The logic is in
 * `handleThemeMessage` and `applyThemeStyle` so it tests without Storybook.
 * See PROTOCOL.md and the white-labelling theme ADR, Decision 6.
 */
import type { Decorator } from '@storybook/react';
import { HADES_VERSION } from '../../src/packages/hades/src/version';
import manifest from '../../src/packages/hades/manifest.json';
import {
  generateThemeCss,
  validateTheme,
  type ThemeManifest,
  type ValidationResult,
} from '@clear-route/hades/theme';
import {
  PROTOCOL_VERSION,
  RESULT_TYPE,
  errorResult,
  isClear,
  isThemeMessage,
  isThemeMessageEnvelope,
  rejectionFor,
  type ThemeResult,
} from './protocol';

export const STYLE_ATTRIBUTE = 'data-theme-preview';

export interface ThemeMessageDeps {
  validate: (
    document: string,
    readAsset: (path: string) => Promise<Uint8Array | null>,
  ) => Promise<ValidationResult>;
  generateCss: (
    theme: Extract<ValidationResult, { ok: true }>['theme'],
    resolveAsset: (path: string) => string,
  ) => string;
  fetchAsset: (url: string) => Promise<Uint8Array | null>;
  /** Injects the CSS, or removes the injected style when null. */
  applyStyle: (css: string | null) => void;
}

/** The production dependencies: the pinned manifest and version, `fetch`, the preview head. */
export const defaultDeps: ThemeMessageDeps = {
  validate: (document, readAsset) =>
    validateTheme(document, {
      manifest: manifest as unknown as ThemeManifest,
      hadesVersion: HADES_VERSION,
      readAsset,
    }),
  generateCss: (theme, resolveAsset) => generateThemeCss(theme, { resolveAsset }),
  fetchAsset: async (url) => {
    const response = await fetch(url);
    return response.ok ? new Uint8Array(await response.arrayBuffer()) : null;
  },
  applyStyle: applyThemeStyle,
};

/** Replaces the preview's theme `<style>`, or removes it when css is null. */
export function applyThemeStyle(css: string | null, doc: Document = document): void {
  doc.head.querySelectorAll(`style[${STYLE_ATTRIBUTE}]`).forEach((node) => node.remove());
  if (css === null) return;
  const style = doc.createElement('style');
  style.setAttribute(STYLE_ATTRIBUTE, '');
  style.textContent = css;
  doc.head.append(style);
}

/**
 * Handles one message event and returns the reply, or null when the message is
 * not ours. Never throws: a failure becomes an error reply.
 */
export async function handleThemeMessage(
  event: Pick<MessageEvent, 'data'>,
  deps: ThemeMessageDeps,
): Promise<ThemeResult | null> {
  const data: unknown = event.data;
  if (!isThemeMessageEnvelope(data)) return null;
  if (!isThemeMessage(data)) return rejectionFor(data);

  try {
    if (isClear(data)) {
      deps.applyStyle(null);
      return { type: RESULT_TYPE, version: PROTOCOL_VERSION, ok: true };
    }
    const urls = new Map(Object.entries(data.assets));
    const result = await deps.validate(data.document, async (path) => {
      const url = urls.get(path);
      if (url === undefined) return null;
      try {
        return await deps.fetchAsset(url);
      } catch {
        return null;
      }
    });
    const report = {
      ignored: result.report.ignored.map((entry) => ({ ...entry })),
      fallThrough: [...result.report.fallThrough],
    };
    if (!result.ok) {
      return {
        type: RESULT_TYPE,
        version: PROTOCOL_VERSION,
        ok: false,
        errors: result.errors.map((e) => ({ path: e.path, message: e.message })),
        report,
      };
    }
    // An asset path the generator resolves but the consumer did not map is a
    // validation fault already, so the lookup here always finds a URL.
    deps.applyStyle(deps.generateCss(result.theme, (path) => urls.get(path) ?? path));
    return { type: RESULT_TYPE, version: PROTOCOL_VERSION, ok: true, report };
  } catch (err) {
    return errorResult('', err instanceof Error ? err.message : 'theme preview failed');
  }
}

let installed = false;

/** Installs the window listener once, inside the preview iframe only. */
export function installThemePreviewListener(
  win: Window = window,
  deps: ThemeMessageDeps = defaultDeps,
): void {
  if (installed || win.parent === win) return;
  installed = true;
  win.addEventListener('message', (event: MessageEvent) => {
    void handleThemeMessage(event, deps).then((reply) => {
      if (reply === null) return;
      const target = event.source as WindowProxy | null;
      // The consumer may be on any origin; the reply goes to whoever asked.
      target?.postMessage(reply, event.origin && event.origin !== 'null' ? event.origin : '*');
    });
  });
}

/** Global decorator: installs the listener on first render, renders the story untouched. */
export const withThemePreview: Decorator = (Story) => {
  if (typeof window !== 'undefined') installThemePreviewListener();
  return Story();
};
