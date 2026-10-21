// The scenes of the kora story walkthroughs: a platform engineer creates the
// Code Reviewer and a trigger that lets kora choose the agent for every
// @kora-agent mention, then a developer mentions @kora-agent on a pull request
// and kora's answer lands on it.
//
// Everything shown is real. The mention, the run and the reply are kora's
// production run on runkora/kora#652, replayed from seed/runs/pr-652-review.json,
// with the decision kora's routing made for it. The pull request is GitHub's
// page, shown as it was when the comment was posted (lib/github.mjs). The
// second mention, on runkora/kora#587, is the same for seed/runs/issue-587-design.json.
import { stagePullRequest } from "../../lib/github.mjs";
import { deliveryOf, removeRun, replayRun, reseed } from "../../seed/seed.mjs";
import { FIELDS, INSTRUCTION, SOURCE, TRIGGER } from "../trigger-create.demo.mjs";

export const PULL = { repo: "runkora/kora", number: 652, comment: "issuecomment-6287995418", reply: "issuecomment-6288012252" };
export const ISSUE = { repo: "runkora/kora", number: 587, reply: "issuecomment-6303249661" };
const COMMENT = "@kora-agent can you please reason through a design for the open comment, then propose the changes.";

// The decisions kora's routing made for the two mentions in production.
const MODEL = "typesafe/jev-1-14";
const REVIEW_RUN = {
  agent: "code-reviewer",
  trigger: TRIGGER,
  eventType: "issue_comment",
  routing: { model: MODEL, probability: 0.79, threshold: 0.7, scores: { "code-reviewer": 0.79, coder: 0.17, "issue-fixer": 0.04, "issue-verifier": 0 } },
};
const DESIGN_RUN = {
  agent: "coder",
  trigger: TRIGGER,
  eventType: "issue_comment",
  routing: { model: MODEL, probability: 0.81, threshold: 0.7, scores: { coder: 0.81, "issue-verifier": 0.12, "issue-fixer": 0.05, "code-reviewer": 0.02 } },
};
// How far into pr-652-review's 19 events the run is shown: its sandbox
// starting, then reading the pull request, then reading the code.
const STARTING = 4;
const STEPS = [10, 14];

// Production's Code Reviewer: its description, the opening of its prompt, and
// the GitHub actions it holds. It posts comments and reviews and reaches no
// other host.
const AGENT = {
  name: "Code Reviewer",
  slug: "code-reviewer",
  description:
    "Reviews a pull request for correctness, security and test coverage, and leaves one review with inline comments on the lines that need a change.",
  instruction: "You are a code review agent. You review one pull request and post one GitHub review.",
  prompt: ` The review contains inline comments on specific diff lines and a summary in the review body.

## Scope

Report the following, in priority order:

1.  **Correctness:** Logic errors, incorrect conditions, off-by-one errors, nil or null dereferences, unhandled errors, race conditions, resource leaks, broken invariants.
2.  **Security:** Injection, authentication and authorization gaps, secret exposure, unsafe deserialization, SSRF, path traversal, unvalidated untrusted input.
3.  **Data integrity:** Migrations that lose or corrupt data, non-idempotent operations that must be idempotent, missing transactions.
4.  **Contract changes:** Incompatible changes to public APIs, schemas or wire formats without a migration or compatibility path.
5.  **Repository conventions:** Rules written in the repository's instruction files.`,
  actions: ["contents:read", "pulls:read", "pulls:write", "issues:read", "issues:write"],
};
const CANDIDATES = { code: ["Code Reviewer", "Coder"], issue: ["Issue Fixer", "Issue Verifier"] };

/** Stages the pull request for the browser. Call from a feature's prepare. */
export async function preparePull(context) {
  revealOnPull = await stagePullRequest(context, { ...PULL, from: PULL.comment });
}
let revealOnPull;

