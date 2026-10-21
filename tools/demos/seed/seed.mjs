// Seeds the platform the walkthroughs record against: the agents, webhook
// sources and triggers kora's own repository runs on, the decision models the
// routing step offers, and two weeks of trigger deliveries and runs so the
// triggers dashboard reads like a platform in use.
//
//   node seed/seed.mjs          seed, replacing an earlier seed's history
//   node seed/seed.mjs clean    remove everything the seed created
//
// Objects go through the API. Delivery and run history has no API, so it is
// written to the local compose Postgres. History rows are found by their
// seeded trigger's name, and clean deletes exactly those rows. Hold the cluster lock while
// seeding: the rows change totals for everyone on the cluster.
//
// Run `kubectl apply -f seed/decision-models.yaml` once first; the decision
// models' virtual models route to that provider.
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { signIn } from "../lib/session.mjs";

const dir = path.dirname(fileURLToPath(import.meta.url));
const config = {
  baseURL: process.env.KORA_URL ?? "https://orbit.local.clearroute.io",
  username: process.env.KORA_USERNAME ?? "dev@orbit.local.clearroute.io",
  password: process.env.KORA_PASSWORD ?? "password",
  stateFile: path.join(dir, "..", ".auth", "state.json"),
};
const DB_CONTAINER = process.env.ORBIT_DB_CONTAINER ?? "orbit-postgres";
const DAYS = 14;
const REPO = "runkora/kora";

// Production descriptions. The routing candidates come last so the target
// step's first page lists them.
export const AGENTS = [
  { slug: "change-mapper", name: "Change Mapper", description: "Draws Mermaid diagrams of a pull request's change and keeps one Visual Walkthrough comment on the pull request up to date." },
  { slug: "dependabot-fixer", name: "Dependabot Fixer", description: "Fixes a Dependabot update pull request whose CI fails by pushing a code fix to its branch, or explains on the pull request why it should not." },
  { slug: "docs-drift", name: "Docs Drift Fix", description: "Finds documentation that commits merged to the default branch made wrong and fixes it in one docs pull request on a schedule." },
  { slug: "kora-run-auditor", name: "Platform Auditor", description: "Audits the last 24 hours of agent runs and trigger deliveries for platform failures, files deduplicated issues per cause and posts a daily report to #kora-dev." },
  { slug: "sre-triage", name: "SRE Triage", description: "Investigates platform alerts and keeps one incident issue per alert group in the infrastructure repository." },
  { slug: "issue-fixer", name: "Issue Fixer", description: "Fixes an issue assigned to kora-agent and opens a pull request, or explains why it should not." },
  { slug: "issue-verifier", name: "Issue Verifier", description: "Tests whether an issue labelled needs-verification is a real defect, then comments and labels the verdict." },
  { slug: "coder", name: "Coder", description: "A general purpose agent for all things coding." },
  { slug: "code-reviewer", name: "Code Reviewer", description: "Reviews a pull request for correctness, security and test coverage, and leaves one review with inline comments on the lines that need a change." },
];

const SOURCES = [
  { name: "runkora-github", provider: "github" },
  { name: "runkora-prod-alertmanager", provider: "alertmanager" },
];

// Decision models served by the provider in decision-models.yaml. The pricing
// sync has no entry for Jev 1.14 or Perplexity Decider V1.2, so their
// production input prices are set as overrides. The gateway's price cache
// picks overrides up on its next refresh, hourly by default, or when the
// inference gateway restarts.
const DECISION_MODELS = [
  { family: "cloudflare", name: "clef", display_name: "Clef", model_id: "cloudflare/clef" },
  { family: "typesafe", name: "jev-1-14", display_name: "Jev 1.14", model_id: "typesafe/jev-1-14", input_cost_per_1m: 0.042 },
  { family: "perplexity", name: "pplx-decider-v1-2", display_name: "Perplexity Decider V1.2", model_id: "perplexity/pplx-decider-v1-2", input_cost_per_1m: 0.02 },
];

