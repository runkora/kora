// Renders a recording's edits from its timeline: cuts, speed changes and
// camera moves. Recording stays a plain capture of a person using the product;
// what a viewer sees is decided here.
//
// Two ffmpeg passes run on the full-resolution recording:
//
// 1. Cuts and speed. The recording is split at every cut and speed change,
//    each piece is retimed, and the pieces are joined, hard or with a blurred
//    slide for a "whip" cut.
// 2. Camera and captions. Every frame is cut down to the window the camera
//    sees and scaled back to the full frame, following the zoom steps with
//    eased moves. Working on the 2x recording keeps a 2x zoom sharp. The
//    captions, drawn by lib/captions.mjs, are laid over the result, so no
//    zoom moves or crops them.
import fs from "node:fs";
import path from "node:path";
import { ffmpeg } from "./recorder.mjs";

const FPS = 60;
const WHIP = 0.35;
const FADE = 0.25;

/**
 * @param {object} opts
 * @param {string} opts.raw        The recording.
 * @param {string} opts.out        The edited MP4.
 * @param {{ file: string, from: number, to: number }[]} [opts.captions]
 *   Caption images and when each shows, in ms of the recording, drawn at the
 *   output's 1920px width.
 * @param {{ file: string, width?: number, fps?: number }} [opts.gif]
 *   Also writes a GIF, 1000px wide at 12 fps unless set.
 * @param {object[]} opts.events   Timeline events, in ms of the recording.
 * @param {number} opts.durationMs The recording's length.
 * @param {{ width: number, height: number }} opts.viewport  CSS pixels.
 * @param {number} opts.scale      Device pixels per CSS pixel in the recording.
 * @returns {(ms: number) => number} Maps recording time to edited time, in ms.
 */
export async function edit({ raw, out, gif, captions = [], events, durationMs, viewport, scale }) {
  const work = `${out}.work`;
  fs.mkdirSync(work, { recursive: true });
  const { pieces, map } = plan(events, durationMs / 1000);

  // Pass 1: cuts and speed.
  const graph = [];
  pieces.forEach((p, i) => {
    graph.push(
      `[0:v]trim=start=${p.from.toFixed(4)}:end=${p.to.toFixed(4)},setpts=(PTS-STARTPTS)/${p.factor},fps=${FPS},format=yuv420p,settb=AVTB[p${i}]`,
    );
  });
  let joined = "p0";
  pieces.slice(1).forEach((p, j) => {
    const i = j + 1;
    if (p.transition === "whip") {
      graph.push(`[${joined}][p${i}]xfade=transition=smoothleft:duration=${WHIP}:offset=${p.outStart.toFixed(4)}[j${i}]`);
    } else {
      graph.push(`[${joined}][p${i}]concat=n=2:v=1:a=0[j${i}]`);
    }
    joined = `j${i}`;
  });
  const cutFile = path.join(work, "cut.mp4");
  const cutGraph = path.join(work, "cut.txt");
  fs.writeFileSync(cutGraph, graph.join(";\n"));
  await ffmpeg(["-y", "-i", raw, "-filter_complex_script", cutGraph, "-map", `[${joined}]`, "-c:v", "libx264", "-preset", "fast", "-crf", "14", cutFile]);

  // Pass 2: camera.
  const W = viewport.width * scale;
  const H = viewport.height * scale;
  const keys = events
    .filter((e) => e.step === "zoom")
    .map((e) => ({
      t: map(e.t) / 1000,
      ease: e.ease / 1000,
      z: e.scale,
      x: e.box ? (e.box.x + e.box.width / 2) * scale : W / 2,
      y: e.box ? (e.box.y + e.box.height / 2) * scale : H / 2,
      match: e.match,
    }));
  // A match cut shows two targets at the same place on screen. A target near
  // the page's edge cannot be centred, so the zoom before the cut is moved to
  // put its target where the matching one lands.
  keys.forEach((k, i) => {
    const before = keys[i - 1];
    if (!k.match || !before) return;
    const window = (size, z, c) => Math.min(Math.max(c - size / z / 2, 0), size - size / z);
    const sx = (k.x - window(W, k.z, k.x)) * k.z;
    const sy = (k.y - window(H, k.z, k.y)) * k.z;
    before.x = before.x - sx / before.z + W / before.z / 2;
    before.y = before.y - sy / before.z + H / before.z / 2;
  });
  // zoompan reads the zoom and the window's top-left per frame, in the
  // frame's own pixels, with `it` as the frame's time. One frame in makes one
  // frame out.
  const Z = curve(keys, "z", 1);
  const X = curve(keys, "x", W / 2);
  const Y = curve(keys, "y", H / 2);
  const graph2 = [
    `[0:v]zoompan=z='${Z}':x='clip(${X}-iw/zoom/2,0,iw-iw/zoom)':y='clip(${Y}-ih/zoom/2,0,ih-ih/zoom)':d=1:s=${W}x${H}:fps=${FPS},` +
      `scale=1920:-2:flags=lanczos,format=yuv420p[c0]`,
  ];
  // Each caption is a still image input that fades in and out, centred 54px
  // above the bottom, as the page's caption bar sat.
  const shown = captions
    .map((c) => ({ ...c, from: map(c.from) / 1000, to: map(c.to) / 1000 }))
    .filter((c) => c.to - c.from > 0.2);
  shown.forEach((c, i) => {
    const fade = Math.min(FADE, (c.to - c.from) / 3);
    graph2.push(
      `[${i + 1}:v]format=rgba,fade=t=in:st=${c.from.toFixed(3)}:d=${fade.toFixed(3)}:alpha=1,` +
        `fade=t=out:st=${(c.to - fade).toFixed(3)}:d=${fade.toFixed(3)}:alpha=1[t${i}]`,
      `[c${i}][t${i}]overlay=x=(W-w)/2:y=H-h-54:enable='between(t,${c.from.toFixed(3)},${c.to.toFixed(3)})':format=auto[c${i + 1}]`,
    );
  });
  graph2.push(`[c${shown.length}]format=yuv420p[v]`);
  const cameraGraph = path.join(work, "camera.txt");
  fs.writeFileSync(cameraGraph, graph2.join(";\n"));
  const length = (pieces.at(-1).outStart + (pieces.at(-1).to - pieces.at(-1).from) / pieces.at(-1).factor).toFixed(3);
  const inputs = shown.flatMap((c) => ["-loop", "1", "-framerate", String(FPS), "-t", length, "-i", c.file]);
  await ffmpeg(["-y", "-i", cutFile, ...inputs, "-filter_complex_script", cameraGraph, "-map", "[v]", "-c:v", "libx264", "-preset", "slow", "-crf", "17", "-movflags", "+faststart", out]);

  if (gif) {
    const { width = 1000, fps = 12 } = gif;
    // One palette for the whole clip, built from the frames' differences, so
    // the product's flat colours stay flat and the moving parts keep detail.
    await ffmpeg([
      "-y",
      "-i", out,
      "-filter_complex", `fps=${fps},scale=${width}:-2:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle`,
      gif.file,
    ]);
  }
  fs.rmSync(work, { recursive: true, force: true });
  return map;
}

