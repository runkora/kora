// kora at a glance, for the README: an issue is assigned to kora-agent on
// GitHub, kora's trigger fires, the Issue Fixer's run starts, and the run is
// followed to the pull request it opens.
//
// Every part is real. The issue is runkora/kora#654, shown as it was before
// kora-agent picked it up (lib/github.mjs). The run is production's run on that
// issue, replayed from seed/runs/issue-654.json. A run's events reach an open
// page from the agent's own pod, so the run is fast-forwarded by writing more
// of it and reloading the page, with each reload cut from the video.
import { replayIssue } from "../lib/github.mjs";
import { feature } from "../lib/feature.mjs";
import { removeRun, replayRun } from "../seed/seed.mjs";

const REPO = "runkora/kora";
const ISSUE = 654;
const FIXTURE = "issue-654";
// kora-agent as GitHub's issue data describes it.
const KORA_AGENT = {
  __typename: "User",
  __isActor: "User",
  __isNode: "User",
  id: "U_kgDOE7h51w",
  login: "kora-agent",
  name: "kora-agent",
  profileResourcePath: "/kora-agent",
  avatarUrl: "https://avatars.githubusercontent.com/u/330856919?s=64&u=037f9a243fb338f0200e053f2de83a231eee906b&v=4",
};
const RUN = { agent: "issue-fixer", trigger: "Kora Issue Fixer" };
// How far into the fixture's 44 events each fast-forward step reaches: the
// sandbox starting, the agent reading the issue and code, installing and
// testing, the denied GraphQL call, and the finished run.
const STARTING = 4;
const STEPS = [10, 16, 25, 34];
const BLOCKED = 37;

export default feature(
  {
    name: "issue-to-pr",
    title: "From an assigned issue to a pull request, governed by kora",
    start: `https://github.com/${REPO}/issues/${ISSUE}`,
    github: true,
    gif: true,
    prepare: (context) => replayIssue(context, { repo: REPO, number: ISSUE, assignee: KORA_AGENT }),
    reset: async () => removeRun(FIXTURE),
  },
  async (demo) => {
    const { page } = demo;
    let runId;
    // Writes the run up to `upTo` events and shows it, out of the video.
    const advance = (upTo) =>
      demo.cut(async () => {
        runId = replayRun(FIXTURE, { ...RUN, upTo });
        await page.reload();
        await page.getByText("Issue Fixer", { exact: true }).first().waitFor();
        await page.waitForLoadState("networkidle");
        // A reloaded run opens at its prompt; a person watching it follows
        // the newest step.
        await page.evaluate(() => {
          for (const el of document.querySelectorAll("*")) {
            if (el.scrollHeight > el.clientHeight + 40 && /(auto|scroll)/.test(getComputedStyle(el).overflowY)) el.scrollTop = el.scrollHeight;
          }
        });
        await page.waitForTimeout(300);
      });

    await demo.caption("Assign an issue to kora-agent");
    await demo.pause(1000);
    const edit = page.getByRole("button", { name: /Edit Assignees/i });
    await demo.zoom(edit, { scale: 1.5, ease: 900 });
    await demo.click(edit);
    await demo.keys("kora");
    // The picker filters as it is typed in; the click waits for the list to
    // narrow to kora-agent, or it can land on an option that is moving.
    await page.waitForFunction(() => document.querySelectorAll('[role="option"]').length === 1);
    await demo.pause(300);
    await demo.click(page.getByRole("option", { name: /kora-agent/ }));
    await page.keyboard.press("Escape");
    await demo.pause(1000);

    // GitHub sends the assignment to kora's webhook, and the trigger starts
    // the run.
    await demo.cut(async () => {
      await demo.caption();
      runId = replayRun(FIXTURE, { ...RUN, upTo: STARTING });
      await page.goto("/triggers");
      await page.getByRole("row", { name: "Open Kora Issue Fixer" }).waitFor();
      await page.waitForLoadState("networkidle");
      await demo.zoom(null, { ease: 0 });
    });
    await demo.caption("kora's trigger fires on the assignment");
    await demo.click(page.getByRole("row", { name: "Open Kora Issue Fixer" }));
    const drawer = page.getByRole("dialog");
    await demo.hover(drawer.getByText("kora-agent", { exact: false }).first());
    await demo.pause(1200);
    await demo.click(drawer.getByRole("tab", { name: "Deliveries" }));
    await demo.hover(drawer.getByText("dispatched").first());
    await demo.pause(1200);

    await demo.caption("The Issue Fixer's run starts");
    await page.keyboard.press("Escape");
    await demo.click(page.getByRole("link", { name: "Agents", exact: true }));
    // The Agents page lists the runs in progress.
    const running = page.getByRole("row").filter({ hasText: runId });
    await demo.hover(running.getByText("Running"));
    await demo.pause(1000);
    await demo.click(running.getByRole("button", { name: "Open" }).or(running.getByRole("link", { name: "Open" })));
    await page.getByText(/^Starting sandbox/).first().waitFor();
    await demo.pause(1800);

    await demo.caption("Fast-forward: the agent works in its sandbox");
    for (const upTo of STEPS) {
      await advance(upTo);
      await demo.pause(900);
    }

    await advance(BLOCKED);
    await demo.caption("Every request it makes is checked against policy");
    await demo.hover(page.getByText("Blocked by policy"));
    await demo.pause(2000);

    await advance(undefined);
    await demo.caption("It opens the pull request and explains the fix");
    await demo.zoom(page.getByText(/^I opened PR #655/), { scale: 1.4, ease: 900 });
    await demo.pause(3000);
    await demo.caption();
    await demo.zoom(null, { ease: 900 });
  },
);
