// Shows a real GitHub issue as it was before an agent picked it up: open,
// unassigned and with no linked pull request. GitHub draws the issue from data
// it sends with the page and fetches after it, so that data is rewritten in
// the browser. Nothing on GitHub changes:
//
// - Every request to GitHub that is not a GET is aborted before it leaves the
//   browser.
// - The one write a walkthrough makes, assigning the issue, is answered in the
//   browser with the response GitHub gives for it, so the page shows the
//   assignee it would show.

/**
 * @param {import("@playwright/test").BrowserContext} context
 * @param {{ repo: string, number: number, assignee: object }} issue
 *   assignee is the GitHub actor the walkthrough assigns, as GitHub's issue
 *   data describes it.
 */
export async function replayIssue(context, { repo, number, assignee }) {
  const page = `https://github.com/${repo}/issues/${number}`;

  // Routes match in reverse order of registration, so the guard is checked
  // after the specific routes below.
  await guard(context);

  await context.route(`${page}/assigned_actors.json*`, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        data: { replaceActorsForAssignable: { assignable: { __typename: "Issue", assignedActors: { nodes: [assignee] } } } },
      }),
    }),
  );

  // Data fetched after the page loads, such as the sidebar's linked pull
  // requests.
  await context.route(new RegExp(`^(https://github\\.com/_graphql|${escape(page)}/secondary_view)`), async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const res = await route.fetch();
    const text = await res.text();
    let body = text;
    try {
      const data = JSON.parse(text);
      asNew(data);
      body = JSON.stringify(data);
    } catch {
      // Not JSON; served as it came.
    }
    await route.fulfill({ response: res, body });
  });

  await context.route(page, async (route) => {
    const res = await route.fetch();
    const html = (await res.text()).replace(
      /(<script type="application\/json" data-target="react-app\.embeddedData">)([\s\S]*?)(<\/script>)/g,
      (_, open, json, close) => {
        const data = JSON.parse(json);
        asNew(data);
        return open + JSON.stringify(data).replace(/</g, "\\u003c") + close;
      },
    );
    await route.fulfill({ response: res, body: html });
  });
}

/**
 * Shows a real pull request as it was when a comment was about to be posted:
 * open, with its conversation up to that comment. The page is GitHub's own,
 * with the later timeline hidden as it loads. Writes are aborted, as for
 * replayIssue.
 *
 * Returns reveal(id), which shows a hidden comment, such as the one the
 * walkthrough types and the reply to it, from the page's next load on.
 *
 * @param {import("@playwright/test").BrowserContext} context
 * @param {{ repo: string, number: number, from: string }} pull
 *   from is the element id of the first comment to hide, e.g.
 *   "issuecomment-6287995418".
 */
export async function stagePullRequest(context, { repo, number, from }) {
  await guard(context);
  const path = `/${repo}/pull/${number}`;
  // Posting a comment is held without an answer, so GitHub shows it posting
  // rather than failing; the walkthrough cuts away and reloads before the
  // request is dropped.
  await context.route(new RegExp(`^https://github\\.com${path}/comment(\\?|$)`), (route) => {
    setTimeout(() => route.abort().catch(() => {}), 20_000);
  });
  // The comments to show are set by a script that runs before the page's
  // own, so the page hides the rest before it first paints. Each reveal adds
  // a later script with the longer list.
  const shown = [];
  await context.addInitScript(
    ({ path, from }) => {
      if (location.pathname !== path) return;
      const stage = () => {
        const shown = window.__demoShown ?? [];
        const items = [...document.querySelectorAll(".js-discussion .js-timeline-item")];
        const item = (id) => document.getElementById(id)?.closest(".js-timeline-item");
        const first = items.indexOf(item(from));
        if (first < 0) return;
        const keep = new Set(shown.map(item));
        items.forEach((el, i) => {
          if (i >= first && !keep.has(el)) el.style.display = "none";
        });
      };
      // The pull request reads as open: no merged state and no merge box.
      // GitHub loads the timeline and redraws the header and the merge box
      // after the page loads, so both run again whenever the page changes.
      const open = () => {
        for (const state of document.querySelectorAll(".State--merged, [class*=StateLabel]")) {
          const label = [...state.childNodes].find((n) => n.nodeType === Node.TEXT_NODE && n.textContent.trim() === "Merged");
          if (!label) continue;
          label.textContent = "Open";
          state.classList.replace("State--merged", "State--open");
          state.style.backgroundColor = "var(--bgColor-open-emphasis, #1f883d)";
        }
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          if (/^\s*merged \d+ commits? into\s*$/.test(n.textContent)) n.textContent = n.textContent.replace("merged", "wants to merge");
        }
        for (const el of document.querySelectorAll("[class*=mergeBox], [class*=MergeBox], .merge-pr, #partial-pull-merging")) {
          el.style.display = "none";
        }
      };
      let queued = false;
      new MutationObserver(() => {
        if (queued || !document.body) return;
        queued = true;
        requestAnimationFrame(() => {
          queued = false;
          stage();
          open();
        });
      }).observe(document, { childList: true, subtree: true });
    },
    { path, from },
  );
  return async (id) => {
    shown.push(id);
    await context.addInitScript((ids) => {
      window.__demoShown = ids;
    }, [...shown]);
  };
}

// Aborts every request to GitHub that could change something. GitHub loads
// commit badges and check rollups with POSTs that only read, so those two pass.
const READS = /^https:\/\/github\.com\/(commits\/badges|[^/]+\/[^/]+\/commits\/checks-statuses-rollups)(\?|$)/;
const GITHUB = /^https:\/\/([a-z0-9.-]+\.)?github(usercontent|assets)?\.com\//;
const guarded = new WeakSet();

export async function guard(context) {
  if (guarded.has(context)) return;
  guarded.add(context);
  await context.route(GITHUB, (route) => {
    const req = route.request();
    if (req.method() === "GET" || READS.test(req.url())) return route.fallback();
    return route.abort();
  });
}

// Clears what an issue gains once it is worked: its assignees, its linked
// pull requests and branches, and its closed state.
function asNew(node) {
  if (Array.isArray(node)) return node.forEach(asNew);
  if (!node || typeof node !== "object") return;
  for (const key of ["assignedActors", "assignees", "closedByPullRequestsReferences", "linkedPullRequests", "linkedBranches"]) {
    const list = node[key];
    if (!list || typeof list !== "object") continue;
    if (Array.isArray(list.nodes)) list.nodes = [];
    if (Array.isArray(list.edges)) list.edges = [];
    if ("totalCount" in list) list.totalCount = 0;
  }
  if (node.state === "CLOSED" && "stateReason" in node) {
    node.state = "OPEN";
    node.stateReason = null;
  }
  if (node.closed === true) node.closed = false;
  if ("closedAt" in node) node.closedAt = null;
  Object.values(node).forEach(asNew);
}

function escape(text) {
  return text.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
}
