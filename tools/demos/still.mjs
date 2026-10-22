// Takes the staged stills, the product screenshots for READMEs and pages.
//
//   node still.mjs                  write every still to out/<name>.png
//   node still.mjs agents           write one
//
// Each still is taken in kora's dark theme, out/<name>.png, and its light
// theme, out/<name>-light.png, for a README to show the one that matches the
// reader's GitHub theme.
//
// A still stages the platform first: the seed's two weeks of history, plus
// replayed production runs in progress, so the page shows work under way.
// Its headline numbers are then restated at the scale of an organisation's
// fleet, which the local cluster cannot run. Run node seed/seed.mjs first.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { signIn } from "./lib/session.mjs";
import { removeRun, removeStagedRuns, replayRun, stageRuns } from "./seed/seed.mjs";

const dir = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.join(dir, "out");
const config = {
  baseURL: process.env.KORA_URL ?? "https://orbit.local.clearroute.io",
  username: process.env.KORA_USERNAME ?? "dev@orbit.local.clearroute.io",
  password: process.env.KORA_PASSWORD ?? "password",
  stateFile: path.join(dir, ".auth", "state.json"),
};
// A 1536px-wide window at 2.25x: the page reads a little larger than at a
// desktop width, in a 3456px-wide image.
const SCALE = 2.25;
// The account a still shows as signed in, in place of the local cluster's dev
// login.
const PERSONA = { name: "Sam Carter", email: "sam@runkora.io" };

// Production runs in progress, each part-way through: Issue Fixer working an
// issue, Code Reviewer reading a pull request, Coder drafting a design.
const LIVE = [
  ["issue-654", { agent: "issue-fixer", trigger: "Kora Issue Fixer", upTo: 24 }],
  ["pr-652-review", { agent: "code-reviewer", trigger: "Kora Review (assigned)", eventType: "issue_comment", upTo: 12 }],
  ["issue-587-design", { agent: "coder", trigger: "Kora Issue Verifier", eventType: "issue_comment", upTo: 22 }],
];

// More of the fleet at work: runs of every webhook-started agent, begun over
// the last half hour, and one waiting for a slot. Dependabot Fixer starts
// recently enough to be in the frame, for the variety.
const BUSY = [
  { agent: "change-mapper", trigger: "Kora Change Mapper (synchronize)", minutesAgo: 2 },
  { agent: "code-reviewer", trigger: "Kora Review (auto)", minutesAgo: 5 },
  { agent: "change-mapper", trigger: "Kora Change Mapper (opened)", minutesAgo: 6 },
  { agent: "dependabot-fixer", trigger: "Kora Dependabot Fixer (CI failed)", minutesAgo: 1 },
  { agent: "code-reviewer", trigger: "Kora Review (auto)", minutesAgo: 13 },
  { agent: "sre-triage", trigger: "SRE Triage (firing)", minutesAgo: 18 },
  { agent: "issue-verifier", trigger: "Kora Issue Verifier", minutesAgo: 22 },
  { agent: "change-mapper", trigger: "Kora Change Mapper (synchronize)", minutesAgo: 27 },
  { agent: "change-mapper", trigger: "Kora Change Mapper (opened)", minutesAgo: 0, state: "pending" },
];

// The Agents page's numbers for a fleet of 43 agents running about 4,000 runs
// a week. Each section is found by its label and the numbers after it are
// replaced in order.
//
// Spend follows what the runs cost: 72% are Change Mapper runs on a
// Haiku-class model at around $0.20, reviews cost around $1.15, Coder runs
// around $1.40 and the rest around $1, so the week comes to about $1.9k, with
// Code Reviewer close behind Change Mapper and Coder third. Tokens follow from
// the same mix. The legend lists the five largest agents and the rest of the
// 43 as Other.
const FLEET = {
  sections: [
    { label: "Agents", within: 6, values: ["43", "43"] },
    { label: "Active runs", within: 8, values: ["22", "19", "2", "1"] },
    { label: "Completed runs", within: 8, values: ["3,987", "3,836", "151", "96% success"] },
    { label: "Tokens", within: 10, values: ["4.3B", "3.2B", "1.0B", "76.9M"] },
    { label: "Running now", within: 6, values: ["22 runs in progress", "19", "2", "1"] },
  ],
  // Each tile's bar, segment by segment, so the bars match the numbers.
  bars: {
    Agents: [43],
    "Active runs": [19, 2, 1],
    "Completed runs": [3836, 151],
    Tokens: [3.2e9, 1.0e9, 76.9e6],
  },
  spend: {
    total: 1926.14,
    change: 0.08,
    agents: [
      ["Change Mapper", 574.12],
      ["Code Reviewer", 549.87],
      ["Coder", 336.40],
      ["Issue Fixer", 127.95],
      ["SRE Triage", 66.21],
      ["Other (38)", 271.59],
    ],
  },
};

