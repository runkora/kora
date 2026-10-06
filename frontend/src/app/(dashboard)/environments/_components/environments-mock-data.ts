import type { MockRequest } from '@orbit/storybook/mock-api';
import type {
  ConnectionCatalogEntry,
  Connector,
  EffectiveAccess,
  EffectiveAccessPreviewRequest,
  EnvironmentSizes,
  SandboxImage,
} from '@orbit/shared/api';
import { COMMON_PACKAGE_REGISTRIES } from '../_types';
import type { EnvironmentRow } from '../_types';

const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;

/** An ISO timestamp the given number of milliseconds before now. */
const ago = (ms: number): string => new Date(Date.now() - ms).toISOString();

export const MOCK_CATALOG: ConnectionCatalogEntry[] = [
  {
    name: 'github-orbital',
    adapter: 'github_app',
    actions: [
      'repo:read',
      'repo:write',
      'pull_request:read',
      'pull_request:write',
      'issue:read',
      'issue:write',
    ],
    baseline: ['repo:read'],
    qualifier: { name: 'repository', description: 'Repositories the installation can reach' },
  },
  {
    name: 'slack-engineering',
    adapter: 'slack',
    actions: ['chat:post', 'chat:read', 'channels:read'],
    baseline: [],
    qualifier: null,
  },
  {
    name: 'linear-product',
    adapter: 'linear',
    actions: ['issue:read', 'issue:write', 'comment:write'],
    baseline: [],
    qualifier: null,
  },
  {
    name: 'prod-cluster',
    adapter: 'kubernetes',
    actions: ['pods:read', 'logs:read', 'deployments:read'],
    baseline: [],
    qualifier: { name: 'namespace', description: 'Kubernetes namespaces' },
  },
];

export const MOCK_ENVIRONMENTS: EnvironmentRow[] = [
  {
    id: '7c1f0a52-3b9e-4d1c-8a47-2f5e61b0c9d3',
    name: 'dev-registries',
    sandboxImage: 'kora-base',
    size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
    description: 'Public package registries needed to install dependencies.',
    egressHosts: [...COMMON_PACKAGE_REGISTRIES],
    connectors: [],
    createdAt: ago(41 * DAY),
    updatedAt: ago(3 * DAY),
  },
  {
    id: 'a94d2e18-6c07-4f3b-b5d2-81e7c4f09a66',
    name: 'code-review',
    sandboxImage: 'acme-go',
    size: { cpu: '4', memory: '8Gi', disk: '20Gi' },
    description: 'GitHub and Slack for reviewing pull requests and reporting back.',
    egressHosts: ['telemetry.kora.example'],
    connectors: [
      {
        connection: 'github-orbital',
        actions: ['repo:read', 'pull_request:read', 'pull_request:write'],
        resources: ['orbital/orbit', 'orbital/kora-docs'],
      },
      { connection: 'slack-engineering', actions: ['chat:post'] },
    ],
    createdAt: ago(28 * DAY),
    updatedAt: ago(2 * 60 * MINUTE),
  },
  {
    id: 'e03b7a91-52d4-4c8e-9f16-b7a2d85c3e40',
    name: 'incident-response',
    sandboxImage: 'acme-ops',
    size: { cpu: '1', memory: '2Gi', disk: '5Gi' },
    description: 'Read-only cluster access plus paging and chat.',
    egressHosts: ['api.pagerduty.com', 'status.kora.example'],
    connectors: [
      {
        connection: 'prod-cluster',
        actions: ['pods:read', 'logs:read'],
        resources: ['orbit-system'],
      },
      { connection: 'slack-engineering', actions: ['chat:post', 'chat:read'] },
    ],
    createdAt: ago(19 * DAY),
    updatedAt: ago(5 * DAY),
  },
  {
    id: '3f68c0d4-91ab-47e2-8d35-0c5b9e72a1f8',
    name: 'locked-down',
    sandboxImage: 'kora-base',
    size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
    description: '',
    egressHosts: [],
    connectors: [],
    createdAt: ago(60 * DAY),
    updatedAt: ago(60 * DAY),
  },
  {
    id: 'b52e9f03-7a18-4b6d-a0c4-d1387e45f2b9',
    name: 'product-triage',
    sandboxImage: 'acme-node',
    size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
    description: 'Linear issues with a single outbound host for release notes.',
    egressHosts: ['releases.kora.example'],
    connectors: [{ connection: 'linear-product', actions: ['issue:read', 'issue:write'] }],
    createdAt: ago(9 * DAY),
    updatedAt: ago(9 * DAY),
  },
];