/**
 * Resets for a walkthrough that creates the Code Reviewer and the trigger on
 * camera: the seeded Code Reviewer, its triggers and the mention trigger are
 * removed first. Afterwards the seed puts its triggers back; the Code Reviewer
 * made on camera has the seeded one's name and description, so it stays.
 */
export async function resetForCreate(api, { after = false } = {}) {
  await deleteMentionTrigger(api);
  removeRun("pr-652-review");
  removeRun("issue-587-design");
  if (after) {
    await reseed(api);
    return;
  }
  const agent = await findAgent(api);
  if (!agent) return;
  for (const t of await list(api, "/api/triggers")) {
    if (t.agent_type_id === agent.id) await api.delete(`/api/triggers/${t.id}`);
  }
  await deleteAgent(api);
}

/** Resets for a walkthrough that starts with the agent and trigger in place. */
export async function resetForMention(api) {
  await deleteMentionTrigger(api);
  removeRun("pr-652-review");
  removeRun("issue-587-design");
  const agents = new Map((await list(api, "/api/agents/types")).map((a) => [a.slug, a.id]));
  const source = (await list(api, "/api/trigger-sources")).find((s) => s.name === SOURCE);
  if (!source) throw new Error("the demo seed is missing; run node seed/seed.mjs");
  const res = await api.post("/api/triggers", {
    data: {
      name: TRIGGER,
      kind: "webhook",
      source_id: source.id,
      event_type: "issue_comment",
      target_kind: "routed",
      routing: {
        candidates: ["code-reviewer", "coder", "issue-fixer", "issue-verifier"].map((slug) => agents.get(slug)),
        model: MODEL,
        confidence: "balanced",
        fallback_agent_type_id: agents.get("coder"),
      },
      match_condition: { action: ["created"] },
      prompt_template: INSTRUCTION + FIELDS,
      max_parallel: 4,
      overflow: "wait",
      dedupe_enabled: true,
      autopause_enabled: true,
    },
  });
  if (!res.ok()) throw new Error(`create ${TRIGGER}: ${res.status()} ${await res.text()}`);
}

/** Removes what resetForMention added. */
export async function removeMention(api) {
  await deleteMentionTrigger(api);
  removeRun("pr-652-review");
  removeRun("issue-587-design");
}

/** S1: the Code Reviewer, created with its access and budget. */
export async function createAgent(demo) {
  const { page } = demo;
  const dialog = page.getByRole("dialog");
  const next = async () => {
    await demo.scrollTo(dialog.getByRole("heading", { level: 3 }).first());
    await demo.click(dialog.getByRole("button", { name: "Continue" }));
  };

  await demo.caption("Define an agent: its job, model, access and budget");
  await demo.pause(900);
  await demo.click(page.getByRole("button", { name: "New Agent" }).first());
  await dialog.waitFor();
  await demo.fast(2.5, async () => {
    await demo.type(dialog.getByPlaceholder("Deployment Assistant"), AGENT.name);
    await demo.type(dialog.getByPlaceholder("What this agent is responsible for."), AGENT.description);
    await next();
    await demo.type(dialog.getByPlaceholder(/^You are a deployment assistant/), AGENT.instruction);
    await demo.paste(AGENT.prompt);
    await demo.click(dialog.getByRole("radio", { name: /Claude Code/ }));
    await demo.click(dialog.getByRole("radio", { name: "Claude Sonnet 5" }));
    await next();
  });

  await demo.caption("It may read and comment on pull requests. Nothing else");
  await demo.click(dialog.getByRole("button", { name: "Expand github actions" }), { room: true });
  await demo.fast(2, async () => {
    for (const action of AGENT.actions) {
      await demo.click(dialog.getByRole("checkbox", { name: `Grant github ${action}` }));
    }
  });
  await demo.hover(dialog.getByText("Effective access", { exact: true }));
  await demo.pause(1400);
  await next();

  await demo.caption("With a budget kora enforces on every run");
  await demo.fast(2, async () => {
    await demo.click(dialog.getByRole("switch", { name: "Agent budget" }));
    await demo.type(dialog.getByRole("textbox", { name: "Agent budget in USD" }), "500");
    await demo.click(dialog.getByRole("switch", { name: "Run budget" }));
    await demo.type(dialog.getByRole("textbox", { name: "Run budget in USD" }), "15");
  });
  await next();
  await demo.pause(800);
  await demo.click(dialog.getByRole("button", { name: "Create agent" }));
  await dialog.getByText("Agent created").waitFor({ timeout: 30_000 });
  await dialog.getByText("$500 / monthly").waitFor();
  await demo.pause(1500);
  await page.keyboard.press("Escape");
}

