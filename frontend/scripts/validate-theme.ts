/**
 * validate-theme: the init container's entry point (white-labelling theme ADR,
 * decision 3).
 *
 *   node validate-theme.mjs <documentFile>
 *
 * Reads the theme document the chart rendered into the frontend's ConfigMap
 * and runs the Hades validator at the version this image carries. It checks
 * the form of every slot URL and never fetches one, so a deploy does not
 * depend on the asset host being reachable.
 *
 * On success it prints the report: every ignored entry and every identity
 * value that falls through to the default, then a warning when the theme mixes its own identity with kora's. On failure it prints every fault.
 * Exit 0 or 1. Bundled by `scripts/bundle-validate-theme.mjs` into the image's
 * standalone directory. No React, no DOM, no Next.
 */
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import manifest from '@runkora/hades/manifest.json';
import { instanceKoraVersion } from '../src/lib/kora-version';
import { ThemeYamlTooLargeError, isMissing, readThemeDocument } from '../src/lib/theme-document';
import { validateTheme, type ThemeManifest, type ThemeReport } from '@runkora/hades/theme';

// The JSON import widens the manifest's unions to string; the cast restores them.
const hadesManifest = manifest as ThemeManifest;

function describeInstance(): string {
  const version = instanceKoraVersion();
  return version ? `kora ${version}` : 'a kora development build';
}

export interface ValidationRun {
  readonly ok: boolean;
  /** What to print, one entry per line. */
  readonly lines: readonly string[];
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
  const mixed = report.mixedIdentity;
  if (mixed.length) {
    lines.push(
      `warning: this theme mixes its own identity with kora defaults; still showing kora: ${mixed.join(', ')}`,
    );
  }
  return lines;
}

export async function validateDocument(documentFile: string): Promise<ValidationRun> {
  let text: string;
  try {
    text = await readThemeDocument(documentFile);
  } catch (err) {
    if (err instanceof ThemeYamlTooLargeError) {
      return { ok: false, lines: [`the theme document at ${documentFile} ${err.message}`] };
    }
    if (!isMissing(err)) throw err;
    return { ok: false, lines: [`no theme document at ${documentFile}`] };
  }

  const result = await validateTheme(text, {
    manifest: hadesManifest,
    koraVersion: instanceKoraVersion(),
  });

  const lines: string[] = [];
  if (!result.ok) {
    lines.push(`theme at ${documentFile} is invalid against ${describeInstance()}:`);
    for (const e of result.errors) lines.push(`  ${e.path || '(document)'}: ${e.message}`);
    lines.push(...reportLines(result.report));
    return { ok: false, lines };
  }

  lines.push(
    `theme ok: written for kora ${result.theme.koraVersion}, validated by ${describeInstance()}${result.report.versionCheckSkipped ? ' (version not compared: not a release build)' : ''}, hash ${result.theme.hash}`,
  );
  lines.push(...reportLines(result.report));
  return { ok: true, lines };
}

export async function main(argv: readonly string[]): Promise<number> {
  const [documentFile] = argv;
  if (!documentFile || argv.length > 1) {
    console.error('usage: validate-theme <documentFile>');
    return 2;
  }
  const run = await validateDocument(path.resolve(documentFile));
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