export const MOCK_SIZES: EnvironmentSizes = {
  default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
};

export const MOCK_IMAGES: SandboxImage[] = [
  {
    name: 'acme-go',
    description: 'Go 1.26, buf and golangci-lint on the kora base image.',
    image: 'registry.acme.example/sandbox/go:1.26',
    digest: 'sha256:5c1e',
    valid: true,
    warm: true,
  },
  {
    name: 'acme-node',
    description: 'Node 22 and pnpm on the kora base image.',
    image: 'registry.acme.example/sandbox/node:22',
    digest: 'sha256:9f02',
    valid: true,
    warm: false,
  },
  {
    name: 'acme-ops',
    description: 'kubectl and helm for incident work.',
    image: 'registry.acme.example/sandbox/ops:3',
    valid: false,
    problem: 'image runs as uid 0, want uid 100',
    warm: false,
  },
  {
    name: 'kora-base',
    description: 'The kora base image.',
    image: 'ghcr.io/clear-route/kora-sandbox-base:1.0.0',
    digest: 'sha256:ab12',
    valid: true,
    warm: true,
  },
];

export const MANY_ENVIRONMENTS: EnvironmentRow[] = [
  ...MOCK_ENVIRONMENTS,
  ...Array.from({ length: 18 }, (_, i): EnvironmentRow => {
    const n = String(i + 1).padStart(2, '0');
    return {
      id: `c${n}0a1b2-0000-4000-8000-00000000${n}00`,
      name: `team-sandbox-${n}`,
      sandboxImage: i % 2 === 0 ? 'acme-go' : 'kora-base',
      size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
      description: i % 2 === 0 ? `Scratch environment for team ${n}.` : '',
      egressHosts: i % 3 === 0 ? [] : COMMON_PACKAGE_REGISTRIES.slice(0, 1 + (i % 5)),
      connectors: i % 4 === 0 ? [{ connection: 'github-orbital', actions: ['repo:read'] }] : [],
      createdAt: ago((i + 2) * DAY),
      updatedAt: ago((i + 1) * DAY),
    };
  }),
];

/** Hosts the egress gateway adds for an adapter once a connection is attached. */
const ADAPTER_HOSTS: Record<string, string[]> = {
  github_app: ['github.com', 'api.github.com', 'codeload.github.com', '*.githubusercontent.com'],
  slack: ['slack.com', 'api.slack.com'],
  linear: ['api.linear.app'],
  kubernetes: ['kubernetes.default.svc'],
};

/** Answers the live preview the environment wizard calls, resolving the draft the way the backend does. */
export function previewHandler(request: MockRequest): { data: EffectiveAccess } {
  const draft = request.body as EffectiveAccessPreviewRequest;
  const own = draft.connectivity.mode === 'whitelist' ? (draft.connectivity.hosts ?? []) : [];
  const connectors: Connector[] = draft.connectors;
  const connectorEgress = connectors.map((c) => {
    const adapter = MOCK_CATALOG.find((e) => e.name === c.connection)?.adapter ?? '';
    return { connection: c.connection, adapter, hosts: ADAPTER_HOSTS[adapter] ?? [] };
  });
  const hosts = [...new Set([...own, ...connectorEgress.flatMap((e) => e.hosts)])].sort();
  return {
    data: {
      open: false,
      own_hosts: own,
      environments: [],
      environment_hosts: [],
      connector_egress: connectorEgress,
      connectors,
      hosts,
    },
  };
}