// The engineering team whose day-to-day Claude Code and Codex traffic goes
// through the gateway. rate is requests on a working day.
const ENGINEERS = [
  ["Priya Raman", 420], ["Marcus Chen", 380], ["Sofia Alvarez", 350], ["James Okafor", 330],
  ["Aisha Bello", 310], ["Tom Nguyen", 300], ["Hannah Schmidt", 290], ["Daniel Kim", 280],
  ["Fatima Haddad", 270], ["Lucas Ferreira", 260], ["Emily Watson", 250], ["Raj Patel", 240],
  ["Chloe Martin", 220], ["Oliver Brooks", 210], ["Mei Lin", 200], ["Ethan Walker", 180],
  ["Zara Ahmed", 160], ["Noah Fischer", 140], ["Grace Liu", 120], ["Sam Taylor", 90],
].map(([name, rate]) => {
  const login = name.toLowerCase().replace(" ", ".");
  // The subject Dex issues for an LDAP user: its login and connector, encoded.
  const raw = Buffer.concat([Buffer.from([0x0a, login.length]), Buffer.from(login), Buffer.from([0x12, 10]), Buffer.from("local-ldap")]);
  return { name, rate, id: raw.toString("base64url"), email: `${login.replace(".", "")}@runkora.io` };
});

const PR_FIELDS = `Pull request: #{{ pull_request.number }} in {{ repository.full_name }}
Event: {{ action }}
Title: {{ pull_request.title }}
Author: {{ pull_request.user.login }}
Branch: {{ pull_request.head.ref }} → {{ pull_request.base.ref }}
Head commit: {{ pull_request.head.sha }}
Size: {{ pull_request.changed_files }} files, {{ pull_request.additions }} additions, {{ pull_request.deletions }} deletions
Link: {{ pull_request.html_url }}`;

const OPENED = { action: ["opened", "reopened", "ready_for_review"], "pull_request.draft": ["false"], "repository.full_name": [REPO], "sender.type": ["User"] };

