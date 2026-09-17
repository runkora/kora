// Renders a diagram to a PNG at natural size for visual self-review during
// authoring. Accepts a .excalidraw source (rendered via the bundle) or an
// already-rendered .svg. This is a review aid, not part of make check; the
// committed artifact is always the .svg from export.mjs.
//
//   node tools/diagrams/preview.mjs <input.excalidraw|input.svg> [out.png]
//
// Screenshots the <svg> element at its intrinsic size (deviceScaleFactor 2), so
// the PNG matches how the SVG renders on GitHub with no aspect distortion.
import fs from "node:fs";
import path from "node:path";
import { chromium } from "@playwright/test";
import { ensureBundle } from "./build.mjs";

async function svgFor(input, page) {
  if (input.endsWith(".svg")) return fs.readFileSync(input, "utf8");
  const scene = JSON.parse(fs.readFileSync(input, "utf8"));
  const bundle = fs.readFileSync(await ensureBundle(), "utf8");
  await page.setContent("<!doctype html><html><body></body></html>");
  await page.addScriptTag({ content: bundle });
  return page.evaluate(async (scene) => {
    const el = await window.__exportToSvg({
      elements: scene.elements,
      appState: { exportBackground: true, viewBackgroundColor: "#ffffff", exportPadding: 16 },
      files: scene.files || {},
    });
    return el.outerHTML;
  }, scene);
}

async function main() {
  const input = process.argv[2];
  if (!input || !fs.existsSync(input)) {
    console.error("usage: node preview.mjs <input.excalidraw|input.svg> [out.png]");
    process.exit(1);
  }
  const out = process.argv[3] || input.replace(/\.(excalidraw|svg)$/, ".preview.png");
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ deviceScaleFactor: 2 });
    const svg = await svgFor(path.resolve(input), page);
    await page.setContent(`<!doctype html><html><body style="margin:0;background:#fff">${svg}</body></html>`);
    const el = await page.$("svg");
    if (!el) throw new Error("no <svg> produced");
    await el.screenshot({ path: out });
    console.log(`preview ${out}`);
  } finally {
    await browser.close();
  }
}

main().catch((e) => {
  console.error(e.message || e);
  process.exit(1);
});
