// Excalidraw scene generator template. Copy into your scratchpad, rename, and
// build the diagram by calling the helpers. Emits a .excalidraw source that the
// pipeline renders:  node <this> <out.excalidraw>
//
// Conventions baked in (keep them, they are what makes Orbit diagrams look right):
//   - roughness 0 + fontFamily 2  -> clean (non-sketchy) look
//   - shared palette below         -> consistent with existing diagrams
//   - title + labels only          -> no subtitles, taglines, or commentary
//   - no middot; use commas/colons
import fs from "node:fs";

let seed = 100;
const S = () => ++seed;
const base = (o) => ({ angle: 0, fillStyle: "solid", strokeWidth: 1.5, strokeStyle: "solid", roughness: 0, opacity: 100, seed: S(), version: 1, versionNonce: S(), isDeleted: false, boundElements: [], updated: 1, link: null, locked: false, groupIds: [], frameId: null, ...o });
const els = [];

// --- palette (match existing diagrams) ---
const BLUE = "#3b5bdb", DKBLUE = "#1c3aa9", MEM = "#eef2fb";      // control plane / orbit-system
const GREEN = "#2f9e44", DKGREEN = "#1b7a34", LGREEN = "#e6f4ea"; // data plane / agent
const GREY = "#868e96", INK = "#343a40", EDGE = "#5c6470";        // external / substrate / flow arrows
const CARD = "#ffffff";

// --- primitives ---
const rect = (x, y, w, h, stroke, bg, opt = {}) => els.push(base({ id: "r" + S(), type: "rectangle", x, y, width: w, height: h, strokeColor: stroke, backgroundColor: bg, strokeStyle: opt.dashed ? "dashed" : "solid", strokeWidth: opt.sw || 1.5, roundness: opt.sharp ? null : { type: 3 } }));
const ctext = (cx, cy, text, size, color) => { const h = size * 1.25, w = Math.max(text.length * size * 0.6, 24); els.push(base({ id: "t" + S(), type: "text", x: cx - w / 2, y: cy - h / 2, width: w, height: h, strokeColor: color, backgroundColor: "transparent", strokeWidth: 1, roundness: null, text, fontSize: size, fontFamily: 2, textAlign: "center", verticalAlign: "middle", containerId: null, originalText: text, lineHeight: 1.25, baseline: Math.round(size) })); };
const ltext = (x, cy, text, size, color) => { const h = size * 1.25, w = text.length * size * 0.6; els.push(base({ id: "t" + S(), type: "text", x, y: cy - h / 2, width: w, height: h, strokeColor: color, backgroundColor: "transparent", strokeWidth: 1, roundness: null, text, fontSize: size, fontFamily: 2, textAlign: "left", verticalAlign: "middle", containerId: null, originalText: text, lineHeight: 1.25, baseline: Math.round(size) })); };
const arrow = (x, y, dx, dy, color, opt = {}) => els.push(base({ id: "a" + S(), type: "arrow", x, y, width: Math.abs(dx), height: Math.abs(dy), strokeColor: color, backgroundColor: "transparent", strokeWidth: opt.sw || 1.4, strokeStyle: opt.dashed ? "dashed" : "solid", roundness: { type: 2 }, points: [[0, 0], [dx, dy]], lastCommittedPoint: null, startBinding: null, endBinding: null, startArrowhead: null, endArrowhead: opt.nohead ? null : "triangle" }));

// --- composites ---
// service chip: labelled card, optional sub-label
const chip = (x, y, w, h, label, sub) => { rect(x, y, w, h, BLUE, CARD, { sw: 1.5 }); ctext(x + w / 2, sub ? y + h / 2 - 8 : y + h / 2, label, 11, DKBLUE); if (sub) ctext(x + w / 2, y + h / 2 + 9, sub, 8, BLUE); };
// external / substrate box
const box = (x, y, w, h, label) => { rect(x, y, w, h, GREY, CARD); ctext(x + w / 2, y + h / 2, label, 10, INK); };
// zone: dashed container with a top-left label
const zone = (x, y, w, h, stroke, bg, label, color) => { rect(x, y, w, h, stroke, bg, { dashed: true, sw: 2.2 }); ltext(x + 24, y + 24, label, 12, color); };

// ===== EXAMPLE (delete and draw your diagram) =====
ltext(48, 36, "Diagram Title", 22, "#212529");
zone(48, 72, 300, 160, BLUE, "#f5f8ff", "CONTROL PLANE (orbit-system)", DKBLUE);
chip(70, 110, 256, 46, "some-service", "what it does");
zone(420, 72, 260, 160, GREEN, "#f0faf3", "DATA PLANE (tenant)", DKGREEN);
box(442, 470, 236, 42, "External thing"); // (place under a real layout)
arrow(348, 152, 72, 0, EDGE); ctext(384, 138, "flow label", 9, EDGE);
// ==================================================

const doc = { type: "excalidraw", version: 2, source: "orbit", elements: els, appState: { viewBackgroundColor: "#ffffff", gridSize: null }, files: {} };
const out = process.argv[2];
if (!out) { console.error("usage: node scene-template.mjs <out.excalidraw>"); process.exit(1); }
fs.writeFileSync(out, JSON.stringify(doc, null, 2) + "\n");
console.log("wrote", out, "elements", els.length);
