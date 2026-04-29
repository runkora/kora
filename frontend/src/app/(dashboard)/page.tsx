import {
  Badge,
  Button,
  Card,
  Icon,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  Grid,
  Heading,
  List,
  Progress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
} from '@orbit/terra';
import { HealthStatus } from '@orbit/components';
import { StatusBadge } from '@orbit/components';
import { MetricCard } from '@orbit/components';
import { EventFeedItem } from '@orbit/components';

// ─── Mock Data ────────────────────────────────────────────────────────────────

const STATS = [
  {
    id: 'agents',
    label: 'Active Agents',
    value: '12',
    delta: '+2 this hour',
    deltaPositive: true,
    icon: 'bot',
  },
  {
    id: 'policies',
    label: 'Policy Evaluations',
    value: '4,821',
    delta: '↑ 18% vs yesterday',
    deltaPositive: true,
    icon: 'shield-check',
  },
  {
    id: 'credentials',
    label: 'Credential Issuances',
    value: '203',
    delta: '↓ 4% vs yesterday',
    deltaPositive: false,
    icon: 'key',
  },
  {
    id: 'events',
    label: 'Events (24 h)',
    value: '91.4k',
    delta: 'across 6 pipelines',
    deltaPositive: true,
    icon: 'activity',
  },
] as const;

const AGENTS = [
  {
    id: 'agt_01HZ9K3V',
    name: 'apollo-retriever',
    status: 'healthy' as const,
    autonomy: 'L2 — Supervised',
    provider: 'anthropic / claude-3-5-haiku',
    lastSeen: '9 s ago',
    runs: 1_204,
  },
  {
    id: 'agt_02JA1M7N',
    name: 'hermes-dispatcher',
    status: 'healthy' as const,
    autonomy: 'L3 — Conditional',
    provider: 'openai / gpt-4o',
    lastSeen: '22 s ago',
    runs: 878,
  },
  {
    id: 'agt_03KB5P2Q',
    name: 'daedalus-planner',
    status: 'degraded' as const,
    autonomy: 'L3 — Conditional',
    provider: 'anthropic / claude-3-7-sonnet',
    lastSeen: '2 m ago',
    runs: 312,
  },
  {
    id: 'agt_04LC8R9S',
    name: 'athena-policy-auditor',
    status: 'idle' as const,
    autonomy: 'L1 — Assisted',
    provider: 'openai / gpt-4o-mini',
    lastSeen: '14 m ago',
    runs: 56,
  },
  {
    id: 'agt_05MD2T4U',
    name: 'prometheus-crawler',
    status: 'error' as const,
    autonomy: 'L2 — Supervised',
    provider: 'google / gemini-2-flash',
    lastSeen: '1 h ago',
    runs: 91,
  },
  {
    id: 'agt_06NE6V1W',
    name: 'iris-notifier',
    status: 'pending' as const,
    autonomy: 'L2 — Supervised',
    provider: 'openai / gpt-4o',
    lastSeen: 'just now',
    runs: 0,
  },
];

const EVENTS = [
  {
    id: 'evt_001',
    type: 'policy.allow',
    agent: 'apollo-retriever',
    message: 'Tool call approved — read_file within scope',
    ts: '09:41:12',
    severity: 'healthy' as const,
  },
  {
    id: 'evt_002',
    type: 'credential.issued',
    agent: 'hermes-dispatcher',
    message: 'Short-lived S3 token issued (TTL 900 s)',
    ts: '09:41:08',
    severity: 'healthy' as const,
  },
  {
    id: 'evt_003',
    type: 'policy.deny',
    agent: 'daedalus-planner',
    message: 'Tool call blocked — exec_shell outside allowed scope',
    ts: '09:40:55',
    severity: 'error' as const,
  },
  {
    id: 'evt_004',
    type: 'agent.degraded',
    agent: 'daedalus-planner',
    message: 'Response latency exceeded P99 threshold (4.2 s)',
    ts: '09:40:50',
    severity: 'degraded' as const,
  },
  {
    id: 'evt_005',
    type: 'agent.started',
    agent: 'iris-notifier',
    message: 'Agent bootstrap complete — awaiting first task',
    ts: '09:40:41',
    severity: 'idle' as const,
  },
  {
    id: 'evt_006',
    type: 'policy.allow',
    agent: 'apollo-retriever',
    message: 'Tool call approved — list_dir within scope',
    ts: '09:40:33',
    severity: 'pending' as const,
  },
];