/** S2: one trigger for every mention, with kora choosing the agent. */
export async function createTrigger(demo) {
  const { page } = demo;
  const dialog = page.getByRole("dialog");
  const step = async (heading) => {
    await demo.scrollTo(dialog.getByRole("heading", { level: 3 }));
    await demo.click(dialog.getByRole("button", { name: "Continue" }));
    await dialog.getByRole("heading", { name: heading, level: 3 }).waitFor();
  };
  const pickField = async (opener, field) => {
    await demo.click(opener, { room: true });
    await demo.type(page.getByRole("textbox", { name: "Search fields" }), field);
    await demo.click(page.locator(`[data-slot=template-catalog-item][data-token="${field}"]`));
  };
  const filter = async (n, field, values, operator) => {
    await demo.click(dialog.getByRole("button", { name: "Add filter" }), { room: true });
    await pickField(dialog.getByRole("button", { name: `Filter ${n} field` }), field);
    if (operator) await demo.select(dialog.getByRole("combobox", { name: `Filter ${n} operator` }), operator);
    await demo.type(dialog.getByRole("textbox", { name: `Filter ${n} values` }), values);
  };

  await demo.cut(async () => {
    await page.goto("/triggers");
    await page.getByRole("button", { name: "New trigger" }).waitFor();
    await page.waitForLoadState("networkidle");
  });
  await demo.caption("One trigger for every @kora-agent mention on GitHub");
  await demo.click(page.getByRole("button", { name: "New trigger" }));
  await demo.fast(2.5, async () => {
    await demo.click(dialog.getByText("Webhook", { exact: true }).first());
    await demo.type(dialog.getByRole("textbox", { name: "Trigger name" }), TRIGGER);
    await demo.click(dialog.getByRole("radio", { name: SOURCE }));
    await demo.click(dialog.getByRole("button", { name: "Event type" }), { room: true });
    await demo.type(page.getByPlaceholder(/^Search events/), "issue_comment");
    await demo.click(dialog.getByText("issue_comment", { exact: true }));
    await filter(1, "action", "created");
    await filter(2, "comment.body", "@kora-agent", "contains");
    for (const relationship of ["Owner", "Member"]) await demo.click(dialog.getByText(relationship, { exact: true }));
    await demo.type(dialog.getByRole("textbox", { name: "Prompt" }), INSTRUCTION);
    await demo.paste(FIELDS);
  });
  await step("Target");

  await demo.caption("kora chooses the agent that fits each comment");
  await demo.click(dialog.getByRole("radio", { name: "Let kora choose" }));
  await demo.scrollTo(dialog.getByText("Candidate agents", { exact: true }));
  const search = dialog.getByPlaceholder(/^Search agents/);
  await demo.fast(2, async () => {
    for (const [query, names] of Object.entries(CANDIDATES)) {
      await demo.type(search, query);
      for (const name of names) await demo.click(dialog.getByText(name, { exact: true }));
    }
  });
  await demo.click(dialog.getByRole("row", { name: "Use Jev 1.14" }));
  await demo.select(dialog.getByRole("combobox", { name: "Fallback agent" }), "Coder");
  await demo.pause(900);
  await demo.fast(2, async () => {
    await step("Delivery");
    await step("Review");
  });
  await demo.click(dialog.getByRole("button", { name: "Create trigger" }));
  await dialog.getByRole("heading", { name: "Trigger created" }).waitFor({ timeout: 30_000 });
  await demo.pause(900);
  await demo.click(dialog.getByRole("button", { name: "Done" }));
}

