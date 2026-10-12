/**
 * Generates `manifest.json` from `src/tokens/tokens.css` and
 * `src/tokens/surface.ts`, or with `--check` verifies the checked-in file is
 * what generation would produce. The only thing that writes the manifest.
 *
 *   node scripts/manifest.ts          # write
 *   node scripts/manifest.ts --check  # verify, exit 1 on drift
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { buildManifest, serializeManifest, SurfaceMismatchError } from './lib/manifest.ts';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const tokensPath = path.join(packageDir, 'src', 'tokens', 'tokens.css');
const manifestPath = path.join(packageDir, 'manifest.json');

const check = process.argv.includes('--check');

const version = (
  JSON.parse(fs.readFileSync(path.join(packageDir, 'package.json'), 'utf8')) as { version: string }
).version;

let text: string;
try {
  text = serializeManifest(buildManifest(fs.readFileSync(tokensPath, 'utf8'), version));
} catch (err) {
  if (err instanceof SurfaceMismatchError) {
    console.error(err.message);
    process.exit(1);
  }
  throw err;
}

if (!check) {
  fs.writeFileSync(manifestPath, text);
  console.log(`wrote ${path.relative(process.cwd(), manifestPath)}`);
  process.exit(0);
}

let current: string | null;
try {
  current = fs.readFileSync(manifestPath, 'utf8');
} catch {
  current = null;
}
if (current === text) {
  console.log('manifest in sync');
  process.exit(0);
}
console.error(
  current === null
    ? 'manifest.json is missing.'
    : 'manifest.json is out of sync with tokens.css and surface.ts.',
);
console.error('Run: make hades-manifest');
process.exit(1);