// Production triggers. perDay is how often each fires in the seeded history.
const TRIGGERS = [
  {
    name: "Kora Review (auto)", agent: "code-reviewer", source: "runkora-github", event_type: "pull_request", perDay: 6,
    match_condition: OPENED,
    prompt_template: `Review this pull request.\n\n${PR_FIELDS}`,
  },
  {
    name: "Kora Review (assigned)", agent: "code-reviewer", source: "runkora-github", event_type: "pull_request", perDay: 2,
    match_condition: { action: ["review_requested"], "repository.full_name": [REPO], "requested_reviewer.login": ["kora-agent"] },
    prompt_template: `Re-review this pull request. {{ sender.login }} requested a review from kora-agent.\n\n${PR_FIELDS}`,
  },
  {
    name: "Kora Change Mapper (opened)", agent: "change-mapper", source: "runkora-github", event_type: "pull_request", perDay: 5,
    match_condition: { ...OPENED, action: ["opened", "ready_for_review"] },
    prompt_template: `Draw the Visual Walkthrough for this pull request.\n\n${PR_FIELDS}`,
  },
  {
    name: "Kora Change Mapper (synchronize)", agent: "change-mapper", source: "runkora-github", event_type: "pull_request", perDay: 11,
    match_condition: { ...OPENED, action: ["synchronize"] },
    prompt_template: "Update the Visual Walkthrough for this pull request after new commits.\n\nPull request: #{{ pull_request.number }} in {{ repository.full_name }}\nPrevious head: {{ before }}\nHead commit: {{ after }}\nLink: {{ pull_request.html_url }}",
  },
  {
    name: "Kora Issue Fixer", agent: "issue-fixer", source: "runkora-github", event_type: "issues", perDay: 1.5,
    match_condition: { action: ["assigned"], "assignee.login": ["kora-agent"], "repository.full_name": [REPO] },
    prompt_template: "Fix this issue. {{ sender.login }} assigned it to kora-agent.\n\nRepository: {{ repository.full_name }}\nIssue: #{{ issue.number }}\nTitle: {{ issue.title }}\nLabels: {{ issue.labels }}\nLink: {{ issue.html_url }}\nBody: {{ issue.body }}",
  },
  {
    name: "Kora Issue Verifier", agent: "issue-verifier", source: "runkora-github", event_type: "issues", perDay: 1,
    match_condition: { action: ["labeled"], "label.name": ["needs-verification"], "repository.full_name": [REPO] },
    prompt_template: "Verify this issue. {{ sender.login }} labelled it needs-verification.\n\nRepository: {{ repository.full_name }}\nIssue: #{{ issue.number }}\nTitle: {{ issue.title }}\nLink: {{ issue.html_url }}\nBody: {{ issue.body }}",
  },
  {
    name: "Kora Dependabot Fixer (CI failed)", agent: "dependabot-fixer", source: "runkora-github", event_type: "workflow_run", perDay: 0.7,
    match_condition: { action: ["completed"], "repository.full_name": [REPO], "workflow_run.actor.login": ["dependabot[bot]"], "workflow_run.conclusion": ["failure"], "workflow_run.event": ["pull_request"] },
    coalesce: { key: "{{ repository.full_name }}@{{ workflow_run.head_sha }}", window_seconds: 300 },
    prompt_template: "CI failed on a Dependabot pull request. Fix it or explain why not.\n\nRepository: {{ repository.full_name }}\nPull request: #{{ workflow_run.pull_requests.0.number }}\nBranch: {{ workflow_run.head_branch }}\nHead commit: {{ workflow_run.head_sha }}\nFailed workflow: {{ workflow_run.name }}\nRun: {{ workflow_run.html_url }}",
  },
  {
    name: "SRE Triage (firing)", agent: "sre-triage", source: "runkora-prod-alertmanager", event_type: "alert.firing", perDay: 0.8, max_parallel: 1,
    match_condition: {},
    prompt_template: "Alertmanager reported an alert group firing.\n\nEvent: alert.firing\nGroup labels: {{ groupLabels }}\nCommon labels: {{ commonLabels }}\nCommon annotations: {{ commonAnnotations }}\nAlerts: {{ alerts }}\nAlertmanager: {{ externalURL }}",
  },
  {
    name: "Kora Docs Drift (scheduled)", agent: "docs-drift", kind: "scheduled", cron: { hour: 9, days: [1, 3, 5] }, overflow: "replace",
    schedule_config: { cron_expression: "0 9 * * 1,3,5", timezone: "Australia/Sydney", catch_up: false },
    prompt_template: "The scheduled docs drift check is due.\nRepository: runkora/kora\nSchedule: 0 9 * * 1,3,5 Australia/Sydney",
  },
  {
    name: "Kora Platform Auditor (daily)", agent: "kora-run-auditor", kind: "scheduled", cron: { hour: 22 }, overflow: "replace",
    schedule_config: { cron_expression: "0 22 * * *", timezone: "Australia/Sydney", catch_up: false },
    prompt_template: "The daily audit is due. Run the daily audit.",
  },
];

async function main() {
  const command = process.argv[2] ?? "seed";
  const browser = await chromium.launch();
  try {
    await signIn(browser, config);
    const context = await browser.newContext({ baseURL: config.baseURL, ignoreHTTPSErrors: true, storageState: config.stateFile });
    const api = client(context.request);
    if (command === "clean") await clean(api);
    else if (command === "seed") await seed(api);
    else throw new Error(`unknown command ${command}; use seed or clean`);
  } finally {
    await browser.close();
  }
}

