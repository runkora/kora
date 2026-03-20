/**
 * Component Grid
 *
 * A single-page reference view of every implemented Terra component.
 * Use this story to audit branding, colour, and typographic changes across
 * the whole system at a glance — the design-system equivalent of a Figma
 * component sheet or a printed style guide.
 *
 * Nothing in this file is a prop-control story. Each block is a static
 * render that shows representative real-world usage with Orbit-flavoured
 * content (agents, credentials, policies).
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Activity, Info, Rocket, Settings, Shield, Trash2, Zap } from 'lucide-react';
import {
  Anchor,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Code,
  Grid,
  Heading,
  PageLayout,
  Section,
  Stack,
  Text,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '../../index';

// ─── Story meta ───────────────────────────────────────────────────────────────

const meta: Meta = {
  title: 'Terra/Overview/Component Grid',
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
};

export default meta;
type Story = StoryObj;

// ─── Local helpers ─────────────────────────────────────────────────────────────

/** A labelled sub-group within a grid section. */
function Group({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <Text size="caption" color="muted" mono className="uppercase tracking-widest">
        {label}
      </Text>
      {children}
    </div>
  );
}

/** A single design-token swatch. */
function Swatch({ name, bg, text }: { name: string; bg: string; text?: string }) {
  return (
    <div className="flex flex-col gap-1.5 min-w-0">
      <div
        className="h-10 w-full rounded-md border border-border-subtle"
        style={{ background: bg }}
      />
      <Text size="micro" mono color="muted" className="truncate">
        {name}
      </Text>
      {text && (
        <Text size="micro" mono color="muted" className="truncate opacity-60">
          {text}
        </Text>
      )}
    </div>
  );
}

/** A horizontal rule that acts as a visual section divider. */
function Rule() {
  return <div className="border-t border-border-subtle" />;
}

// ─── Story ─────────────────────────────────────────────────────────────────────

