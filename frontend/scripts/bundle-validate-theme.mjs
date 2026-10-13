/**
 * Bundles scripts/validate-theme.ts into one self-contained ESM file,
 * .next/standalone/validate-theme.mjs, which the Dockerfile's runner stage
 * copies with the rest of the standalone output and the chart's init container
 * runs. esbuild is not a direct dependency of the workspace; vitest brings vite,
 * and vite brings esbuild, so it is resolved along that chain rather than from
 * PATH, which pnpm's strict layout keeps it off.
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
  // `yaml` ships CommonJS and requires Node built-ins; in an ESM bundle those
  // land as `require` calls, which only work once one is in scope.
  banner: {
    js: "import { createRequire } from 'node:module';\nconst require = createRequire(import.meta.url);",
  },
});