/**
 * S3: a developer mentions @kora-agent on the pull request. GitHub's post is
 * aborted; a cut shows the real comment that was posted.
 */
export async function mention(demo, { cutTo = true } = {}) {
  const { page } = demo;
  if (cutTo) {
    await demo.cut(async () => {
      await demo.caption();
      await page.goto(`https://github.com/${PULL.repo}/pull/${PULL.number}`);
      await page.locator("#new_comment_field").waitFor();
    });
  }
  await demo.caption("A developer asks @kora-agent on a pull request");
  await demo.pause(800);
  const box = page.locator("#new_comment_field");
  await demo.click(box);
  await demo.keys(COMMENT);
  await demo.pause(600);
  await demo.click(page.getByRole("button", { name: "Comment", exact: true }));
  // The webhook delivery starts the review run as the comment posts.
  await demo.cut(async () => {
    startRun();
    await revealOnPull(PULL.comment);
    await page.reload();
    await page.locator(`#${PULL.comment}`).waitFor();
    await demo.scrollTo(page.locator(`#${PULL.comment}`));
  });
  await demo.pause(1200);
}

/** S4: the delivery, and the agent kora chose for it. */
export async function routing(demo, { agent = "Code Reviewer", fixture = "pr-652-review", caption } = {}) {
  const { page } = demo;
  await demo.cut(async () => {
    await demo.caption();
    await page.goto("/triggers");
    await page.getByRole("row", { name: `Open ${TRIGGER}` }).waitFor();
    await page.waitForLoadState("networkidle");
  });
  await demo.caption(caption ?? `kora routes the comment to the ${agent}, and shows why`);
  await demo.click(page.getByRole("row", { name: `Open ${TRIGGER}` }));
  const drawer = page.getByRole("dialog");
  await demo.click(drawer.getByRole("tab", { name: "Deliveries" }));
  await demo.click(drawer.getByText(deliveryOf(fixture)));
  const chosen = drawer.getByText(agent, { exact: true }).last();
  await drawer.getByText("Routes at", { exact: false }).waitFor();
  await demo.zoom(chosen, { scale: 1.6 });
  await demo.hover(chosen);
  await demo.pause(2200);
  await demo.zoom(null);
  await page.keyboard.press("Escape");
}

/** S5: the run, opened from the Agents page and fast-forwarded to its answer. */
export async function followRun(demo) {
  const { page } = demo;
  let runId;
  const advance = (upTo) =>
    demo.cut(async () => {
      runId = replayRun("pr-652-review", { ...REVIEW_RUN, upTo });
      await page.reload();
      await page.getByText("Code Reviewer", { exact: true }).first().waitFor();
      await page.waitForLoadState("networkidle");
      // A reloaded run opens at its prompt; a person watching follows the
      // newest step.
      await page.evaluate(() => {
        for (const el of document.querySelectorAll("*")) {
          if (el.scrollHeight > el.clientHeight + 40 && /(auto|scroll)/.test(getComputedStyle(el).overflowY)) el.scrollTop = el.scrollHeight;
        }
      });
      await page.waitForTimeout(300);
    });

  await demo.caption("The Code Reviewer's run starts in its own sandbox");
  await demo.click(page.getByRole("link", { name: "Agents", exact: true }));
  const running = page.getByRole("row").filter({ hasText: startedRun });
  await demo.hover(running.getByText("Running"));
  await demo.pause(800);
  await demo.click(running.getByRole("button", { name: "Open" }).or(running.getByRole("link", { name: "Open" })));
  await page.getByText(/^Starting sandbox/).first().waitFor();
  await demo.pause(1500);
  await demo.caption("Fast-forward: it reads the change and the code");
  for (const upTo of STEPS) {
    await advance(upTo);
    await demo.pause(1100);
  }
  await advance(undefined);
  await demo.caption("It posts its design on the pull request");
  await demo.zoom(page.getByText(/^I posted a design/), { scale: 1.4, ease: 900 });
  await demo.pause(2200);
  await demo.zoom(null, { ease: 600 });
  return runId;
}

