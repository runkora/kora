/**
 * Bundles scripts/validate-theme.ts into one self-contained ESM file,
 * .next/standalone/validate-theme.mjs. The Dockerfile's runner stage copies it
 * with the rest of the standalone output, and the chart's init container runs it.
 *
 * esbuild is not a direct dependency. It is resolved through vitest and then
 * vite, because pnpm's strict layout keeps it off PATH.
 */
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const requireFromRoot = createRequire(path.join(root, 'package.json'));
const requireFromVitest = createRequire(requireFromRoot.resolve('vitest/package.json'));
const requireFromVite = createRequire(requireFromVitest.resolve('vite/package.json'));
const esbuild = requireFromVite('esbuild');

const outfile = process.argv[2] ?? path.join(root, '.next', 'standalone', 'validate-theme.mjs');

await esbuild.build({
  entryPoints: [path.join(root, 'scripts', 'validate-theme.ts')],
  bundle: true,
  platform: 'node',
  format: 'esm',
  target: 'node24',
  outfile,
  logLevel: 'info',
  // `yaml` is CommonJS and calls `require` for Node built-ins. An ESM bundle
  // needs a `require` in scope for those calls.
  banner: {
    js: "import { createRequire } from 'node:module';\nconst require = createRequire(import.meta.url);",
  },
});
