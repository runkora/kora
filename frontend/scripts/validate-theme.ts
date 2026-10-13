/**
 * validate-theme: the init container's entry point (white-labelling theme ADR,
 * decision 3).
 *
 *   node validate-theme.mjs <bundleDir> <documentFile>
 *
 * Reads `<bundleDir>/theme.yaml` and `<bundleDir>/assets/`, runs the Hades
 * validator at the version this image carries, and checks that the chart's
 * copy of the document at `<documentFile>` parses to the same document as the
 * bundle's. Parsed, not byte-equal: the chart copy has been through templating
 * and a ConfigMap, which can change line endings, quoting and key order, none
 * of which is a difference. A document that differs in substance means a
 * bundle that was not rebuilt, or a document that was not committed, and the
 * deploy fails with a message that says so.
 *
 * On success it prints the report: every ignored entry and every identity
 * value that falls through to the default. On failure it prints every fault.
 * Exit 0 or 1. Bundled by `scripts/bundle-validate-theme.mjs` into the image's
 * standalone directory. No React, no DOM, no Next.
 */
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse as parseYaml } from 'yaml';
import manifest from '@runkora/hades/manifest.json';
import { validateTheme, type ThemeManifest, type ThemeReport } from '@runkora/hades/theme';

// The JSON import widens the manifest's unions to string; the cast restores them.
const hadesManifest = manifest as ThemeManifest;

export interface ValidationRun {
  readonly ok: boolean;
  /** What to print, one entry per line. */
  readonly lines: readonly string[];
}

function isMissing(err: unknown): boolean {
  return typeof err === 'object' && err !== null && (err as { code?: string }).code === 'ENOENT';
}

/** Structural equality over what YAML produces: maps, sequences and scalars. */
export function deepEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((v, i) => deepEqual(v, b[i]));
  }
  if (a instanceof Date || b instanceof Date) {
    return a instanceof Date && b instanceof Date && a.getTime() === b.getTime();
  }
  const ka = Object.keys(a as object);
  const kb = Object.keys(b as object);
  if (ka.length !== kb.length) return false;
  return ka.every(
    (k) =>
      Object.prototype.hasOwnProperty.call(b, k) &&
      deepEqual((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]),
  );
}

/** Whether two YAML texts parse to the same document. */
export function documentsEqual(bundleText: string, documentText: string): boolean {
  return deepEqual(parseYaml(bundleText), parseYaml(documentText));
}

function reportLines(report: ThemeReport): string[] {
  const lines: string[] = [];
  lines.push(
    report.ignored.length
      ? `ignored (unknown to this Hades): ${report.ignored.map((e) => `${e.kind} ${e.name}`).join(', ')}`
      : 'ignored: none',
  );
  lines.push(
    report.fallThrough.length
      ? `falls through to the default: ${report.fallThrough.join(', ')}`
      : 'falls through to the default: nothing',
  );
  return lines;
}

export async function validateBundle(
  bundleDir: string,
  documentFile: string,
): Promise<ValidationRun> {
  const lines: string[] = [];
  const bundlePath = path.join(bundleDir, 'theme.yaml');

  let bundleText: string;
  try {
    bundleText = await readFile(bundlePath, 'utf8');
  } catch (err) {
    if (!isMissing(err)) throw err;
    return { ok: false, lines: [`no theme.yaml in the bundle at ${bundleDir}`] };
  }

  let documentText: string;
  try {
    documentText = await readFile(documentFile, 'utf8');
  } catch (err) {
    if (!isMissing(err)) throw err;
    return { ok: false, lines: [`no chart-supplied document at ${documentFile}`] };
  }

  const result = await validateTheme(bundleText, {
    manifest: hadesManifest,
    hadesVersion: hadesManifest.version,
    readAsset: async (assetPath) => {
      try {
        return new Uint8Array(await readFile(path.join(bundleDir, 'assets', assetPath)));
      } catch (err) {
        if (isMissing(err)) return null;
        throw err;
      }
    },
  });

  if (!result.ok) {
    lines.push(`theme at ${bundlePath} is invalid against Hades ${hadesManifest.version}:`);
    for (const e of result.errors) lines.push(`  ${e.path || '(document)'}: ${e.message}`);
    lines.push(...reportLines(result.report));
    return { ok: false, lines };
  }

  let same: boolean;
  try {
    same = documentsEqual(bundleText, documentText);
  } catch (err) {
    return {
      ok: false,
      lines: [
        `the chart-supplied document at ${documentFile} is not valid YAML: ${(err as Error).message}`,
      ],
    };
  }
  if (!same) {
    return {
      ok: false,
      lines: [
        `the chart-supplied document at ${documentFile} does not match the bundle's theme.yaml at ${bundlePath}.`,
        'They parse to different documents: either the bundle image was not rebuilt after the document changed, or the document was not updated after the bundle. Rebuild the bundle from the committed document, or revert the document.',
      ],
    };
  }

  lines.push(
    `theme ok: written for Hades ${result.theme.hadesVersion}, validated by Hades ${hadesManifest.version}, hash ${result.theme.hash}`,
  );
  lines.push(...reportLines(result.report));
  return { ok: true, lines };
}

export async function main(argv: readonly string[]): Promise<number> {
  const [bundleDir, documentFile] = argv;
  if (!bundleDir || !documentFile) {
    console.error('usage: validate-theme <bundleDir> <documentFile>');
    return 2;
  }
  const run = await validateBundle(path.resolve(bundleDir), path.resolve(documentFile));
  for (const line of run.lines) (run.ok ? console.log : console.error)(line);
  return run.ok ? 0 : 1;
}

const invokedDirectly =
  typeof process.argv[1] === 'string' && import.meta.url === pathToFileURL(process.argv[1]).href;

if (invokedDirectly) {
  main(process.argv.slice(2)).then(
    (code) => {
      process.exitCode = code;
    },
    (err) => {
      console.error(err instanceof Error ? (err.stack ?? err.message) : String(err));
      process.exitCode = 1;
    },
  );
}
