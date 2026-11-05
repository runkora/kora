// The vocabulary a feature file is written in. A feature is a scene: where it
// starts, what it resets, and the steps a person would take. The same steps run
// in two modes:
//
// - record: the cursor travels to each target on an eased path, text is typed
//   at a human pace, captions are written for the edit, and pauses hold the
//   frame.
// - check: the steps run at full speed with no pauses, captions or recording.
//   It proves every target still exists and every step still works, which is
//   the cheap guard to run before a release.
//
// Every step is written to a timeline (milliseconds since the recording began,
// the step and its target's box). Zooms, cuts and speed changes are written
// there too, and lib/edit.mjs renders them into the final video.

/**
 * Declares a feature walkthrough.
 *
 * @param {object} meta
 * @param {string} meta.name   Output file name, kebab-case.
 * @param {string} meta.title  What the walkthrough shows.
 * @param {string} meta.start  Path the recording opens on.
 * @param {(api: import("@playwright/test").APIRequestContext) => Promise<void>} [meta.reset]
 *   Returns the platform to the feature's starting state. Runs before, and
 *   after unless meta.after is set.
 * @param {(api: import("@playwright/test").APIRequestContext) => Promise<void>} [meta.after]
 *   Cleans up after the feature, for one whose reset takes something away
 *   that it then has to put back.
 * @param {(context: import("@playwright/test").BrowserContext) => Promise<void>} [meta.prepare]
 *   Sets up the browser before the first page opens, such as request routes.
 * @param {boolean} [meta.github]  Starts signed in to GitHub, as github-login.mjs saved.
 * @param {boolean | { width?: number, fps?: number }} [meta.gif]
 *   Also writes out/<name>.gif, optionally at a set width and frame rate.
 * @param {(demo: Demo) => Promise<void>} steps
 */
export function feature(meta, steps) {
  return { ...meta, steps };
}

export class Demo {
  constructor(page, { mode, elapsed = () => 0 }) {
    this.page = page;
    this.mode = mode;
    this.elapsed = elapsed;
    this.timeline = [];
    this.random = seeded(7);
  }

  get recording() {
    return this.mode === "record";
  }

  /**
   * Clicks the target. Pass room: true when the click opens something below
   * it, a menu or a new row, so the target is first brought high enough in
   * the view for that to open in place.
   */
  async click(target, { room = false } = {}) {
    const locator = await this.#visible(target);
    if (!this.recording) {
      await locator.click();
      return;
    }
    await locator.waitFor({ state: "visible" });
    const box = await this.#reveal(locator, { room });
    await this.#moveTo(box.x + box.width / 2, box.y + box.height / 2);
    await this.page.waitForTimeout(120);
    this.#log("click", locator, box);
    await this.page.mouse.down();
    await this.page.waitForTimeout(70);
    await this.page.mouse.up();
    await this.page.waitForTimeout(220);
  }

  async type(locator, text) {
    if (!this.recording) {
      await locator.fill(text);
      return;
    }
    await this.click(locator);
    // A field that opens with a default value is replaced, as fill() does.
    // Rich editors are contenteditable and have text rather than a value.
    const current = await locator.evaluate((el) => ("value" in el ? el.value : el.textContent));
    if (current !== "") await this.page.keyboard.press("ControlOrMeta+A");
    this.#log("type", locator, await this.#boxOf(locator), { text });
    await this.keys(text);
  }

  /** Types into whatever has focus, such as an editor between inserted fields. */
  async keys(text) {
    if (!this.recording) {
      await this.page.keyboard.type(text);
      return;
    }
    for (const char of text) {
      await this.page.keyboard.type(char);
      // 22-55ms per key, a touch longer after a word or a sentence.
      const base = 22 + this.random() * 33;
      await this.page.waitForTimeout(char === " " ? base + 25 : /[.,]/.test(char) ? base + 80 : base);
    }
    await this.page.waitForTimeout(250);
  }

  async hover(target) {
    const locator = await this.#visible(target);
    if (!this.recording) {
      await locator.hover();
      return;
    }
    const box = await this.#reveal(locator);
    await this.#moveTo(box.x + box.width / 2, box.y + box.height / 2);
    this.#log("hover", locator, box);
  }

