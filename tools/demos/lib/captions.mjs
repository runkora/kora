// Draws each caption as an image, for lib/edit.mjs to lay over the finished
// video. A caption drawn in the page would move and crop with every zoom; laid
// over the frame it stays the same size and in the same place in every shot.
import fs from "node:fs";
import path from "node:path";

// The caption bar, at the size it reads on a 1440px-wide page.
const STYLE = `
  html, body { margin: 0; background: transparent; }
  #caption {
    display: inline-block; max-width: 1008px; padding: 14px 24px; border-radius: 12px;
    background: rgba(20, 24, 31, 0.88); color: #f4f2ee;
    font: 500 20px/1.35 Inter, system-ui, sans-serif; letter-spacing: -0.01em;
    text-align: center;
  }
`;

/**
 * Renders every distinct caption to a PNG in dir, at `scale` device pixels
 * per CSS pixel, and returns a map from caption text to its file.
 *
 * @param {import("@playwright/test").Browser} browser
 * @param {string[]} texts
 * @param {{ dir: string, scale: number }} opts
 */
export async function renderCaptions(browser, texts, { dir, scale }) {
  fs.mkdirSync(dir, { recursive: true });
  const files = new Map();
  const unique = [...new Set(texts)];
  if (unique.length === 0) return files;
  const context = await browser.newContext({ viewport: { width: 1440, height: 400 }, deviceScaleFactor: scale });
  try {
    const page = await context.newPage();
    for (const [i, text] of unique.entries()) {
      await page.setContent(`<style>${STYLE}</style><div id="caption"></div>`);
      await page.locator("#caption").evaluate((el, t) => (el.textContent = t), text);
      const file = path.join(dir, `caption-${i}.png`);
      await page.locator("#caption").screenshot({ path: file, omitBackground: true });
      files.set(text, file);
    }
  } finally {
    await context.close();
  }
  return files;
}
