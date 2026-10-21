// Runs the feature walkthroughs in features/ against a running kora.
//
//   node run.mjs                     record every feature to out/<name>.mp4
//   node run.mjs agent-create        record one feature
//   node run.mjs --check             run every feature at full speed, no recording
//   node run.mjs --light             record in the light theme instead of dark
//
// KORA_URL, KORA_USERNAME and KORA_PASSWORD select the deployment and the
// account. They default to the local cluster's dev user.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "@playwright/test";
import { renderCaptions } from "./lib/captions.mjs";
import { edit } from "./lib/edit.mjs";
import { Demo } from "./lib/feature.mjs";
import { overlayScript } from "./lib/overlay.mjs";
import { startRecording } from "./lib/recorder.mjs";
import { signIn } from "./lib/session.mjs";

const dir = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.join(dir, "out");

const config = {
  baseURL: process.env.KORA_URL ?? "https://orbit.local.clearroute.io",
  username: process.env.KORA_USERNAME ?? "dev@orbit.local.clearroute.io",
  password: process.env.KORA_PASSWORD ?? "password",
  stateFile: path.join(dir, ".auth", "state.json"),
  githubStateFile: path.join(dir, ".auth", "github.json"),
};
const VIEWPORT = { width: 1440, height: 900 };
const SCALE = 2;

// The screencast sends frames at the window's real pixel size and ignores
// Playwright's emulated device scale factor, so a recording sets the scale on
// the browser itself and sizes the window instead of emulating a viewport.
const LAUNCH = {
  record: {
    args: [`--force-device-scale-factor=${SCALE}`, `--window-size=${VIEWPORT.width},${VIEWPORT.height}`],
  },
  check: {},
};

async function loadFeatures(names) {
  const featuresDir = path.join(dir, "features");
  const files = fs.readdirSync(featuresDir).filter((f) => f.endsWith(".demo.mjs"));
  const features = await Promise.all(
    files.map(async (f) => (await import(pathToFileURL(path.join(featuresDir, f)))).default),
  );
  if (names.length === 0) return features;
  const unknown = names.filter((n) => !features.some((f) => f.name === n));
  if (unknown.length > 0) throw new Error(`unknown feature: ${unknown.join(", ")}`);
  return features.filter((f) => names.includes(f.name));
}