  /** Pastes into whatever has focus, as a person pastes a prepared block. */
  async paste(text) {
    await this.page.evaluate((t) => navigator.clipboard.writeText(t), text);
    await this.page.keyboard.press("ControlOrMeta+V");
    if (this.recording) await this.page.waitForTimeout(600);
  }

  // A native select opens a menu headless Chromium never paints, so the cursor
  // goes to the select and the option is chosen directly.
  async select(target, label) {
    const locator = await this.#visible(target);
    if (this.recording) {
      const box = await this.#reveal(locator);
      await this.#moveTo(box.x + box.width / 2, box.y + box.height / 2);
      this.#log("select", locator, box, { label });
      await this.page.waitForTimeout(250);
    }
    await locator.selectOption({ label });
    if (this.recording) await this.page.waitForTimeout(400);
  }

  /**
   * Shows a caption from now until the next one, or ends it when called with
   * no text. lib/edit.mjs lays it over the video.
   */
  async caption(text) {
    if (!this.recording) return;
    this.timeline.push({ t: this.elapsed(), step: "caption", text: text ?? null });
    await this.page.waitForTimeout(300);
  }

  async pause(ms) {
    if (this.recording) await this.page.waitForTimeout(ms);
  }

  /**
   * Moves the camera to frame the target at `scale` times, or back to the
   * whole page when called with no target. The move eases over `ease`
   * milliseconds; 0 jumps, as a match cut does. match: true lines the zoom
   * before it up with this one, so the two targets meet on screen across a
   * cut. lib/edit.mjs renders it.
   */
  async zoom(target, { scale = 2, ease = 700, match = false } = {}) {
    if (!this.recording) return;
    if (!target) {
      this.timeline.push({ t: this.elapsed(), step: "zoom", box: null, scale: 1, ease });
    } else {
      const locator = await this.#visible(target);
      const box = await this.#settled(locator);
      this.timeline.push({ t: this.elapsed(), step: "zoom", box, scale, ease, match, target: locator.toString() });
    }
    if (ease > 0) await this.page.waitForTimeout(ease);
  }

  /**
   * Runs steps that are cut from the video, such as moving between pages.
   * transition "whip" blurs across the cut; the default is a hard cut.
   */
  async cut(steps, { transition = "hard" } = {}) {
    const from = this.elapsed();
    await steps();
    // A page just navigated to can paint a blank frame first; the cut runs on
    // past it.
    if (this.recording) await this.page.waitForTimeout(250);
    if (this.recording) this.timeline.push({ t: from, step: "cut", until: this.elapsed(), transition });
  }

  /** Runs steps played back `factor` times faster in the video. */
  async fast(factor, steps) {
    const from = this.elapsed();
    await steps();
    if (this.recording) this.timeline.push({ t: from, step: "speed", until: this.elapsed(), factor });
  }

