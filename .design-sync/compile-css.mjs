// Compile Terra's Tailwind v4 stylesheet into a self-contained, browser-ready
// CSS artifact for the design-sync bundle.
//
// WHY THIS EXISTS
// ───────────────────────────────────────────────────────────────────────────
// Terra styles components with Tailwind utility class STRINGS (className="flex
// bg-background …"), and its tokens live in tokens.css behind Tailwind v4's
// `@theme inline` directive. Neither the utility classes nor the `@theme`
// tokens are real CSS a browser understands until Tailwind compiles them:
//   - `@theme inline { --color-*: … }`  → real `:root { --color-*: … }`
//   - class usage found via `@source`   → emitted `.flex{}`, `.bg-background{}`…
// Shipping the raw tokens.css (as an earlier sync did) ships `@theme`, which
// browsers ignore → every design renders unstyled. This script produces the
// COMPILED output instead, which package-build.mjs copies verbatim into
// _ds_bundle.css (via cfg.cssEntry) and styles.css @imports.
//
// The compile entry mirrors the app's real Tailwind entry (frontend/src/app/
// globals.css): pull in the engine, the tokens, and scan Terra's source for
// class usage. The result is exactly the CSS the running app generates.
//
// Run from repo root:  node .design-sync/compile-css.mjs
// Output: frontend/src/packages/terra/.design-sync/_ds_bundle.css (the path
// cfg.cssEntry points at, resolved relative to the terra package).

import { createRequire } from 'node:module';
import { globSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const REPO = resolve(dirname(new URL(import.meta.url).pathname), '..');
const FRONTEND = join(REPO, 'frontend');
const TERRA = join(FRONTEND, 'src', 'packages', 'terra');
const OUT = join(TERRA, '.design-sync', '_ds_bundle.css');

// Resolve postcss + @tailwindcss/postcss from frontend/node_modules. Under
// pnpm's strict layout a bare import from the repo root fails, so resolve
// against frontend/package.json first; fall back to scanning .pnpm/ so a
// version bump doesn't break this.
const req = createRequire(pathToFileURL(join(FRONTEND, 'package.json')));
async function load(pkg, subpathGlob) {
  try {
    return (await import(pathToFileURL(req.resolve(pkg)))).default;
  } catch {
    const hits = globSync(join(FRONTEND, 'node_modules', '.pnpm', subpathGlob));
    if (!hits.length) throw new Error(`cannot resolve ${pkg} under frontend/node_modules`);
    return (await import(pathToFileURL(hits.sort().at(-1)))).default;
  }
}
const postcss = await load('postcss', 'postcss@*/node_modules/postcss/lib/postcss.mjs');
const tailwind = await load('@tailwindcss/postcss', '@tailwindcss+postcss@*/node_modules/@tailwindcss/postcss/dist/index.mjs');

// Mirror globals.css: engine + tokens + scan Terra source for class usage.
// `from` sits inside frontend so @tailwindcss/postcss resolves `tailwindcss`
// against frontend/node_modules; absolute @import/@source keep it cwd-agnostic.
const entry = [
  `@import "tailwindcss";`,
  `@import "${join(TERRA, 'src', 'tokens', 'tokens.css')}";`,
  `@source "${join(TERRA, 'src')}";`,
  ``,
].join('\n');

const from = join(FRONTEND, '.terra-compile-entry.css');
const result = await postcss([tailwind()]).process(entry, { from, to: OUT });

mkdirSync(dirname(OUT), { recursive: true });
writeFileSync(OUT, result.css);

// Fail loudly if the output isn't what the bundle needs — a silent regression
// here reships the unstyled bug.
const css = result.css;
const checks = {
  ':root tokens': /:root\b/.test(css) && /--color-background:/.test(css),
  'concrete colors': /oklch\(/.test(css),
  'utility classes': /\.flex\s*\{/.test(css) && /\.bg-background\s*\{/.test(css),
  'no @theme leftover': !/@theme\b/.test(css),
};
const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([k]) => k);
console.error(`compiled → ${OUT} (${(css.length / 1024).toFixed(0)} KB)`);
for (const [k, ok] of Object.entries(checks)) console.error(`  ${ok ? '✓' : '✗'} ${k}`);
if (failed.length) {
  console.error(`FAILED: ${failed.join(', ')}`);
  process.exit(1);
}