async function seed(api) {
  const tenant = sql("SELECT DISTINCT tenant_id FROM agent_types LIMIT 1").trim() || "example";

  // Triggers are recreated so their ids, and the history pointing at them,
  // always match this file. They go first: an agent with triggers cannot be
  // replaced.
  for (const t of await api.list("/api/triggers")) {
    if (TRIGGERS.some((s) => s.name === t.name)) await api.send("DELETE", `/api/triggers/${t.id}`);
  }

  const agents = new Map((await api.list("/api/agents/types")).map((a) => [a.slug, a]));
  for (const agent of AGENTS) {
    const current = agents.get(agent.slug);
    if (current && current.description === agent.description) continue;
    if (current) await api.send("DELETE", `/api/agents/types/${current.id}`);
    const created = await api.send("POST", "/api/agents/types", {
      ...agent,
      framework: "claude-code",
      model_id: "anthropic/claude-sonnet-5",
      system_prompt: agent.description,
    });
    agents.set(agent.slug, (await created.json()).data);
  }

  const sources = new Map((await api.list("/api/trigger-sources")).map((s) => [s.name, s]));
  for (const source of SOURCES) {
    if (sources.has(source.name)) continue;
    const created = await api.send("POST", "/api/trigger-sources", source);
    const body = await created.json();
    sources.set(source.name, body.data?.source ?? body.data ?? body);
  }

  // A model whose every route points at a provider that is not registered,
  // such as a local Ollama, can never serve a call.
  const providers = new Set((await api.list("/api/catalog/provider-models")).map((m) => m.provider));
  for (const m of await api.list("/api/catalog/virtual-models")) {
    if (!m.routes.some((r) => providers.has(r.provider))) await api.send("DELETE", `/api/catalog/virtual-models/${m.id}`);
  }

  const models = new Set((await api.list("/api/catalog/virtual-models")).map((m) => m.name));
  for (const { model_id, input_cost_per_1m, ...model } of DECISION_MODELS) {
    if (input_cost_per_1m !== undefined) {
      await api.send("PUT", `/api/catalog/pricing/compatible:decision-models/${encodeURIComponent(model_id)}/override`, {
        input_cost_per_1m,
        output_cost_per_1m: 0,
      });
    }
    if (models.has(model.name)) continue;
    await api.send("POST", "/api/catalog/virtual-models", {
      ...model,
      strategy: "priority",
      enabled: true,
      routes: [{ provider: "compatible:decision-models", model_id, priority: 1, weight: 100, enabled: true }],
    });
  }

  const triggers = [];
  for (const t of TRIGGERS) {
    const { agent, source, perDay, cron, kind = "webhook", ...fields } = t;
    const created = await api.send("POST", "/api/triggers", {
      kind,
      target_kind: "agent",
      agent_type_id: agents.get(agent).id,
      ...(kind === "webhook" && { source_id: sources.get(source).id }),
      match_condition: {},
      max_parallel: 4,
      overflow: "wait",
      dedupe_enabled: true,
      autopause_enabled: true,
      ...fields,
    });
    const row = (await created.json()).data;
    triggers.push({ ...t, kind, id: row.id, agentId: agents.get(agent).id, sourceId: kind === "webhook" ? sources.get(source).id : null });
  }

  sql(historySql(tenant, triggers));
  // The account that records the walkthroughs works like the rest of the team.
  // The gateway overview's recent requests list only the viewer's own calls.
  const viewer = sql(`SELECT id FROM user_profiles WHERE email = '${config.username.replace(/'/g, "''")}' LIMIT 1`).trim();
  sql(teamSql(tenant, viewer));
  const counts = sql(`SELECT count(*) FROM trigger_deliveries WHERE trigger_name IN (${seededNames})`).trim();
  console.log(`seeded ${AGENTS.length} agents, ${triggers.length} triggers, ${counts} deliveries over ${DAYS} days`);
}

async function clean(api) {
  for (const t of await api.list("/api/triggers")) {
    if (TRIGGERS.some((s) => s.name === t.name)) await api.send("DELETE", `/api/triggers/${t.id}`);
  }
  sql(deleteHistory);
  sql(`DELETE FROM inference_usage WHERE principal_id IN (${engineerIds});
DELETE FROM user_profiles WHERE id IN (${engineerIds});`);
  for (const s of await api.list("/api/trigger-sources")) {
    if (SOURCES.some((x) => x.name === s.name)) await api.send("DELETE", `/api/trigger-sources/${s.id}`);
  }
  for (const a of await api.list("/api/agents/types")) {
    if (AGENTS.some((x) => x.slug === a.slug)) await api.send("DELETE", `/api/agents/types/${a.id}`);
  }
  console.log("cleaned the demo seed");
}

