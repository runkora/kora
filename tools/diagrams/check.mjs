// Sync check: every .excalidraw source must have a sibling .svg stamped with the
// current source sha256. Pure fs + crypto, no browser, safe for `make check`.
// A stale or missing SVG fails with the sources that need `make diagrams`.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));
const diagramsDir = path.resolve(dir, "../../docs/diagrams");
const STAMP = "excalidraw-sha256";
const hash = (buf) => crypto.createHash("sha256").update(buf).digest("hex");

if (!fs.existsSync(diagramsDir)) process.exit(0);

const stale = [];
for (const f of fs.readdirSync(diagramsDir)) {
  if (!f.endsWith(".excalidraw")) continue;
  const src = path.join(diagramsDir, f);
  const svg = src.replace(/\.excalidraw$/, ".svg");
  if (!fs.existsSync(svg)) {
    stale.push(`${f} -> missing ${path.basename(svg)}`);
    continue;
  }
  const want = hash(fs.readFileSync(src));
  const m = fs.readFileSync(svg, "utf8").match(new RegExp(`${STAMP}: ([0-9a-f]{64})`));
  if (!m) stale.push(`${path.basename(svg)} -> no ${STAMP} stamp`);
  else if (m[1] !== want) stale.push(`${path.basename(svg)} -> stale (source changed)`);
}

if (stale.length > 0) {
  console.error("Diagram SVGs out of sync with their Excalidraw sources:");
  for (const s of stale) console.error(`  ${s}`);
  console.error("Run: make diagrams");
  process.exit(1);
}
console.log("diagrams in sync");