const STILLS = {
  // The Agents page in one 16:10 frame, for the GitHub organisation README:
  // the fleet's numbers, its spend and the runs in progress.
  agents: {
    path: "/agents",
    viewport: { width: 1536, height: 960 },
    async stage(api) {
      const drafts = await api.get("/api/agents/types/drafts");
      for (const draft of drafts.ok() ? ((await drafts.json()).data ?? []) : []) {
        await api.delete(`/api/agents/types/drafts/${draft.id}`);
      }
      for (const [fixture, options] of LIVE) replayRun(fixture, options);
      stageRuns(BUSY);
    },
    async cleanup() {
      for (const [fixture] of LIVE) removeRun(fixture);
      removeStagedRuns();
    },
    restate: FLEET,
    // The version badge dates the image, and Running now's tool calls column
    // reads "–" for every run.
    hide: { version: true, columns: ["Tool calls"] },
  },
};

async function shoot(browser, name, still, theme) {
  const context = await browser.newContext({
    baseURL: config.baseURL,
    ignoreHTTPSErrors: true,
    storageState: config.stateFile,
    viewport: still.viewport,
    deviceScaleFactor: SCALE,
    colorScheme: theme,
  });
  try {
    await still.stage(context.request);
    const page = await context.newPage();
    await page.goto(still.path);
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(1500);
    await page.evaluate(({ local, persona }) => {
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        if (n.textContent.trim() === local) n.textContent = persona.email;
        else if (n.textContent.trim() === "Dev") n.textContent = persona.name;
      }
    }, { local: config.username, persona: PERSONA });
    if (still.restate) await page.evaluate(restate, still.restate);
    if (still.hide) await page.evaluate(hide, still.hide);
    const file = `${name}${theme === "dark" ? "" : `-${theme}`}.png`;
    await page.screenshot({ path: path.join(outDir, file) });
    console.log(`ok    ${name} (${theme}) -> out/${file}`);
  } finally {
    await still.cleanup?.();
    await context.close();
  }
}