const seededNames = TRIGGERS.map((t) => `'${t.name.replace(/'/g, "''")}'`).join(", ");
const seededRuns = `SELECT id FROM runs WHERE delivery_id IN (SELECT id FROM trigger_deliveries WHERE trigger_name IN (${seededNames}))`;
const deleteHistory = `
DELETE FROM inference_usage WHERE run_id IN (${seededRuns});
DELETE FROM runs WHERE id IN (${seededRuns});
DELETE FROM trigger_deliveries WHERE trigger_name IN (${seededNames});`;

// Two weeks of history: deliveries spread through working hours, most starting a
// run, a few failing or denied, with dispatch latency of one to three seconds.
// Each run makes a handful of model calls at the catalog's Claude Sonnet 5
// prices. Deliveries and agent usage whose trigger or agent no longer exists
// are dropped, so they do not count towards the dashboards' totals.
function historySql(tenant, triggers) {
  const random = seeded(11);
  const now = Date.now();
  const deliveries = [];
  const deliveryRuns = [];
  const runs = [];
  const usage = [];
  let n = 0;
  const q = (v) => (v === null ? "NULL" : `'${String(v).replace(/'/g, "''")}'`);
  const ts = (ms) => q(new Date(ms).toISOString());

  for (const t of triggers) {
    for (const at of firings(t, now, random)) {
      n++;
      const id = crypto.randomUUID();
      const roll = random();
      const outcome = roll < 0.03 ? "failed" : roll < 0.05 ? "denied" : "dispatched";
      const latency = 600 + Math.exp(random() * 1.6) * 400;
      const runId = outcome === "dispatched" ? crypto.randomUUID() : null;
      deliveries.push(
        `(${q(id)}, ${q(tenant)}, ${q(t.sourceId)}, ${q(t.kind === "webhook" ? crypto.randomUUID() : "")}, ${q(t.event_type ?? "")}, ` +
          `${q(outcome === "dispatched" ? "dispatched" : "failed")}, ${q(t.id)}, ${q(runId)}, ${ts(at)}, ${q(t.kind)}, ` +
          `${t.kind === "scheduled" ? ts(at) : "NULL"}, ${q(t.name)})`,
      );
      deliveryRuns.push(
        `(${q(crypto.randomUUID())}, ${q(tenant)}, ${q(id)}, ${q(t.id)}, ${q(runId)}, ${q(outcome)}, ` +
          `${q(outcome === "failed" ? "agent API unavailable" : outcome === "denied" ? "the agent's grants do not allow this trigger" : null)}, ` +
          `${ts(at + latency)}, ${q(t.name)})`,
      );
      if (runId) {
        const started = at + latency;
        const minutes = 2 + Math.exp(random() * 2.4);
        const done = Math.min(now, started + minutes * 60_000);
        const state = done === now ? "running" : random() < 0.92 ? "completed" : "failed";
        runs.push(
          `(${q(runId)}, ${q(tenant)}, ${q(t.agentId)}, ${q(state)}, 'trigger', ${ts(started)}, ` +
            `${state === "running" ? "NULL" : ts(done)}, ${ts(started)}, ${ts(done)}, ${q(t.id)}, ${q(t.name)}, ${q(id)})`,
        );
        const calls = 15 + Math.floor(random() * 31);
        for (let i = 0; i < calls; i++) {
          const prompt = Math.round(6000 + random() * 30000);
          const cached = Math.round(prompt * (1 + random() * 3));
          const completion = Math.round(300 + random() * 2200);
          const cost = (prompt * 2.2 + completion * 11 + cached * 0.22) / 1e6;
          usage.push(
            `(${q(crypto.randomUUID())}, ${q(tenant)}, ${q(`spiffe://orbit.test/tenant/orbit-tenant-${tenant}/agent/${t.agent}`)}, 'AGENT', ` +
              `'anthropic', 'bedrock:us', 'anthropic/claude-sonnet-5', 'anthropic', ${prompt}, ${completion}, ${prompt + completion}, ` +
              `${cached}, ${cost.toFixed(8)}, 'success', ${Math.round(2000 + random() * 12000)}, ` +
              `${ts(started + ((done - started) * (i + 0.5)) / calls)}, ${q(runId)}, ${q(t.agent)})`,
          );
        }
      }
    }
  }

  return `BEGIN;
${deleteHistory}
DELETE FROM trigger_deliveries d WHERE d.trigger_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM triggers t WHERE t.id = d.trigger_id);
INSERT INTO trigger_deliveries (id, tenant_id, source_id, provider_delivery_id, event_type, status, trigger_id, run_id, received_at, kind, scheduled_at, trigger_name)
VALUES ${deliveries.join(",\n")};
INSERT INTO trigger_delivery_runs (id, tenant_id, delivery_id, trigger_id, run_id, status, error, created_at, trigger_name)
VALUES ${deliveryRuns.join(",\n")};
INSERT INTO runs (id, tenant_id, agent_type_id, state, created_by, started_at, completed_at, created_at, updated_at, trigger_id, trigger_name, delivery_id)
VALUES ${runs.join(",\n")};
DELETE FROM inference_usage u WHERE u.principal_type = 'AGENT' AND NOT EXISTS (SELECT 1 FROM agent_types a WHERE a.slug = u.agent_slug);
INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, api_dialect, provider, model_id, model_family, prompt_tokens,
  completion_tokens, total_tokens, cache_read_input_tokens, estimated_cost_usd, status, latency_ms, created_at, run_id, agent_slug)
VALUES ${usage.join(",\n")};
COMMIT;`;
}