// Splits the recording into the pieces that stay, each with its speed, its
// start in the edited video and how it joins the piece before it.
function plan(events, duration) {
  const cuts = events.filter((e) => e.step === "cut").map((e) => ({ from: e.t / 1000, to: e.until / 1000, transition: e.transition }));
  const speeds = events.filter((e) => e.step === "speed").map((e) => ({ from: e.t / 1000, to: e.until / 1000, factor: e.factor }));
  const edges = [...new Set([0, duration, ...cuts.flatMap((c) => [c.from, c.to]), ...speeds.flatMap((s) => [s.from, s.to])])]
    .filter((t) => t >= 0 && t <= duration)
    .sort((a, b) => a - b);

  const pieces = [];
  let outStart = 0;
  for (let i = 0; i < edges.length - 1; i++) {
    const from = edges[i];
    const to = edges[i + 1];
    const mid = (from + to) / 2;
    if (to - from < 1 / FPS || cuts.some((c) => mid > c.from && mid < c.to)) continue;
    const factor = speeds.find((s) => mid > s.from && mid < s.to)?.factor ?? 1;
    const after = cuts.find((c) => Math.abs(c.to - from) < 1e-6);
    const transition = pieces.length > 0 && after ? after.transition : "hard";
    if (transition === "whip") outStart -= WHIP;
    pieces.push({ from, to, factor, transition, outStart });
    outStart += (to - from) / factor;
  }

  // A time inside a cut maps to where the video resumes.
  const map = (ms) => {
    const t = ms / 1000;
    for (const p of pieces) {
      if (t < p.from) return p.outStart * 1000;
      if (t <= p.to) return (p.outStart + (t - p.from) / p.factor) * 1000;
    }
    return outStart * 1000;
  };
  return { pieces, map };
}

// An ffmpeg expression in `it`, the frame's time, for one camera value: held
// between moves, eased with smoothstep through each move.
function curve(keys, field, initial) {
  const build = (i, prev) => {
    if (i === keys.length) return `${prev}`;
    const k = keys[i];
    const next = build(i + 1, k[field]);
    if (k.ease <= 0) return `if(lt(it,${k.t.toFixed(4)}),${prev},${next})`;
    const p = `((it-${k.t.toFixed(4)})/${k.ease.toFixed(4)})`;
    const eased = `${prev}+(${k[field] - prev})*${p}*${p}*(3-2*${p})`;
    return `if(lt(it,${k.t.toFixed(4)}),${prev},if(lt(it,${(k.t + k.ease).toFixed(4)}),${eased},${next}))`;
  };
  return build(0, initial);
}
