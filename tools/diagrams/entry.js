import { exportToSvg } from "@excalidraw/excalidraw";

// Exposed to the headless page by export.mjs; the only entry point the
// Playwright bundle needs.
window.__exportToSvg = exportToSvg;