const engineerIds = ENGINEERS.map((e) => `'${e.id}'`).join(", ");

// The team's own usage: each engineer's working-day requests in Sydney hours,
// a few at weekends and a handful in the last hour, 70% Claude Code on Claude Sonnet 5 and 30% Codex on
// GPT-5.5, mostly served from the prompt cache, with the odd throttled call.
// Usage that is neither the team's nor a seeded run's is removed, so the
// gateway's totals describe this team alone. setseed keeps it repeatable.
function teamSql(tenant, viewer) {
  const users = [...ENGINEERS, ...(viewer ? [{ id: viewer, rate: 280 }] : [])]
    .map((e) => `('${e.id}', ${e.rate})`)
    .join(", ");
  const profiles = ENGINEERS.map(
    (e) => `('${e.id}', '${tenant}', '${e.email}', '${e.name.replace(/'/g, "''")}', ARRAY['users','engineer'])`,
  ).join(",\n");
  return `BEGIN;
DELETE FROM inference_usage WHERE run_id IS NULL OR run_id NOT IN (${seededRuns});
INSERT INTO user_profiles (id, tenant_id, email, display_name, roles) VALUES ${profiles}
ON CONFLICT (id, tenant_id) DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name, roles = EXCLUDED.roles;
SELECT setseed(0.42);
INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, api_dialect, provider, model_id, model_family,
  prompt_tokens, completion_tokens, total_tokens, cache_read_input_tokens, estimated_cost_usd, status, error_code, latency_ms, created_at)
SELECT gen_random_uuid()::text, '${tenant}', id, 'HUMAN',
  CASE WHEN claude THEN 'anthropic' ELSE 'responses' END, 'bedrock:us',
  CASE WHEN claude THEN 'anthropic/claude-sonnet-5' ELSE 'openai/gpt-5-5' END,
  CASE WHEN claude THEN 'anthropic' ELSE 'openai' END,
  CASE WHEN failed THEN 0 ELSE p END, CASE WHEN failed THEN 0 ELSE c END, CASE WHEN failed THEN 0 ELSE p + c END,
  CASE WHEN failed THEN 0 ELSE cr END,
  CASE WHEN failed THEN 0 WHEN claude THEN (p * 2.2 + c * 11 + cr * 0.22) / 1e6 ELSE (p * 5.5 + c * 33 + cr * 0.55) / 1e6 END,
  CASE WHEN failed THEN 'provider_error' ELSE 'success' END, CASE WHEN failed THEN 'THROTTLED' END,
  latency, at
FROM (
  SELECT u.id, random() < 0.7 AS claude, random() < 0.003 AS failed,
    (1500 + random() * 9000)::int AS p, (150 + random() * 2200)::int AS c, (15000 + random() * 90000)::int AS cr,
    (800 + random() * 5200)::int AS latency,
    ((day + interval '8 hours' + random() * interval '10 hours') AT TIME ZONE 'Australia/Sydney') AS at
  FROM (VALUES ${users}) u(id, rate)
  CROSS JOIN LATERAL (
    SELECT date_trunc('day', now() AT TIME ZONE 'Australia/Sydney') - d * interval '1 day' AS day
    FROM generate_series(0, ${DAYS - 1}) d
  ) days
  CROSS JOIN LATERAL generate_series(1, CASE WHEN extract(isodow FROM day) < 6 THEN u.rate ELSE u.rate / 15 END) n
  UNION ALL
  -- A few people are always working, so the last hour is never empty.
  SELECT u.id, random() < 0.7, false, (1500 + random() * 9000)::int, (150 + random() * 2200)::int,
    (15000 + random() * 90000)::int, (800 + random() * 5200)::int, now() - random() * interval '1 hour'
  FROM (VALUES ${users}) u(id, rate)
  CROSS JOIN generate_series(1, 3) n
  WHERE u.rate > 250
) r
WHERE at < now();
COMMIT;`;
}

