import type { ConnectionRow } from '../_types';

const GITHUB_ACTIONS = [
  'metadata:read',
  'contents:read',
  'contents:write',
  'workflows:write',
  'issues:read',
  'issues:write',
  'pulls:read',
  'pulls:write',
  'ci:read',
  'ci:write',
];

/** A realistic catalog: a few adapters, one connection with no actions and one nobody attaches. */
export const MOCK_CONNECTIONS: ConnectionRow[] = [
  {
    name: 'github-orbital',
    adapter: 'github_app',
    actions: GITHUB_ACTIONS,
    usedBy: [
      { slug: 'code-reviewer', name: 'Code Reviewer' },
      { slug: 'coder', name: 'Coder' },
      { slug: 'security-reviewer', name: 'Security Reviewer' },
    ],
  },
  {
    name: 'github-oss',
    adapter: 'github_app',
    actions: ['contents:read', 'pulls:read', 'issues:read', 'issues:write'],
    usedBy: [{ slug: 'issue-triager', name: 'Issue Triager' }],
  },
  {
    name: 'slack-engineering',
    adapter: 'slack',
    actions: ['chat:post', 'chat:read', 'channels:read', 'reactions:write'],
    usedBy: [
      { slug: 'incident-responder', name: 'Incident Responder' },
      { slug: 'code-reviewer', name: 'Code Reviewer' },
    ],
  },
  {
    name: 'linear-product',
    adapter: 'linear',
    actions: ['issue:read', 'issue:write', 'project:read', 'comment:write'],
    usedBy: [{ slug: 'issue-triager', name: 'Issue Triager' }],
  },
  {
    name: 'google-workspace',
    adapter: 'google',
    actions: ['drive:read', 'docs:read', 'docs:write', 'calendar:read', 'gmail:read'],
    usedBy: [{ slug: 'research-assistant', name: 'Research Assistant' }],
  },
  {
    name: 'pagerduty-oncall',
    adapter: 'oauth2_generic',
    actions: ['incident:read', 'incident:write', 'oncall:read'],
    usedBy: [],
  },
  {
    name: 'prod-cluster',
    adapter: 'kubernetes',
    actions: ['pods:read', 'logs:read', 'deployments:read', 'deployments:write'],
    usedBy: [{ slug: 'incident-responder', name: 'Incident Responder' }],
  },
  {
    name: 'legacy-webhook',
    adapter: 'oauth2_generic',
    actions: [],
    usedBy: [],
  },
];

/** Enough rows to exercise scrolling, search and sorting. */
export const MANY_CONNECTIONS: ConnectionRow[] = [
  ...MOCK_CONNECTIONS,
  ...Array.from({ length: 22 }, (_, i): ConnectionRow => {
    const n = String(i + 1).padStart(2, '0');
    return {
      name: `github-team-${n}`,
      adapter: 'github_app',
      actions: GITHUB_ACTIONS.slice(0, 2 + (i % 8)),
      usedBy: i % 3 === 0 ? [{ slug: 'coder', name: 'Coder' }] : [],
    };
  }),
];