// Runs in the page. Walks the text in order: a section's label starts it, and
// each following text that has the shape of the next replacement, a count, a
// percentage or an amount, takes that replacement.
function restate({ sections, bars, spend }) {
  const nodes = [];
  const walker = document.createTreeWalker(document.querySelector("main"), NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) if (n.textContent.trim()) nodes.push(n);
  const shape = (text) =>
    /in progress$/.test(text) ? /in progress$/ : /success$/.test(text) ? /success$/ : /[BM]$/.test(text) ? /^[\d.]+[KMB]$/ : /^[\d,]+$/;
  nodes.forEach((node, i) => {
    const section = sections.find((s) => node.textContent.trim().toLowerCase() === s.label.toLowerCase());
    if (!section) return;
    let next = 0;
    for (const later of nodes.slice(i + 1, i + 1 + section.within)) {
      if (next === section.values.length) break;
      if (shape(section.values[next]).test(later.textContent.trim())) later.textContent = section.values[next++];
    }
  });
  // A tile is the largest ancestor of its label that holds no other tile's
  // label; its bar is the run of rounded segments sized by flex.
  const labels = Object.keys(bars);
  const isLabel = (el, label) => el.children.length === 0 && el.textContent.trim().toLowerCase() === label.toLowerCase();
  for (const [label, values] of Object.entries(bars)) {
    let tile = [...document.querySelectorAll("main *")].find((el) => isLabel(el, label));
    while (tile?.parentElement && !labels.some((l) => l !== label && [...tile.parentElement.querySelectorAll("*")].some((el) => isLabel(el, l)))) {
      tile = tile.parentElement;
    }
    const segments = [...(tile?.querySelectorAll("div") ?? [])].filter((el) => /flex: [\d.e+]+ 1 0px/.test(el.getAttribute("style") ?? ""));
    if (segments.length === 0) continue;
    // A segment for a zero count is not drawn, so missing ones are cloned
    // from the last, in the next lighter shade.
    const shades = [100, 72, 52, 38];
    while (segments.length < values.length) {
      const copy = segments.at(-1).cloneNode(true);
      segments.at(-1).after(copy);
      segments.push(copy);
    }
    segments.forEach((segment, i) => {
      if (i >= values.length) return segment.remove();
      segment.style.flex = `${values[i]} 1 0px`;
      segment.style.background = `color-mix(in oklch, var(--color-accent) ${shades[i]}%, transparent)`;
    });
  }

  // The donut: one arc per legend row, in the legend's order, each as long as
  // its share of the ring less the gap between arcs.
  const ring = [...document.querySelectorAll("main svg")].find((svg) => svg.querySelectorAll("circle[stroke-dasharray]").length >= 2);
  if (ring) {
    const arcs = [...ring.querySelectorAll("circle[stroke-dasharray]")];
    const circumference = 2 * Math.PI * Number(arcs[0].getAttribute("r"));
    const gap = 2;
    let offset = 0;
    arcs.forEach((arc, i) => {
      const amount = spend.agents[i]?.[1];
      if (amount === undefined) return arc.remove();
      const length = Math.max(0, (amount / spend.total) * circumference - gap);
      arc.setAttribute("stroke-dasharray", `${length} ${circumference}`);
      arc.setAttribute("stroke-dashoffset", String(-offset));
      offset += length + gap;
    });
  }

  // Spend: the total, each agent's amount and share, and the week before.
  const start = nodes.findIndex((n) => n.textContent.trim() === "Spend");
  const end = nodes.findIndex((n, i) => i > start && n.textContent.trim() === "View usage");
  const money = (v) => `$${v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
  const block = nodes.slice(start, end);
  // A legend row is a name followed by its share, "%" and its amount. The
  // rows are rewritten in order, names included.
  const rows = block
    .map((n, i) => ({ name: n, percent: block[i + 1], sign: block[i + 2], amount: block[i + 3] }))
    .filter((r) => r.sign?.textContent.trim() === "%" && /^\d+$/.test(r.percent.textContent.trim()));
  rows.forEach((row, i) => {
    const [name, amount] = spend.agents[i] ?? [];
    if (name === undefined) return;
    row.name.textContent = name;
    row.percent.textContent = String(Math.round((amount / spend.total) * 100));
    row.amount.textContent = money(amount);
  });
  for (const [i, n] of block.entries()) {
    const text = n.textContent.trim();
    // The total sits inside the donut, which fits whole dollars only.
    if (/^\$[\d,.]+$/.test(text) && rows.length > 0 && i < block.indexOf(rows[0].name)) {
      n.textContent = `$${Math.round(spend.total).toLocaleString("en-US")}`;
    }
    if (/^[▲▼] /.test(text)) n.textContent = `${spend.change >= 0 ? "▲" : "▼"} ${Math.round(Math.abs(spend.change) * 100)}%`;
    if (text === "before") {
      const value = block.slice(0, i).reverse().find((m) => /^\$[\d,.]+$/.test(m.textContent.trim()));
      if (value) value.textContent = money(spend.total / (1 + spend.change));
    }
  }
}

// Runs in the page. Removes the version badge and whole table columns, found
// by their header's text, so the table lays out as if they never existed.
function hide({ version, columns = [] }) {
  if (version) {
    for (const el of document.querySelectorAll("header *, body *")) {
      if (el.children.length === 0 && /^v\d+\.\d+/.test(el.textContent.trim())) el.remove();
    }
  }
  for (const table of document.querySelectorAll("table")) {
    const heads = [...table.querySelectorAll("thead th")];
    const drop = heads
      .map((th, i) => (columns.some((c) => th.textContent.trim().toLowerCase() === c.toLowerCase()) ? i : -1))
      .filter((i) => i >= 0)
      .reverse();
    for (const row of table.querySelectorAll("tr")) {
      for (const i of drop) row.children[i]?.remove();
    }
  }
}

async function main() {
  const names = process.argv.slice(2);
  const unknown = names.filter((n) => !STILLS[n]);
  if (unknown.length > 0) throw new Error(`unknown still: ${unknown.join(", ")}`);
  fs.mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch();
  try {
    await signIn(browser, config);
    for (const name of names.length > 0 ? names : Object.keys(STILLS)) {
      for (const theme of ["dark", "light"]) await shoot(browser, name, STILLS[name], theme);
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
