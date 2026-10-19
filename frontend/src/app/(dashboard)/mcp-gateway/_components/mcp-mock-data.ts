import type { McpCatalogTool, McpServer } from '@runkora/shared/api';

/** Mock MCP servers and tool catalog for the page stories. */

const TENANT = 'ten_7c1e4a90';

const hoursAgo = (h: number): string => new Date(Date.now() - h * 3_600_000).toISOString();

const server = (
  partial: Partial<McpServer> & Pick<McpServer, 'name' | 'display_name' | 'description'>,
): McpServer => ({
  transport: 'streamable-http',
  caller_identity: 'forward',
  endpoint: `https://mcp.internal/${partial.name}`,
  global: true,
  tool_count: 0,
  domains: [],
  last_synced: hoursAgo(1),
  status: 'healthy',
  ...partial,
});

export const GITHUB = server({
  name: 'github',
  display_name: 'GitHub',
  description:
    'Read and change repositories, issues, pull requests and Actions runs as the signed-in user.',
  tool_count: 24,
  domains: ['repos', 'issues', 'pulls', 'actions', 'search'],
  last_synced: hoursAgo(0.2),
});

export const SLACK = server({
  name: 'slack',
  display_name: 'Slack',
  description: 'Post to channels, read threads and look up people in the connected workspace.',
  caller_identity: 'static',
  tool_count: 9,
  domains: ['chat', 'channels', 'users'],
  last_synced: hoursAgo(3),
});

export const LINEAR = server({
  name: 'linear',
  display_name: 'Linear',
  description: 'Create and triage issues, move work between cycles and read project status.',
  tool_count: 11,
  domains: ['issues', 'projects', 'cycles'],
  last_synced: hoursAgo(5),
});

export const POSTGRES = server({
  name: 'postgres-analytics',
  display_name: 'Postgres (analytics)',
  description: 'Read-only SQL against the analytics replica. Writes are rejected by the upstream.',
  transport: 'sse',
  caller_identity: 'static',
  global: false,
  tool_count: 3,
  domains: ['query', 'schema'],
  last_synced: hoursAgo(30),
  status: 'stale',
});

export const FILESYSTEM = server({
  name: 'filesystem',
  display_name: 'Filesystem',
  description: 'Browse and edit files inside the agent workspace volume.',
  transport: 'stdio',
  caller_identity: 'static',
  global: false,
  tool_count: 6,
  domains: ['files', 'directories'],
  last_synced: hoursAgo(2),
});

export const KORA = server({
  name: 'kora',
  display_name: 'kora Platform',
  description:
    'Manage agents, runs, policy grants and models through the kora control plane. Every call is checked against your own grants.',
  endpoint: 'http://orbit-mcp-gateway.orbit-system.svc:8095/kora',
  tool_count: 18,
  domains: ['agents', 'runs', 'policy', 'models', 'triggers'],
  last_synced: hoursAgo(0.1),
});

export const CUSTOM = server({
  name: 'billing-internal',
  display_name: 'Billing (internal)',
  description: '',
  caller_identity: 'static',
  global: false,
  tool_count: 0,
  domains: [],
  last_synced: '',
  status: 'unknown',
});

export const MOCK_SERVERS: McpServer[] = [
  KORA,
  GITHUB,
  SLACK,
  LINEAR,
  POSTGRES,
  FILESYSTEM,
  CUSTOM,
];

/** Extra servers so the list exercises scrolling and the grid wrap. */
export const MANY_SERVERS: McpServer[] = [
  ...MOCK_SERVERS,
  ...['Notion', 'Sentry', 'PagerDuty', 'Jira', 'Datadog', 'Stripe', 'Confluence', 'Figma'].map(
    (display, i) =>
      server({
        name: display.toLowerCase(),
        display_name: display,
        description: `${display} tools for agents working in your workspace.`,
        caller_identity: i % 3 === 0 ? 'forward' : 'static',
        global: i % 2 === 0,
        tool_count: 4 + i * 3,
        domains: ['read', 'write', 'search', 'admin'].slice(0, 2 + (i % 3)),
        last_synced: hoursAgo(i + 1),
        status: i === 5 ? 'stale' : 'healthy',
      }),
  ),
];

const tool = (slug: string, name: string, description: string): McpCatalogTool => ({
  tenant_id: TENANT,
  server_slug: slug,
  tool_name: name,
  scope: `mcp:${slug}:${name.replace('.', ':')}:use`,
  description,
});

