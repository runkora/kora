/**
 * Bundles scripts/validate-theme.ts into .next/standalone/validate-theme.mjs for the init container.
 * esbuild is resolved through vitest and vite, because pnpm keeps it off PATH.
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
  // `yaml` is CommonJS and calls `require`; an ESM bundle needs one in scope.
  banner: {
    js: "import { createRequire } from 'node:module';\nconst require = createRequire(import.meta.url);",
  },
});
