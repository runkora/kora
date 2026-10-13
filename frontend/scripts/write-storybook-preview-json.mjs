#!/usr/bin/env node
/**
 * Writes preview.json into a built Storybook, so a downloaded archive is
 * self-describing. Usage: node write-storybook-preview-json.mjs <outputDir> <koraVersion>
 * The Hades version is read from src/packages/hades/package.json and the
 * contract version from its manifest.json; the protocol version from
 * .storybook/theme-preview/protocol.ts.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const [outDir, koraVersion = '0.0.0-dev'] = process.argv.slice(2);
if (!outDir) {
  console.error('usage: write-storybook-preview-json.mjs <outputDir> [koraVersion]');
  process.exit(2);
}

const hades = JSON.parse(
  readFileSync(join(root, 'src/packages/hades/package.json'), 'utf8'),
).version;
const contract = JSON.parse(
  readFileSync(join(root, 'src/packages/hades/manifest.json'), 'utf8'),
).contractVersion;
const protocolSource = readFileSync(join(root, '.storybook/theme-preview/protocol.ts'), 'utf8');
const protocol = Number(/PROTOCOL_VERSION = (\d+)/.exec(protocolSource)?.[1]);
if (!Number.isInteger(protocol)) {
  console.error('could not read PROTOCOL_VERSION from protocol.ts');
  process.exit(1);
}

const file = join(resolve(outDir), 'preview.json');
writeFileSync(
  file,
  `${JSON.stringify({ kora: koraVersion, hades, contract, protocol }, null, 2)}\n`,
);
console.log(`wrote ${file}`);