async function runFeature(browser, feature, { mode, colorScheme }) {
  const context = await browser.newContext({
    baseURL: config.baseURL,
    ignoreHTTPSErrors: true,
    storageState: config.stateFile,
    viewport: mode === "record" ? null : VIEWPORT,
    // kora follows the system theme, so the emulated preference picks it.
    colorScheme,
    // demo.paste writes to the clipboard before pasting.
    permissions: ["clipboard-read", "clipboard-write"],
  });
  if (mode === "record") await context.addInitScript(overlayScript);
  // A walkthrough that starts on GitHub uses the session github-login.mjs saved.
  if (feature.github) {
    if (!fs.existsSync(config.githubStateFile)) throw new Error("no GitHub session; run node github-login.mjs");
    const { cookies } = JSON.parse(fs.readFileSync(config.githubStateFile, "utf8"));
    await context.addCookies(cookies);
  }
  await feature.prepare?.(context);

  // Uncaught errors in the app, reported with a failure: a step that cannot
  // find its target is often a page that crashed rendering it.
  const pageErrors = [];
  fs.rmSync(path.join(outDir, `${feature.name}.failure.png`), { force: true });
  try {
    await feature.reset?.(context.request);
    const page = await context.newPage();
    page.on("pageerror", (err) => pageErrors.push(err.message));
    await page.goto(feature.start);
    // GitHub keeps connections open, so its pages never go network idle.
    await page.waitForLoadState(feature.start.startsWith("https://github.com/") ? "load" : "networkidle");

    if (mode === "check") {
      await feature.steps(new Demo(page, { mode }));
      return;
    }

    const recording = await startRecording(page, {
      ...VIEWPORT,
      scale: SCALE,
      workDir: path.join(outDir, `.frames-${feature.name}`),
    });
    const demo = new Demo(page, { mode, elapsed: recording.elapsed });
    await page.waitForTimeout(600);
    await feature.steps(demo);
    await page.waitForTimeout(1200);
    const durationMs = recording.elapsed();
    const raw = path.join(outDir, `${feature.name}.raw.mp4`);
    const { offsetMs } = await recording.stop(raw);
    // Timeline times count from the recording's first frame.
    const events = demo.timeline.map((e) => ({
      ...e,
      t: e.t - offsetMs,
      ...(e.until !== undefined && { until: e.until - offsetMs }),
    }));
    const out = path.join(outDir, `${feature.name}.mp4`);
    // A caption shows until the next caption step or the end of the video.
    const marks = events.filter((e) => e.step === "caption");
    const shown = marks
      .map((e, i) => ({ text: e.text, from: e.t, to: marks[i + 1]?.t ?? durationMs - offsetMs }))
      .filter((c) => c.text);
    const images = await renderCaptions(browser, shown.map((c) => c.text), {
      dir: path.join(outDir, `.captions-${feature.name}`),
      scale: 1920 / VIEWPORT.width,
    });
    if (feature.gif || shown.length > 0 || events.some((e) => ["zoom", "cut", "speed"].includes(e.step))) {
      await edit({
        captions: shown.map((c) => ({ file: images.get(c.text), from: c.from, to: c.to })),
        raw,
        out,
        gif: feature.gif ? { ...(feature.gif === true ? {} : feature.gif), file: path.join(outDir, `${feature.name}.gif`) } : undefined,
        events,
        durationMs: durationMs - offsetMs,
        viewport: VIEWPORT,
        scale: SCALE,
      });
    } else {
      fs.renameSync(raw, out);
    }
    fs.rmSync(path.join(outDir, `.captions-${feature.name}`), { recursive: true, force: true });
    fs.writeFileSync(
      path.join(outDir, `${feature.name}.timeline.json`),
      JSON.stringify({ name: feature.name, title: feature.title, viewport: VIEWPORT, events }, null, 2),
    );
  } catch (err) {
    const page = context.pages()[0];
    if (page) await page.screenshot({ path: path.join(outDir, `${feature.name}.failure.png`) }).catch(() => {});
    if (pageErrors.length > 0) err.message += `\n      page errors: ${pageErrors.join(" | ")}`;
    throw err;
  } finally {
    await (feature.after ?? feature.reset)?.(context.request).catch((err) =>
      console.error(`  reset after ${feature.name} failed: ${err.message}`),
    );
    await context.close();
  }
}

async function main() {
  const args = process.argv.slice(2);
  const mode = args.includes("--check") ? "check" : "record";
  // Dark is kora's primary theme.
  const colorScheme = args.includes("--light") ? "light" : "dark";
  const features = await loadFeatures(args.filter((a) => !a.startsWith("--")));
  fs.mkdirSync(outDir, { recursive: true });

  const browser = await chromium.launch(LAUNCH[mode]);
  let failed = 0;
  try {
    await signIn(browser, config);
    for (const feature of features) {
      const started = Date.now();
      try {
        await runFeature(browser, feature, { mode, colorScheme });
        const took = ((Date.now() - started) / 1000).toFixed(1);
        const output = mode === "record" ? ` -> out/${feature.name}.mp4` : "";
        console.log(`ok    ${feature.name} (${took}s)${output}`);
      } catch (err) {
        failed++;
        const [summary, ...rest] = err.message.split("\n");
        const target = rest.find((line) => line.includes("waiting for"))?.trim();
        const errors = rest.find((line) => line.includes("page errors:"));
        console.log(`FAIL  ${feature.name}: ${summary}`);
        if (target) console.log(`      ${target.replace(/\x1b\[[0-9;]*m/g, "")}`);
        if (errors) console.log(errors);
        console.log(`      screenshot: out/${feature.name}.failure.png`);
      }
    }
  } finally {
    await browser.close();
  }
  if (failed > 0) process.exit(1);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