  // Design-system checkboxes are a visually hidden input followed by the box
  // that is drawn, both inside a label. A person clicks the drawn box, and
  // scrolling a 1px input into view can push a scroll container to a blank
  // region, so the step acts on the box. The box is inside the label, so the
  // click still toggles the input.
  async #visible(locator) {
    await locator.waitFor({ state: "attached" });
    const hidden = await locator.evaluate((el) => {
      const r = el.getBoundingClientRect();
      return el instanceof HTMLInputElement && r.width <= 2 && r.height <= 2;
    });
    return hidden ? locator.locator("xpath=following-sibling::*[1]") : locator;
  }

  /**
   * Scrolls the target's container until the target sits at the top, as a
   * person scrolls back up to read a page from its start. The cursor stays.
   */
  async scrollTo(target) {
    const locator = await this.#visible(target);
    if (!this.recording) return;
    await this.#scroll(locator, { align: "start", always: true, band: 1 });
    await this.#settled(locator);
    await this.page.waitForTimeout(300);
  }

  // Scrolls a target into the upper part of its scroll container the way a
  // person would, smoothly, rather than jumping, and returns its settled box.
  async #reveal(locator, { room = false } = {}) {
    await this.#scroll(locator, { align: "center", always: false, band: room ? 0.6 : 0.85 });
    const box = await this.#settled(locator);
    const height = await this.page.evaluate(() => window.innerHeight);
    const width = await this.page.evaluate(() => window.innerWidth);
    if (box.y < 0 || box.y + box.height > height || box.x < 0 || box.x + box.width > width) {
      throw new Error(`${locator} is off screen at ${Math.round(box.x)},${Math.round(box.y)}`);
    }
    return box;
  }

  // The scroll is animated here and awaited: a native smooth scroll can be
  // dropped without notice, which leaves the cursor clicking empty space.
  async #scroll(locator, { align, always, band }) {
    await locator.evaluate(async (el, { align, always, band }) => {
      const r = el.getBoundingClientRect();
      // Only the nearest scrollable container moves. Scrolling an
      // overflow-hidden ancestor such as a modal's frame slides the whole
      // modal body out of view.
      let box = el.parentElement;
      while (box && !(/(auto|scroll)/.test(getComputedStyle(box).overflowY) && box.scrollHeight > box.clientHeight)) {
        box = box.parentElement;
      }
      // A page that scrolls as a whole, as GitHub's do, scrolls the document.
      box ??= document.scrollingElement;
      const c = box === document.scrollingElement ? { top: 0, height: window.innerHeight } : box.getBoundingClientRect();
      // A person keeps what they work on within the view, and higher up when
      // something is about to open below it. Outside that band the target is
      // brought up to a third of the way down.
      const comfortable = r.top >= c.top + 40 && r.bottom <= c.top + c.height * band;
      if (!always && comfortable) return;
      const from = box.scrollTop;
      const offset = align === "start" ? r.top - c.top - 24 : r.top - c.top - c.height / 3;
      const to = Math.max(0, Math.min(box.scrollHeight - box.clientHeight, from + offset));
      if (Math.abs(to - from) < 2) return;
      const duration = Math.min(900, 300 + Math.abs(to - from) * 0.25);
      const started = performance.now();
      await new Promise((resolve) => {
        const step = () => {
          const t = Math.min(1, (performance.now() - started) / duration);
          const e = t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2;
          box.scrollTop = from + (to - from) * e;
          if (t < 1) setTimeout(step, 16);
          else resolve();
        };
        step();
      });
    }, { align, always, band });
  }

  // A smooth scroll or an expanding section moves the target for a while, so
  // the cursor aims only once its box has stopped changing.
  async #settled(locator) {
    let box = await this.#boxOf(locator);
    for (let i = 0; i < 30; i++) {
      await this.page.waitForTimeout(100);
      const next = await this.#boxOf(locator);
      if (next.x === box.x && next.y === box.y) return next;
      box = next;
    }
    return box;
  }

  async #boxOf(locator) {
    const box = await locator.boundingBox();
    if (!box) throw new Error(`no box for ${locator}`);
    return box;
  }

  // Moves along an eased, slightly arced path. A straight constant-speed line
  // reads as a robot; real hands accelerate, decelerate and curve.
  async #moveTo(x, y) {
    const from = this.cursor ?? { x: x - 160, y: y + 120 };
    const dx = x - from.x;
    const dy = y - from.y;
    const distance = Math.hypot(dx, dy);
    const duration = Math.min(900, Math.max(280, 200 + distance * 0.5));
    const bow = Math.min(60, distance * 0.08) * (this.random() > 0.5 ? 1 : -1);
    const nx = distance ? -dy / distance : 0;
    const ny = distance ? dx / distance : 0;
    // Each move is a round trip to the browser, so progress comes from the
    // clock rather than a step count: the path takes `duration` however slow
    // the round trips are.
    const started = Date.now();
    for (let t = 0; t < 1; ) {
      t = Math.min(1, (Date.now() - started) / duration);
      const e = t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2;
      const arc = Math.sin(Math.PI * t) * bow;
      await this.page.mouse.move(from.x + dx * e + nx * arc, from.y + dy * e + ny * arc);
    }
    this.cursor = { x, y };
  }

  #log(step, locator, box, extra = {}) {
    this.timeline.push({
      t: this.elapsed(),
      step,
      target: locator.toString(),
      box: { x: box.x, y: box.y, width: box.width, height: box.height },
      ...extra,
    });
  }
}

// Deterministic jitter, so two recordings of the same release type and move
// identically and diffs between releases come from the product only.
function seeded(seed) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
