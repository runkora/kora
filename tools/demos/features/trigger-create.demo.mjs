// Creating the trigger kora's own repository runs on: a comment that mentions
// @kora-agent starts a run, and kora chooses the agent that fits the comment.
// The trigger, its prompt and the agents are copied from production, without
// its filter to the one repository.
import { feature } from "../lib/feature.mjs";

export const TRIGGER = "Kora Agent (mentioned)";
export const SOURCE = "runkora-github";

// The agents kora chooses between, by the search that finds them. The list
// pages at five, so a person searches. seed/seed.mjs creates them.
const CANDIDATES = {
  issue: ["Issue Fixer", "Issue Verifier"],
  code: ["Code Reviewer", "Coder"],
};

// The prompt opens with the instruction, typed, and continues with the
// delivery's fields, pasted as the block it is in production.
export const INSTRUCTION =
  "{{ comment.user.login }} mentioned @kora-agent in a comment. Do what the comment asks, within your role.";
export const FIELDS = `

Repository: {{ repository.full_name }}
Number: #{{ issue.number }}
Title: {{ issue.title }}
State: {{ issue.state }}
Link: {{ issue.html_url }}
Pull request link: {{ issue.pull_request.html_url }}
Comment: {{ comment.body }}
Comment link: {{ comment.html_url }}

If Pull request link is empty, this is an issue. Otherwise it is a pull request with the same number. Post your answer as a comment on the issue or pull request. Do not answer only in the run.`;

export default feature(
  {
    name: "trigger-create",
    title: "Start agents from GitHub events, routed by kora",
    start: "/triggers",
    reset,
  },
  async (demo) => {
    const { page } = demo;
    const dialog = page.getByRole("dialog");
    // The next step renders at the current scroll offset, so the finished
    // step is scrolled back to its top first and the next one opens at its
    // own top, with no frame halfway down it.
    const step = async (heading) => {
      await demo.scrollTo(dialog.getByRole("heading", { level: 3 }));
      await demo.click(dialog.getByRole("button", { name: "Continue" }));
      await dialog.getByRole("heading", { name: heading, level: 3 }).waitFor();
    };
    // The field catalog lists the event's fields; its items carry the field.
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

    await demo.caption("Start agents from the events your team already produces");
    await demo.pause(1500);
    await demo.click(page.getByRole("button", { name: "New trigger" }));
    await demo.click(dialog.getByText("Webhook", { exact: true }).first());

    await demo.type(dialog.getByRole("textbox", { name: "Trigger name" }), TRIGGER);
    await demo.click(dialog.getByRole("radio", { name: SOURCE }));
    await demo.click(dialog.getByRole("button", { name: "Event type" }), { room: true });
    await demo.type(page.getByPlaceholder(/^Search events/), "issue_comment");
    await demo.click(dialog.getByText("issue_comment", { exact: true }));

    await demo.caption("Run only for new comments that mention @kora-agent");
    await filter(1, "action", "created");
    await filter(2, "comment.body", "@kora-agent", "contains");

    await demo.caption("Only owners and organisation members can start a run");
    for (const relationship of ["Owner", "Member"]) {
      await demo.click(dialog.getByText(relationship, { exact: true }));
    }

    await demo.caption("The prompt pulls in fields from the event");
    await demo.type(dialog.getByRole("textbox", { name: "Prompt" }), INSTRUCTION);
    await demo.pause(900);
    await demo.paste(FIELDS);
    await demo.pause(1200);

    await step("Target");
    await demo.caption("Send every comment to one agent");
    await demo.type(dialog.getByPlaceholder(/^Search agents/), "coder");
    await demo.click(dialog.getByRole("radio", { name: "Coder" }));
    await demo.pause(1000);
    await demo.caption("Or let kora choose the agent that fits each comment");
    await demo.click(dialog.getByRole("radio", { name: "Let kora choose" }));
    await demo.scrollTo(dialog.getByText("Candidate agents", { exact: true }));
    // The card toggles its candidate. Its checkbox toggles too, so a click on
    // the box itself cancels out (#654); a person clicks the agent's name.
    const search = dialog.getByPlaceholder(/^Search agents/);
    for (const [query, names] of Object.entries(CANDIDATES)) {
      await demo.type(search, query);
      for (const name of names) await demo.click(dialog.getByText(name, { exact: true }));
    }
    await demo.caption("A decision model reads each comment and picks the agent");
    await demo.click(dialog.getByRole("row", { name: "Use Jev 1.14" }));
    await demo.hover(dialog.getByRole("radio", { name: "Balanced (default)" }));
    await demo.select(dialog.getByRole("combobox", { name: "Fallback agent" }), "Coder");
    await demo.hover(dialog.getByRole("switch", { name: /^Join open runs/ }));
    await demo.pause(800);

    await step("Delivery");
    await demo.caption("Cap parallel runs, and pause a trigger that keeps failing");
    await demo.hover(dialog.getByRole("button", { name: "More parallel runs" }));
    await demo.hover(dialog.getByRole("switch", { name: /^Auto-pause on repeated failure/ }));
    await demo.pause(800);

    await step("Review");
    await demo.caption("Review, then create");
    await demo.pause(2500);
    await demo.click(dialog.getByRole("button", { name: "Create trigger" }));
    await dialog.getByRole("heading", { name: "Trigger created" }).waitFor({ timeout: 30_000 });
    await demo.caption();
    await demo.pause(2500);
    await demo.click(dialog.getByRole("button", { name: "Done" }));
    await page.getByRole("row", { name: /Kora Agent \(mentioned\)/ }).waitFor();
    await demo.pause(2500);
  },
);

// Deletes the trigger this walkthrough creates. The agents, source and
// decision models it uses come from seed/seed.mjs.
async function reset(api) {
  const res = await api.get("/api/triggers");
  if (!res.ok()) throw new Error(`GET /api/triggers: ${res.status()}`);
  const triggers = (await res.json()).data ?? [];
  for (const trigger of triggers) {
    if (trigger.name === TRIGGER) await api.delete(`/api/triggers/${trigger.id}`);
  }
  const sources = (await (await api.get("/api/trigger-sources")).json()).data ?? [];
  if (!sources.some((s) => s.name === SOURCE)) throw new Error("the demo seed is missing; run node seed/seed.mjs");
}