// When a trigger fired over the last DAYS days. Webhooks cluster in Sydney
// working hours on weekdays; schedules fire on their cron.
function firings(t, now, random) {
  const out = [];
  const sydney = 11 * 60 * 60 * 1000;
  for (let day = DAYS; day >= 0; day--) {
    const midnight = Math.floor((now + sydney) / 86_400_000 - day) * 86_400_000 - sydney;
    const weekday = new Date(midnight + sydney).getUTCDay();
    if (t.cron) {
      if (t.cron.days && !t.cron.days.includes(weekday)) continue;
      const at = midnight + t.cron.hour * 3_600_000 + random() * 2000;
      if (at < now) out.push(at);
      continue;
    }
    const weekend = weekday === 0 || weekday === 6;
    const expected = t.perDay * (weekend ? 0.25 : 1.3);
    let count = Math.floor(expected) + (random() < expected % 1 ? 1 : 0);
    while (count-- > 0) {
      const hour = t.event_type?.startsWith("alert") ? random() * 24 : 8 + random() * 10;
      const at = midnight + hour * 3_600_000;
      if (at < now) out.push(at);
    }
  }
  return out;
}

function client(request) {
  return {
    async list(url) {
      const res = await request.get(url);
      if (!res.ok()) throw new Error(`GET ${url}: ${res.status()}`);
      const body = await res.json();
      return body.data ?? body;
    },
    async send(method, url, data) {
      const res = await request.fetch(url, { method, data });
      if (!res.ok() && res.status() !== 404) throw new Error(`${method} ${url}: ${res.status()} ${await res.text()}`);
      return res;
    },
  };
}

function sql(text) {
  const res = spawnSync("docker", ["exec", "-i", DB_CONTAINER, "psql", "-v", "ON_ERROR_STOP=1", "-qAt", "-U", "orbit", "-d", "orbit"], {
    input: text,
    encoding: "utf8",
  });
  if (res.status !== 0) throw new Error(`psql failed: ${res.stderr}`);
  return res.stdout;
}

function seeded(seed) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch((err) => {
    console.error(err.message);
    process.exit(1);
  });
}
