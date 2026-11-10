// Creating an agent end to end: an identity, its behaviour, the access it may
// use, a spend ceiling, then the agent provisioning. The agent is kora's own
// Vulnerability Fixer from production, without its Slack reporting, which the
// local cluster has no connection for.
import { feature } from "../lib/feature.mjs";

const NAME = "Vulnerability Fixer";
const SLUG = "vulnerability-fixer";
const DESCRIPTION = "Scans a repository daily for known vulnerabilities and opens one pull request per affected manifest.";

// The prompt opens with its first sentences, typed, and continues with the
// rest of the production prompt, pasted.
const INSTRUCTION = "You are a vulnerability fix agent. A schedule starts you for one repository.";
const PROMPT_REST = ` You scan the repository for known vulnerabilities and fix each affected manifest in one pull request. You write only branches and pull requests.

## Input

The run message gives the repository and, optionally, the sandbox environment.

## Tools

*   **Sandbox:** \`git\`, \`gh\` and the repository's toolchain.
*   **Pull requests:** Use \`gh pr list\` and \`gh pr create\`, and read pull request details through \`gh api\`.

## Process

1.  Clone the repository. Read \`AGENTS.md\`, \`CLAUDE.md\` and \`CONTRIBUTING.md\` where they exist, and follow their rules.
2.  **Scan:** From the repository root, run \`osv-scanner scan source -r --format json .\` and write \`osv.json\`. In each directory that holds a \`go.mod\`, run \`govulncheck -format json ./...\`.
3.  **Fix:** Create the branch \`fix/vuln-{manifest-slug}\` from the default branch, fix every finding in the manifest, and open one pull request.`;

// The GitHub actions it is granted, and the hosts its scanners and package
// managers reach, as in production.
const GITHUB_ACTIONS = ["contents:read", "contents:write", "pulls:read", "pulls:write", "checks:read", "actions:read"];
const FIRST_HOST = "api.osv.dev";
const OTHER_HOSTS = [
  "vuln.go.dev",
  "proxy.golang.org",
  "sum.golang.org",
  "storage.googleapis.com",
  "registry.npmjs.org",
  "pypi.org",
  "files.pythonhosted.org",
  "registry-1.docker.io",
  "auth.docker.io",
];

export default feature(
  {
    name: "agent-create",
    title: "Create an agent with its own identity and access",
    start: "/agents",
    reset,
  },
  async (demo) => {
    const { page } = demo;
    const dialog = page.getByRole("dialog");
    // The next step renders at the current scroll offset, so the finished
    // step is scrolled back to its top first.
    const next = async () => {
      await demo.scrollTo(dialog.getByRole("heading", { level: 3 }).first());
      await demo.click(dialog.getByRole("button", { name: "Continue" }));
    };

    await demo.caption("Every agent starts with a verified identity");
    await demo.pause(1500);
    await demo.click(page.getByRole("button", { name: "New Agent" }).first());
    await dialog.waitFor();
    await demo.pause(600);

    await demo.type(dialog.getByPlaceholder("Deployment Assistant"), NAME);
    await demo.type(dialog.getByPlaceholder("What this agent is responsible for."), DESCRIPTION);
    await demo.pause(900);
    await next();

    await demo.caption("Describe its job, then pick the framework and model");
    await demo.type(dialog.getByPlaceholder(/^You are a deployment assistant/), INSTRUCTION);
    await demo.pause(900);
    await demo.paste(PROMPT_REST);
    await demo.pause(900);
    await demo.click(dialog.getByRole("radio", { name: /Claude Code/ }));
    await demo.click(dialog.getByRole("radio", { name: "Claude Sonnet 5" }));
    await demo.pause(700);
    await next();

    await demo.caption("Grant exactly the GitHub actions it needs. Everything else is denied");
    await demo.click(dialog.getByRole("button", { name: "Expand github actions" }), { room: true });
    for (const action of GITHUB_ACTIONS) {
      await demo.click(dialog.getByRole("checkbox", { name: `Grant github ${action}` }));
    }
    await demo.pause(700);

    await demo.caption("It reaches only the vulnerability databases and package registries");
    await demo.click(dialog.getByRole("radio", { name: "Allowlist" }), { room: true });
    await demo.type(dialog.getByRole("textbox", { name: "Allowed hosts" }), FIRST_HOST);
    await demo.pause(600);
    await demo.paste(`\n${OTHER_HOSTS.join("\n")}`);
    await demo.pause(800);
    await demo.hover(dialog.getByText("Effective access", { exact: true }));
    await demo.pause(1500);
    await next();

    await demo.caption("Set the ceilings kora enforces at runtime");
    await demo.click(dialog.getByRole("switch", { name: "Agent budget" }));
    await demo.type(dialog.getByRole("textbox", { name: "Agent budget in USD" }), "150");
    await demo.click(dialog.getByRole("switch", { name: "Run budget" }));
    await demo.type(dialog.getByRole("textbox", { name: "Run budget in USD" }), "10");
    await demo.pause(900);
    await next();

    await demo.caption("Review, then create");
    await demo.pause(2500);
    await demo.click(dialog.getByRole("button", { name: "Create agent" }));
    await dialog.getByText("Agent created").waitFor({ timeout: 30_000 });
    // The summary proves the grants landed, so a click that missed fails the run.
    await dialog.getByText(/MCP tools · 1 connections/).waitFor();
    await dialog.getByText("$150 / monthly").waitFor();
    await demo.caption();
    await demo.pause(3000);
  },
);

async function reset(api) {
  const agent = await api.get(`/api/agents/types/${SLUG}`);
  if (agent.ok()) {
    const { data } = await agent.json();
    await expectOk(api.delete(`/api/agents/types/${data.id}`), `delete agent ${SLUG}`);
  }

  const drafts = await api.get("/api/agents/types/drafts");
  if (!drafts.ok()) return;
  const { data = [] } = await drafts.json();
  for (const draft of data.filter((d) => d.payload?.slug === SLUG || d.payload?.name === NAME)) {
    await expectOk(api.delete(`/api/agents/types/drafts/${draft.id}`), `delete draft ${draft.id}`);
  }
}

async function expectOk(request, what) {
  const res = await request;
  if (!res.ok()) throw new Error(`${what}: HTTP ${res.status()}`);
}
