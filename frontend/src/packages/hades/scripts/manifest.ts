/**
 * Generates `manifest.json` and `theme.schema.json` from the token files, or with `--check` verifies them.
 *
 *   node scripts/manifest.ts          # write
 *   node scripts/manifest.ts --check  # verify, exit 1 on drift
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { buildManifest, serializeManifest, SurfaceMismatchError } from './lib/manifest.ts';
import { buildThemeSchema, serializeSchema } from './lib/schema.ts';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const tokensPath = path.join(packageDir, 'src', 'tokens', 'tokens.css');
const manifestPath = path.join(packageDir, 'manifest.json');
const schemaPath = path.join(packageDir, 'theme.schema.json');

const check = process.argv.includes('--check');

let manifestText: string;
let schemaText: string;
try {
  const manifest = buildManifest(fs.readFileSync(tokensPath, 'utf8'));
  manifestText = serializeManifest(manifest);
  schemaText = serializeSchema(buildThemeSchema(manifest));
} catch (err) {
  if (err instanceof SurfaceMismatchError) {
    console.error(err.message);
    process.exit(1);
  }
  throw err;
}

const outputs: [file: string, text: string][] = [
  [manifestPath, manifestText],
  [schemaPath, schemaText],
];

if (!check) {
  for (const [file, text] of outputs) {
    fs.writeFileSync(file, text);
    console.log(`wrote ${path.relative(process.cwd(), file)}`);
  }
  process.exit(0);
}

let drifted = false;
for (const [file, text] of outputs) {
  let current: string | null;
  try {
    current = fs.readFileSync(file, 'utf8');
  } catch {
    current = null;
  }
  if (current === text) continue;
  drifted = true;
  const name = path.basename(file);
  console.error(
    current === null
      ? `${name} is missing.`
      : `${name} is out of sync with tokens.css and surface.ts.`,
  );
}
if (!drifted) {
  console.log('manifest and theme schema in sync');
  process.exit(0);
}
console.error('Run: make hades-manifest');
process.exit(1);