export const GITHUB_TOOLS: McpCatalogTool[] = [
  tool('github', 'repos.get', 'Get repository metadata, default branch and visibility.'),
  tool('github', 'repos.list', 'List repositories the user can access in an organisation.'),
  tool('github', 'repos.get_file', 'Read a file or directory listing at a given ref.'),
  tool('github', 'repos.create_branch', 'Create a branch from an existing ref.'),
  tool('github', 'repos.push_files', 'Commit one or more file changes to a branch.'),
  tool('github', 'issues.create', 'Open an issue with labels, assignees and a body.'),
  tool('github', 'issues.get', 'Fetch an issue with its comments.'),
  tool('github', 'issues.list', 'List issues filtered by state, label or assignee.'),
  tool('github', 'issues.update', 'Edit the title, body, labels or state of an issue.'),
  tool('github', 'issues.comment', 'Add a comment to an issue or pull request.'),
  tool('github', 'pulls.create', 'Open a pull request from a head branch into a base branch.'),
  tool('github', 'pulls.get', 'Fetch a pull request with its status and mergeability.'),
  tool('github', 'pulls.list', 'List pull requests by state, base or author.'),
  tool('github', 'pulls.get_diff', 'Return the unified diff for a pull request.'),
  tool('github', 'pulls.review', 'Submit a review with inline comments.'),
  tool('github', 'pulls.merge', 'Merge a pull request using merge, squash or rebase.'),
  tool('github', 'actions.list_runs', 'List workflow runs for a repository or workflow.'),
  tool('github', 'actions.get_run', 'Fetch a workflow run with job and step results.'),
  tool('github', 'actions.get_logs', 'Download the logs for a failed job.'),
  tool('github', 'actions.rerun', 'Re-run all or only the failed jobs of a workflow run.'),
  tool('github', 'search.code', 'Search code across repositories the user can read.'),
  tool('github', 'search.issues', 'Search issues and pull requests with qualifiers.'),
  tool('github', 'search.repos', 'Search repositories by name, topic or language.'),
  tool('github', 'search.users', 'Look up users and organisations.'),
];

export const SLACK_TOOLS: McpCatalogTool[] = [
  tool('slack', 'chat.post_message', 'Post a message to a channel or thread.'),
  tool('slack', 'chat.update_message', 'Edit a message the app previously posted.'),
  tool('slack', 'chat.get_thread', 'Read every reply in a thread.'),
  tool('slack', 'channels.list', 'List public channels in the workspace.'),
  tool('slack', 'channels.history', 'Read recent messages from a channel.'),
  tool('slack', 'channels.join', 'Join a public channel.'),
  tool('slack', 'users.get', 'Look up a person by id or email.'),
  tool('slack', 'users.list', 'List people in the workspace.'),
  tool('slack', 'users.presence', 'Check whether a person is active.'),
];

export const KORA_TOOLS: McpCatalogTool[] = [
  tool('kora', 'agents.list', 'List the agent types in this workspace.'),
  tool('kora', 'agents.get', 'Fetch one agent type with its grants and connectors.'),
  tool('kora', 'agents.create', 'Create an agent type from a name, model and system prompt.'),
  tool('kora', 'agents.update', 'Change an agent type configuration.'),
  tool('kora', 'runs.list', 'List recent runs, newest first.'),
  tool('kora', 'runs.start', 'Start a run of an agent type with an initial message.'),
  tool('kora', 'runs.get', 'Fetch a run with its state and summary.'),
  tool('kora', 'runs.events', 'Stream the event log for a run.'),
  tool('kora', 'runs.terminate', 'Stop an active run.'),
  tool('kora', 'policy.grants_list', 'List grants for a principal.'),
  tool('kora', 'policy.grants_create', 'Grant scopes to a principal, capped by your own.'),
  tool('kora', 'policy.scopes', 'List the scopes you are allowed to delegate.'),
  tool('kora', 'models.list', 'List models available to this workspace.'),
  tool('kora', 'models.providers_list', 'List configured model providers.'),
  tool('kora', 'triggers.list', 'List triggers and their sources.'),
  tool('kora', 'triggers.create', 'Create a webhook or cron trigger for an agent type.'),
  tool('kora', 'triggers.deliveries', 'List recent trigger deliveries.'),
  tool('kora', 'triggers.delete', 'Delete a trigger.'),
];

export const MOCK_TOOLS: McpCatalogTool[] = [...GITHUB_TOOLS, ...SLACK_TOOLS, ...KORA_TOOLS];
