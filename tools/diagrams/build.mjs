// Bundles the Excalidraw exportToSvg entry into a single browser IIFE that the
// headless page loads. Rebuilds when the entry or the resolved dependency set
// is newer than bundle.js.
import { build } from "esbuild";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));
const entry = path.join(dir, "entry.js");
const out = path.join(dir, "bundle.js");
// The bundle inlines @excalidraw/excalidraw, so a dependency change invalidates
// it just as an entry change does.
const inputs = [entry, path.join(dir, "package-lock.json"), path.join(dir, "package.json")];

export async function ensureBundle() {
  const newestInput = Math.max(
    ...inputs.filter(fs.existsSync).map((f) => fs.statSync(f).mtimeMs),
  );
  const fresh = fs.existsSync(out) && fs.statSync(out).mtimeMs >= newestInput;
  if (fresh) return out;
  await build({
    entryPoints: [entry],
    bundle: true,
    format: "iife",
    outfile: out,
    define: { "process.env.NODE_ENV": '"production"' },
    loader: { ".woff2": "dataurl", ".woff": "dataurl", ".ttf": "dataurl" },
    logLevel: "error",
  });
  return out;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  await ensureBundle();
  console.log("bundle ready");
}