// Starts the review run, as the delivery does, at the moment of the mention.
function startRun() {
  startedRun = replayRun("pr-652-review", { ...REVIEW_RUN, upTo: STARTING });
  return startedRun;
}
let startedRun;

/** S6: the answer on the pull request. */
export async function reply(demo) {
  const { page } = demo;
  await demo.cut(
    async () => {
      await demo.caption();
      await revealOnPull(PULL.reply);
      await page.goto(`https://github.com/${PULL.repo}/pull/${PULL.number}`);
      await page.locator(`#${PULL.reply}`).waitFor();
      await demo.scrollTo(page.locator(`#${PULL.comment}`));
    },
    { transition: "whip" },
  );
  await demo.caption("The answer lands on the pull request, as a comment from kora");
  await demo.pause(900);
  await demo.scrollTo(page.locator(`#${PULL.reply}`));
  await demo.pause(1200);
  await demo.zoom(page.locator(`#${PULL.reply}`).getByRole("table").first(), { scale: 1.4, ease: 900 });
  await demo.pause(2200);
  await demo.zoom(null, { ease: 700 });
  await demo.caption();
}

/** The bonus beat: the same trigger, on an issue, chooses the Coder. */
export async function secondMention(demo) {
  const { page } = demo;
  await demo.cut(async () => {
    await demo.caption();
    replayRun("issue-587-design", { ...DESIGN_RUN, endedAgoMs: 60_000 });
  });
  await routing(demo, { agent: "Coder", fixture: "issue-587-design", caption: "The same trigger on an issue: kora picks the Coder" });
  await demo.cut(
    async () => {
      await demo.caption();
      await page.goto(`https://github.com/${ISSUE.repo}/issues/${ISSUE.number}`);
      await page.getByText("Proposed design", { exact: true }).first().waitFor();
      await demo.scrollTo(page.getByText("Proposed design", { exact: true }).first());
    },
    { transition: "whip" },
  );
  await demo.caption("And its design lands on the issue");
  await demo.pause(2500);
  await demo.caption();
}

async function findAgent(api) {
  const res = await api.get(`/api/agents/types/${AGENT.slug}`);
  return res.ok() ? (await res.json()).data : null;
}

// Deleting an agent tears down its workloads and can outlast the API's
// timeout, so the delete is repeated until the agent is gone.
async function deleteAgent(api) {
  for (let attempt = 0; ; attempt++) {
    const agent = await findAgent(api);
    if (!agent) break;
    if (attempt === 6) throw new Error(`could not delete agent ${AGENT.slug}`);
    await api.delete(`/api/agents/types/${agent.id}`, { timeout: 60_000 });
  }
  const drafts = await api.get("/api/agents/types/drafts");
  if (!drafts.ok()) return;
  for (const draft of (await drafts.json()).data ?? []) {
    if (draft.payload?.slug === AGENT.slug || draft.payload?.name === AGENT.name) await api.delete(`/api/agents/types/drafts/${draft.id}`);
  }
}

async function deleteMentionTrigger(api) {
  for (const t of await list(api, "/api/triggers")) {
    if (t.name === TRIGGER) await api.delete(`/api/triggers/${t.id}`);
  }
}

async function list(api, url) {
  const res = await api.get(url);
  if (!res.ok()) throw new Error(`GET ${url}: ${res.status()}`);
  const body = await res.json();
  return body.data ?? body;
}