const HEALTH_RESOURCES = [
  { label: 'Control Plane CPU', value: 34, variant: 'healthy' as const },
  { label: 'Control Plane Memory', value: 61, variant: 'degraded' as const },
  { label: 'Event Spine Throughput', value: 48, variant: 'healthy' as const },
  { label: 'Policy Engine Load', value: 22, variant: 'healthy' as const },
  { label: 'Credential Store Disk', value: 87, variant: 'error' as const },
];

// ─── Sub-components ───────────────────────────────────────────────────────────

// ─── Page ─────────────────────────────────────────────────────────────────────

export default function DashboardPage() {
  return (
    <Stack gap="spacious" asChild>
      <main className="px-6 py-8 max-w-screen-xl mx-auto">
        {/* Page title */}
        <Stack gap="tight">
          <Heading variant="title">Dashboard</Heading>
          <Text color="muted">Real-time overview of the Orbit control plane.</Text>
        </Stack>

        <HealthStatus />

        {/* ── Stat cards ── */}
        <Grid cols={{ base: 2, lg: 4 }} gap="comfortable">
          {STATS.map((stat) => (
            <MetricCard
              key={stat.id}
              icon={
                <Icon
                  name={stat.icon}
                  size="sm"
                  container
                  containerVariant="surface-subtle"
                  aria-hidden
                />
              }
              label={stat.label}
              value={stat.value}
              delta={{ text: stat.delta, positive: stat.deltaPositive }}
            />
          ))}
        </Grid>

        {/* ── Agents table ── */}
        <Card>
          <CardHeader>
            <Stack direction="horizontal" align="center" justify="between">
              <Stack gap="tight">
                <CardTitle>Active Agents</CardTitle>
                <Text size="supporting" color="muted">
                  All agents currently registered with the control plane.
                </Text>
              </Stack>
              <Button variant="outline" size="sm">
                View all
                <Icon name="chevron-right" size="2xs" />
              </Button>
            </Stack>
          </CardHeader>
          {/*
           * flush: removes all padding so the table reaches the card edges.
           * The table provides its own internal cell padding.
           */}
          <CardContent flush>
            <TableRoot>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Agent</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Autonomy</TableHead>
                    <TableHead>Provider / Model</TableHead>
                    <TableHead>Runs</TableHead>
                    <TableHead>Last seen</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {AGENTS.map((agent) => (
                    <TableRow key={agent.id}>
                      <TableCell>
                        <Stack gap="none">
                          {/*
                           * mono: renders agent names in monospace — they are
                           * technical identifiers, not prose. weight="emphasis"
                           * gives the name mild prominence over the ID below.
                           */}
                          <Text size="supporting" weight="emphasis" mono>
                            {agent.name}
                          </Text>
                          {/*
                           * mono: agent IDs are machine-generated identifiers.
                           * Terra `Text` with `mono` is correct here (vs `Code`)
                           * because this is a standalone field, not inline prose.
                           */}
                          <Text size="caption" color="subtle" mono>
                            {agent.id}
                          </Text>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={agent.status} />
                      </TableCell>
                      <TableCell>
                        <Text size="supporting" color="muted">
                          {agent.autonomy}
                        </Text>
                      </TableCell>
                      <TableCell>
                        {/*
                         * mono + color="muted": provider/model strings are
                         * technical identifiers rendered in a secondary colour.
                         */}
                        <Text size="supporting" color="muted" mono>
                          {agent.provider}
                        </Text>
                      </TableCell>
                      <TableCell>
                        {/*
                         * mono + tabular: run counts are numeric — tabular-nums
                         * prevents layout jitter as values change; mono ensures
                         * digits align vertically across rows.
                         */}
                        <Text size="supporting" mono tabular>
                          {agent.runs.toLocaleString()}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Text size="supporting" color="muted">
                          {agent.lastSeen}
                        </Text>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableRoot>
          </CardContent>
          {/*
           * justify="between": count label on the left, load-more action on
           * the right. compact: tightens py to py-3 for a dense footer strip.
           */}
          <CardFooter justify="between" compact>
            <Text size="caption" color="subtle">
              Showing {AGENTS.length} of 12 agents
            </Text>
            <Button variant="ghost" size="sm">
              Load more
              <Icon name="chevron-right" size="2xs" />
            </Button>
          </CardFooter>
        </Card>

        {/* ── Bottom row ── */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          {/* Recent Events */}
          {/*
           * column: makes the card a flex column so CardFooter's mt-auto
           * pushes it to the card's bottom edge regardless of content height.
           */}
          <Card column>
            <CardHeader>
              <Stack direction="horizontal" align="center" justify="between">
                <Stack gap="tight">
                  <CardTitle>Recent Events</CardTitle>
                  <Text size="supporting" color="muted">
                    Live feed from the event spine.
                  </Text>
                </Stack>
                <Badge variant="pending" dot>
                  Live
                </Badge>
              </Stack>
            </CardHeader>
            {/*
             * flush grow: edge-to-edge event list that fills remaining card
             * height before the footer. `grow` only works inside `<Card column>`.
             */}
            <CardContent flush grow>
              <List separator>
                {EVENTS.map((evt) => (
                  <EventFeedItem
                    key={evt.id}
                    status={evt.severity}
                    label={evt.type}
                    meta={evt.agent}
                    message={evt.message}
                    timestamp={evt.ts}
                  />
                ))}
              </List>
            </CardContent>
            <CardFooter justify="center" compact>
              <Button variant="ghost" size="sm">
                Open event log
                <Icon name="chevron-right" size="2xs" />
              </Button>
            </CardFooter>
          </Card>

          {/* System Health */}
          <Card column>
            <CardHeader>
              <Stack direction="horizontal" align="center" justify="between">
                <Stack gap="tight">
                  <CardTitle>System Health</CardTitle>
                  <Text size="supporting" color="muted">
                    Control plane resource utilisation.
                  </Text>
                </Stack>
                <Button variant="ghost" size="icon" aria-label="Refresh health">
                  <Icon name="refresh-cw" />
                </Button>
              </Stack>
            </CardHeader>
            {/*
             * grow: fills remaining card height so the footer stays at the
             * card's bottom edge. The inner Stack carries gap="spacious" because
             * CardContent has no spacing prop — internal layout belongs inside.
             */}
            <CardContent grow>
              <Stack gap="comfortable">
                {HEALTH_RESOURCES.map(({ label, value, variant }) => (
                  <Stack key={label} gap="snug">
                    <Stack direction="horizontal" align="center" justify="between">
                      <Text size="supporting" color="muted">
                        {label}
                      </Text>
                      {/*
                       * mono + tabular: percentage values are numeric — tabular
                       * prevents jitter; mono aligns digits in a fixed column.
                       * color="status-*": matches the progress bar token below.
                       */}
                      <Text
                        size="caption"
                        weight="emphasis"
                        mono
                        tabular
                        color={
                          variant === 'error'
                            ? 'status-error'
                            : variant === 'degraded'
                              ? 'status-degraded'
                              : 'status-healthy'
                        }
                      >
                        {value}%
                      </Text>
                    </Stack>
                    <Progress label={label} value={value} variant={variant} size="md" />
                  </Stack>
                ))}
              </Stack>
            </CardContent>
            <CardFooter justify="between" compact>
              <Stack direction="horizontal" align="center" gap="snug">
                <Icon name="cpu" size="2xs" color="subtle" aria-hidden />
                <Text size="caption" color="subtle">
                  Updated 30 s ago
                </Text>
              </Stack>
              <Button variant="ghost" size="sm">
                Infrastructure
                <Icon name="chevron-right" size="2xs" />
              </Button>
            </CardFooter>
          </Card>
        </Grid>
      </main>
    </Stack>
  );
}