export const ComponentGrid: Story = {
  name: 'Component Grid',
  render: () => (
    <TooltipProvider>
      <div className="min-h-screen bg-background">
        <div className="columns-2 [&>*]:break-inside-avoid">
          {/* ── Design Tokens ──────────────────────────────────────────── */}
          <Section heading="Design Tokens">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Backgrounds">
                <Grid cols={6} gap="comfortable">
                  <Swatch name="background" bg="oklch(4%  0.04 137.73)" text="--color-background" />
                  <Swatch name="surface" bg="oklch(9%  0.04 137.73)" text="--color-surface" />
                  <Swatch
                    name="surface-elevated"
                    bg="oklch(14% 0.04 137.73)"
                    text="--color-surface-elevated"
                  />
                  <Swatch
                    name="surface-overlay"
                    bg="oklch(20% 0.04 137.73)"
                    text="--color-surface-overlay"
                  />
                </Grid>
              </Group>

              <Rule />

              <Group label="Foregrounds">
                <Grid cols={6} gap="comfortable">
                  <Swatch name="foreground" bg="oklch(96% 0.04 137.73)" text="--color-foreground" />
                  <Swatch name="muted" bg="oklch(72% 0.04 137.73)" text="--color-muted" />
                  <Swatch name="subtle" bg="oklch(63% 0.04 137.73)" text="--color-subtle" />
                </Grid>
              </Group>

              <Rule />

              <Group label="Accent">
                <Grid cols={6} gap="comfortable">
                  <Swatch name="accent" bg="oklch(72% 0.23 26)" text="--color-accent" />
                  <Swatch name="accent-hover" bg="oklch(82% 0.23 26)" text="--color-accent-hover" />
                  <Swatch
                    name="border-accent"
                    bg="oklch(42% 0.23 26)"
                    text="--color-border-accent"
                  />
                </Grid>
              </Group>

              <Rule />

              <Group label="Borders">
                <Grid cols={6} gap="comfortable">
                  <Swatch name="border" bg="oklch(52% 0.04 137.73)" text="--color-border" />
                  <Swatch
                    name="border-subtle"
                    bg="oklch(36% 0.04 137.73)"
                    text="--color-border-subtle"
                  />
                </Grid>
              </Group>

              <Rule />

              <Group label="Status signals">
                <Grid cols={6} gap="comfortable">
                  <Swatch name="status-healthy" bg="#8ae250" text="--color-status-healthy" />
                  <Swatch name="status-degraded" bg="#ffd046" text="--color-status-degraded" />
                  <Swatch name="status-error" bg="#ef4444" text="--color-status-error" />
                  <Swatch
                    name="status-idle"
                    bg="oklch(63% 0.04 137.73)"
                    text="--color-status-idle"
                  />
                  <Swatch
                    name="status-pending"
                    bg="oklch(72% 0.23 26)"
                    text="--color-status-pending"
                  />
                </Grid>
              </Group>
            </Stack>
          </Section>

          {/* ── Typography ─────────────────────────────────────────────── */}
          <Section heading="Typography">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Heading scale">
                <Stack direction="vertical" gap="snug">
                  <Heading variant="display">Display — Control plane for AI agents</Heading>
                  <Heading variant="title">Title — Agent Identity Management</Heading>
                  <Heading variant="section">Section — Active Deployments</Heading>
                  <Heading variant="subsection">Subsection — Policy Groups</Heading>
                  <Heading variant="label">Label — Credential scope</Heading>
                  <Heading variant="metric">42</Heading>
                </Stack>
              </Group>

              <Rule />

              <Group label="Heading colours">
                <Stack direction="vertical" gap="tight">
                  <Heading variant="section" color="foreground">
                    Foreground — default
                  </Heading>
                  <Heading variant="section" color="muted">
                    Muted — secondary headings
                  </Heading>
                  <Heading variant="section" color="accent">
                    Accent — highlighted callout
                  </Heading>
                </Stack>
              </Group>

              <Rule />

              <Group label="Text sizes">
                <Stack direction="vertical" gap="snug">
                  <Text size="lead">
                    Lead — A federated identity layer for autonomous agents across every compute
                    boundary.
                  </Text>
                  <Text size="body">
                    Body — Orbit enforces policy before execution, not after. Every credential is
                    scoped, temporary, and auditable.
                  </Text>
                  <Text size="supporting">
                    Supporting — Session expiry is set to 24 hours by default. Rotate signing keys
                    via the key management API.
                  </Text>
                  <Text size="caption">
                    Caption — Last evaluated 3 minutes ago · Policy version 1.4.2
                  </Text>
                  <Text size="micro">Micro — orbit-3-turbo · us-east-1 · 2024-01-15</Text>
                </Stack>
              </Group>

              <Rule />

              <Group label="Text colours">
                <Stack direction="horizontal" gap="relaxed" wrap>
                  <Text size="body" color="foreground">
                    foreground
                  </Text>
                  <Text size="body" color="muted">
                    muted
                  </Text>
                  <Text size="body" color="subtle">
                    subtle
                  </Text>
                  <Text size="body" color="accent">
                    accent
                  </Text>
                  <Text size="body" color="status-healthy">
                    status-healthy
                  </Text>
                  <Text size="body" color="status-degraded">
                    status-degraded
                  </Text>
                  <Text size="body" color="status-error">
                    status-error
                  </Text>
                </Stack>
              </Group>

              <Rule />

              <Group label="Text weights">
                <Stack direction="horizontal" gap="relaxed" wrap>
                  <Text size="body" weight="regular">
                    Regular — 400
                  </Text>
                  <Text size="body" weight="emphasis">
                    Emphasis — 500
                  </Text>
                  <Text size="body" weight="strong">
                    Strong — 600
                  </Text>
                  <Text size="body" weight="heavy">
                    Heavy — 700
                  </Text>
                </Stack>
              </Group>

              <Rule />

              <Group label="Text modifiers">
                <Stack direction="vertical" gap="tight">
                  <Text size="body" mono>
                    Mono — orbit-agent-apollo-3 · SPIFFE/SVID
                  </Text>
                  <Text size="body" truncate className="max-w-xs">
                    Truncate — This text is intentionally very long and will be clipped at the
                    container edge with an ellipsis
                  </Text>
                  <Text size="caption" tabular>
                    Tabular — 1,024,000 tokens · 99.97% uptime · $0.0042
                  </Text>
                </Stack>
              </Group>

              <Rule />

              <Group label="Code">
                <Stack direction="vertical" gap="snug">
                  <Text size="body">
                    Call the <Code variant="inline">evaluate(policy, context)</Code> function to run
                    OPA against the current request.
                  </Text>
                  <Text size="body">
                    Set <Code variant="inline">ORBIT_POLICY_ENGINE=opa</Code> before starting the
                    control plane.
                  </Text>
                  <div className="rounded-md bg-surface-elevated p-4">
                    <Code variant="bare">
                      {`const result = await orbit.agents.deploy({
  id: "apollo-3",
  policy: "default-restricted",
  autonomy: "supervised",
});`}
                    </Code>
                  </div>
                </Stack>
              </Group>
            </Stack>
          </Section>

          {/* ── Button ─────────────────────────────────────────────────── */}
          <Section heading="Button">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Variants">
                <Stack direction="horizontal" gap="snug" wrap>
                  <Button variant="default">Deploy agent</Button>
                  <Button variant="secondary">View details</Button>
                  <Button variant="outline">Configure</Button>
                  <Button variant="ghost">Dismiss</Button>
                  <Button variant="muted">Archive</Button>
                  <Button variant="destructive">
                    <Trash2 />
                    Revoke access
                  </Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="Sizes">
                <Stack direction="horizontal" gap="snug" align="center" wrap>
                  <Button size="xs">Extra small</Button>
                  <Button size="sm">Small</Button>
                  <Button size="md">Medium</Button>
                  <Button size="lg">Large</Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="With icon">
                <Stack direction="horizontal" gap="snug" wrap>
                  <Button variant="default">
                    <Rocket />
                    Launch
                  </Button>
                  <Button variant="secondary">
                    <Shield />
                    Policies
                  </Button>
                  <Button variant="outline">
                    <Activity />
                    Monitor
                  </Button>
                  <Button variant="destructive">
                    <Trash2 />
                    Delete
                  </Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="Icon-only">
                <Stack direction="horizontal" gap="snug">
                  <Button variant="ghost" size="icon" aria-label="Settings">
                    <Settings />
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label="Info">
                    <Info />
                  </Button>
                  <Button variant="outline" size="icon" aria-label="Activity">
                    <Activity />
                  </Button>
                  <Button variant="default" size="icon" aria-label="Launch">
                    <Rocket />
                  </Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="Pill shape">
                <Stack direction="horizontal" gap="snug" wrap>
                  <Button shape="pill" variant="default">
                    Deploy
                  </Button>
                  <Button shape="pill" variant="secondary">
                    Configure
                  </Button>
                  <Button shape="pill" variant="outline">
                    Archive
                  </Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="States — disabled">
                <Stack direction="horizontal" gap="snug" wrap>
                  <Button variant="default" disabled>
                    Disabled default
                  </Button>
                  <Button variant="outline" disabled>
                    Disabled outline
                  </Button>
                  <Button variant="destructive" disabled>
                    Disabled destructive
                  </Button>
                </Stack>
              </Group>

              <Rule />

              <Group label="Full width">
                <div className="max-w-sm">
                  <Button variant="default" fullWidth>
                    Create new agent
                  </Button>
                </div>
              </Group>
            </Stack>
          </Section>

          {/* ── Badge ──────────────────────────────────────────────────── */}
          <Section heading="Badge">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Structural variants">
                <Stack direction="horizontal" gap="snug" wrap align="center">
                  <Badge variant="default">Accent</Badge>
                  <Badge variant="secondary">Secondary</Badge>
                  <Badge variant="outline">Outline</Badge>
                  <Badge variant="dim">Dim</Badge>
                  <Badge variant="destructive">Destructive</Badge>
                </Stack>
              </Group>

              <Rule />

              <Group label="Status variants — with dot">
                <Stack direction="horizontal" gap="snug" wrap align="center">
                  <Badge variant="healthy" dot>
                    Healthy
                  </Badge>
                  <Badge variant="degraded" dot>
                    Degraded
                  </Badge>
                  <Badge variant="error" dot>
                    Error
                  </Badge>
                  <Badge variant="idle" dot>
                    Idle
                  </Badge>
                  <Badge variant="pending" dot>
                    Pending
                  </Badge>
                </Stack>
              </Group>

              <Rule />

              <Group label="Status variants — no dot">
                <Stack direction="horizontal" gap="snug" wrap align="center">
                  <Badge variant="healthy">Healthy</Badge>
                  <Badge variant="degraded">Degraded</Badge>
                  <Badge variant="error">Error</Badge>
                  <Badge variant="idle">Idle</Badge>
                  <Badge variant="pending">Pending</Badge>
                </Stack>
              </Group>

              <Rule />

              <Group label="Size comparison">
                <Stack direction="horizontal" gap="snug" align="center" wrap>
                  <Badge variant="default" size="md">
                    md — Standard
                  </Badge>
                  <Badge variant="default" size="sm">
                    sm — Compact
                  </Badge>
                  <Badge variant="dim" size="sm">
                    orbit-3-turbo
                  </Badge>
                  <Badge variant="secondary" size="sm">
                    ⌘ K
                  </Badge>
                  <Badge variant="secondary" size="sm">
                    v1.4.2
                  </Badge>
                </Stack>
              </Group>
            </Stack>
          </Section>

          {/* ── Anchor ─────────────────────────────────────────────────── */}
          <Section heading="Anchor">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Sizes in context">
                <Stack direction="vertical" gap="snug">
                  <Text size="body">
                    Read the{' '}
                    <Anchor size="md" href="#">
                      agent deployment guide
                    </Anchor>{' '}
                    before configuring credential scopes.
                  </Text>
                  <Text size="caption" color="muted">
                    Already have an account?{' '}
                    <Anchor size="sm" href="#">
                      Sign in instead
                    </Anchor>
                  </Text>
                  <Heading variant="section">
                    <Anchor size="inherit" href="#">
                      Inherit — sized by parent heading
                    </Anchor>
                  </Heading>
                </Stack>
              </Group>
            </Stack>
          </Section>

          {/* ── Tooltip ────────────────────────────────────────────────── */}
          <Section heading="Tooltip">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Positions">
                <Stack direction="horizontal" gap="comfortable" wrap>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="outline">Top (default)</Button>
                    </TooltipTrigger>
                    <TooltipContent side="top">Policy last evaluated 3 min ago</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="outline">Bottom</Button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">48 agents in this workspace</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="outline">Left</Button>
                    </TooltipTrigger>
                    <TooltipContent side="left">Revoke credential</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="outline">Right</Button>
                    </TooltipTrigger>
                    <TooltipContent side="right">View audit trail</TooltipContent>
                  </Tooltip>
                </Stack>
              </Group>

              <Rule />

              <Group label="On icon buttons">
                <Stack direction="horizontal" gap="snug">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label="Settings">
                        <Settings />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Agent settings</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label="Deploy">
                        <Rocket />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Deploy to production</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label="Trigger">
                        <Zap />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Trigger manual run</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button variant="ghost" size="icon" aria-label="Delete">
                        <Trash2 />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Delete agent</TooltipContent>
                  </Tooltip>
                </Stack>
              </Group>
            </Stack>
          </Section>

          {/* ── Card ───────────────────────────────────────────────────── */}
          <Section heading="Card">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Standard compositions">
                <Grid cols={2} gap="comfortable">
                  {/* Standard card */}
                  <Card>
                    <CardHeader>
                      <CardTitle>Agent: apollo-3</CardTitle>
                      <CardDescription>Last active 2 minutes ago · us-east-1</CardDescription>
                    </CardHeader>
                    <CardContent>
                      <Stack direction="horizontal" gap="snug" align="center">
                        <Badge variant="healthy" dot>
                          Healthy
                        </Badge>
                        <Badge variant="secondary" size="sm">
                          orbit-3-turbo
                        </Badge>
                      </Stack>
                    </CardContent>
                    <CardFooter>
                      <Button variant="ghost" size="sm">
                        View logs
                      </Button>
                      <Button variant="outline" size="sm">
                        Configure
                      </Button>
                    </CardFooter>
                  </Card>

                  {/* Stat / metric card */}
                  <Card>
                    <CardHeader compact>
                      <CardDescription>Active agents</CardDescription>
                      <Heading variant="metric">48</Heading>
                    </CardHeader>
                    <CardContent>
                      <Text size="caption" color="muted">
                        ↑ 12 from last week · 3 degraded
                      </Text>
                    </CardContent>
                    <CardFooter>
                      <Anchor size="sm" href="#">
                        View all
                      </Anchor>
                    </CardFooter>
                  </Card>

                  {/* Column card with sticky footer */}
                  <Card column>
                    <CardHeader>
                      <CardTitle>Credential scope</CardTitle>
                      <CardDescription>Temporary credentials issued on demand</CardDescription>
                    </CardHeader>
                    <CardContent grow>
                      <Stack direction="vertical" gap="tight">
                        <Text size="caption" color="muted" mono>
                          arn:aws:iam::123456789:role/orbit-agent
                        </Text>
                        <Text size="caption" color="muted" mono>
                          Expires in 58 minutes
                        </Text>
                      </Stack>
                    </CardContent>
                    <CardFooter justify="between">
                      <Badge variant="pending" size="sm" dot>
                        Pending rotation
                      </Badge>
                      <Button variant="outline" size="sm">
                        Rotate now
                      </Button>
                    </CardFooter>
                  </Card>
                </Grid>
              </Group>

              <Rule />

              <Group label="Flush content (code result)">
                <div className="max-w-sm">
                  <Card>
                    <CardHeader>
                      <CardTitle>Policy evaluation</CardTitle>
                      <CardDescription>OPA result for request #8821</CardDescription>
                    </CardHeader>
                    <CardContent flush>
                      <div className="rounded-b-lg bg-surface-elevated p-4">
                        <Code variant="bare">{`{ "allow": true, "reason": "role:operator" }`}</Code>
                      </div>
                    </CardContent>
                  </Card>
                </div>
              </Group>
            </Stack>
          </Section>

          {/* ── Stack ──────────────────────────────────────────────────── */}
          <Section heading="Stack">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Vertical">
                <div className="max-w-xs rounded-lg bg-surface-elevated p-4">
                  <Stack direction="vertical" gap="snug">
                    {['Identity layer', 'Policy engine', 'Credential broker', 'Event spine'].map(
                      (item) => (
                        <div key={item} className="rounded-md bg-surface px-3 py-2">
                          <Text size="caption" mono>
                            {item}
                          </Text>
                        </div>
                      ),
                    )}
                  </Stack>
                </div>
              </Group>

              <Rule />

              <Group label="Horizontal with wrap">
                <div className="rounded-lg bg-surface-elevated p-4">
                  <Stack direction="horizontal" gap="snug" wrap>
                    {[
                      'apollo-3',
                      'hermes-1',
                      'athena-7',
                      'zeus-2',
                      'ares-4',
                      'hera-9',
                      'poseidon-1',
                    ].map((name) => (
                      <Badge key={name} variant="secondary">
                        {name}
                      </Badge>
                    ))}
                  </Stack>
                </div>
              </Group>

              <Rule />

              <Group label="Cross-axis alignment — start / center / end / stretch">
                <Stack direction="vertical" gap="snug">
                  {(['start', 'center', 'end', 'stretch'] as const).map((align) => (
                    <div key={align} className="flex items-center gap-4">
                      <Text size="micro" mono color="muted" className="w-14 shrink-0">
                        {align}
                      </Text>
                      <Stack
                        direction="horizontal"
                        gap="snug"
                        align={align}
                        className="flex-1 rounded-md bg-surface p-3 h-16"
                      >
                        <div className="h-5 w-12 rounded bg-surface-elevated" />
                        <div className="h-3 w-20 rounded bg-surface-elevated" />
                        <div className="h-8 w-8 rounded bg-surface-elevated" />
                      </Stack>
                    </div>
                  ))}
                </Stack>
              </Group>

              <Rule />

              <Group label="Justify between — toolbar pattern">
                <Stack
                  direction="horizontal"
                  gap="snug"
                  justify="between"
                  align="center"
                  className="rounded-md bg-surface px-4 py-3"
                >
                  <Stack direction="horizontal" gap="snug" align="center">
                    <Heading variant="subsection">apollo-3</Heading>
                    <Badge variant="healthy" dot>
                      Healthy
                    </Badge>
                  </Stack>
                  <Stack direction="horizontal" gap="snug">
                    <Button variant="ghost" size="icon-sm" aria-label="Settings">
                      <Settings />
                    </Button>
                    <Button variant="outline" size="sm">
                      View logs
                    </Button>
                  </Stack>
                </Stack>
              </Group>
            </Stack>
          </Section>

          {/* ── Grid ───────────────────────────────────────────────────── */}
          <Section heading="Grid">
            <Stack direction="vertical" gap="relaxed">
              {([2, 3, 4] as const).map((cols) => (
                <Group key={cols} label={`${cols}-column grid`}>
                  <Grid cols={cols} gap="comfortable">
                    {Array.from({ length: cols }).map((_, i) => (
                      <div
                        key={i}
                        className="h-16 rounded-md bg-surface flex items-center justify-center"
                      >
                        <Text size="caption" color="muted" mono>
                          col {i + 1}
                        </Text>
                      </div>
                    ))}
                  </Grid>
                </Group>
              ))}

              <Rule />

              <Group label="Responsive card grid (2 cols)">
                <Grid cols={2} gap="comfortable">
                  {[
                    { name: 'apollo-3', status: 'healthy' as const, model: 'orbit-3-turbo' },
                    { name: 'hermes-1', status: 'degraded' as const, model: 'orbit-2' },
                    { name: 'athena-7', status: 'idle' as const, model: 'orbit-3' },
                  ].map(({ name, status, model }) => (
                    <Card key={name}>
                      <CardHeader compact>
                        <Stack direction="horizontal" justify="between" align="center">
                          <CardTitle>{name}</CardTitle>
                          <Badge variant={status} dot>
                            {status}
                          </Badge>
                        </Stack>
                        <CardDescription>{model}</CardDescription>
                      </CardHeader>
                      <CardFooter>
                        <Button variant="ghost" size="sm">
                          Logs
                        </Button>
                        <Button variant="outline" size="sm">
                          Open
                        </Button>
                      </CardFooter>
                    </Card>
                  ))}
                </Grid>
              </Group>
            </Stack>
          </Section>

          {/* ── Section ────────────────────────────────────────────────── */}
          <Section heading="Section">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Compositions">
                <div className="rounded-lg overflow-hidden">
                  <Section heading="Active Agents">
                    <Grid cols={2} gap="comfortable">
                      <Card>
                        <CardHeader compact>
                          <CardTitle>apollo-3</CardTitle>
                          <CardDescription>Supervised · orbit-3-turbo</CardDescription>
                        </CardHeader>
                      </Card>
                      <Card>
                        <CardHeader compact>
                          <CardTitle>hermes-1</CardTitle>
                          <CardDescription>Autonomous · orbit-2</CardDescription>
                        </CardHeader>
                      </Card>
                    </Grid>
                  </Section>

                  <Section heading="Overview">
                    <></>
                  </Section>

                  <Section>
                    <></>
                  </Section>
                </div>
              </Group>
            </Stack>
          </Section>

          {/* ── PageLayout ─────────────────────────────────────────────── */}
          <Section heading="PageLayout">
            <Stack direction="vertical" gap="relaxed">
              <Group label="Full shell — sidebar + header + main">
                {/*
                PageLayout is `h-screen` by default. We override to a fixed height
                here so it renders inline within the grid without escaping the canvas.
              */}
                <div className="rounded-lg overflow-hidden">
                  <PageLayout
                    className="h-96"
                    sidebar={
                      <Stack direction="vertical" gap="tight" className="p-4">
                        <Text
                          size="caption"
                          color="muted"
                          mono
                          className="uppercase tracking-widest px-2 pb-1"
                        >
                          Workspace
                        </Text>
                        {['Overview', 'Agents', 'Policies', 'Credentials', 'Audit log'].map(
                          (item) => (
                            <div
                              key={item}
                              className="rounded-md px-3 py-1.5 text-sm text-foreground/70 hover:bg-surface-elevated hover:text-foreground cursor-pointer transition-colors"
                            >
                              {item}
                            </div>
                          ),
                        )}
                      </Stack>
                    }
                    header={
                      <Stack
                        direction="horizontal"
                        justify="between"
                        align="center"
                        className="w-full"
                      >
                        <Stack direction="horizontal" gap="snug" align="center">
                          <Heading variant="label">Orbit Control Plane</Heading>
                          <Badge variant="secondary" size="sm">
                            v1.4.2
                          </Badge>
                        </Stack>
                        <Stack direction="horizontal" gap="snug" align="center">
                          <Badge variant="healthy" dot>
                            Healthy
                          </Badge>
                          <Button variant="ghost" size="icon-sm" aria-label="Settings">
                            <Settings />
                          </Button>
                        </Stack>
                      </Stack>
                    }
                  >
                    <Section heading="Active Agents">
                      <Grid cols={2} gap="comfortable">
                        {[
                          { name: 'apollo-3', status: 'healthy' as const, model: 'orbit-3-turbo' },
                          { name: 'hermes-1', status: 'degraded' as const, model: 'orbit-2' },
                        ].map(({ name, status, model }) => (
                          <Card key={name}>
                            <CardHeader compact>
                              <Stack direction="horizontal" justify="between" align="center">
                                <CardTitle>{name}</CardTitle>
                                <Badge variant={status} dot>
                                  {status}
                                </Badge>
                              </Stack>
                              <CardDescription>{model}</CardDescription>
                            </CardHeader>
                            <CardFooter>
                              <Button variant="ghost" size="sm">
                                Logs
                              </Button>
                              <Button variant="outline" size="sm">
                                Open
                              </Button>
                            </CardFooter>
                          </Card>
                        ))}
                      </Grid>
                    </Section>
                  </PageLayout>
                </div>
              </Group>

              <Rule />

              <Group label="Header-only — no sidebar">
                <div className="rounded-lg overflow-hidden">
                  <PageLayout
                    className="h-48"
                    header={
                      <Stack
                        direction="horizontal"
                        justify="between"
                        align="center"
                        className="w-full"
                      >
                        <Heading variant="label">Agent detail — apollo-3</Heading>
                        <Stack direction="horizontal" gap="snug">
                          <Button variant="outline" size="sm">
                            Configure
                          </Button>
                          <Button variant="default" size="sm">
                            <Rocket />
                            Deploy
                          </Button>
                        </Stack>
                      </Stack>
                    }
                  >
                    <></>
                  </PageLayout>
                </div>
              </Group>

              <Rule />

              <Group label="Bare shell — no sidebar, no header">
                <div className="rounded-lg overflow-hidden">
                  <PageLayout className="h-32">
                    <></>
                  </PageLayout>
                </div>
              </Group>
            </Stack>
          </Section>
        </div>
      </div>
    </TooltipProvider>
  ),
};
