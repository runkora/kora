// Renders every docs/diagrams/*.excalidraw source to a sibling
// *.svg, stamping the source's sha256 into the SVG so the sync check can detect
// a stale render without launching a browser. Requires Playwright's Chromium
// (npm exec playwright install chromium).
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { ensureBundle } from "./build.mjs";

const dir = path.dirname(fileURLToPath(import.meta.url));
const diagramsDir = path.resolve(dir, "../../docs/diagrams");

export const STAMP = "excalidraw-sha256";
export const sourceHash = (buf) => crypto.createHash("sha256").update(buf).digest("hex");

function sources() {
  if (!fs.existsSync(diagramsDir)) return [];
  return fs
    .readdirSync(diagramsDir)
    .filter((f) => f.endsWith(".excalidraw"))
    .map((f) => path.join(diagramsDir, f));
}

async function main() {
  const files = sources();
  if (files.length === 0) {
    console.log("no .excalidraw sources under docs/diagrams");
    return;
  }
  const bundle = fs.readFileSync(await ensureBundle(), "utf8");
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    await page.setContent("<!doctype html><html><body></body></html>");
    await page.addScriptTag({ content: bundle });
    for (const src of files) {
      const raw = fs.readFileSync(src);
      let scene;
      try {
        scene = JSON.parse(raw.toString("utf8"));
      } catch (e) {
        throw new Error(`${path.basename(src)}: not valid Excalidraw JSON (${e.message})`);
      }
      const svg = await page.evaluate(async (scene) => {
        const el = await window.__exportToSvg({
          elements: scene.elements,
          appState: {
            exportBackground: true,
            viewBackgroundColor: "#ffffff",
            exportWithDarkMode: false,
            exportPadding: 16,
          },
          files: scene.files || {},
        });
        return el.outerHTML;
      }, scene);
      if (typeof svg !== "string" || !svg.startsWith("<svg")) {
        throw new Error(`${path.basename(src)}: export produced no SVG`);
      }
      const stamped = `<!-- ${STAMP}: ${sourceHash(raw)} -->\n${svg}\n`;
      const outPath = src.replace(/\.excalidraw$/, ".svg");
      fs.writeFileSync(outPath, stamped);
      console.log(`rendered ${path.basename(outPath)}`);
    }
  } finally {
    await browser.close();
  }
}

main().catch((e) => {
  console.error(e.message || e);
  process.exit(1);
});
