import type { Meta, StoryObj } from '@storybook/react';
import { Heading, Text, Code } from './typography';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta = {
  title: 'Terra/Foundation/Typography',
  parameters: {
    layout: 'padded',
  },
} satisfies Meta;

export default meta;
type Story = StoryObj;

// ─── Helpers ──────────────────────────────────────────────────────────────────

/**
 * Labelled row showing: name → use when → example output.
 * The hint column is intentionally dim — it's guidance, not content.
 */
function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline gap-6 py-2 border-b border-border-subtle last:border-0">
      <div className="w-36 shrink-0 font-mono text-xs text-subtle">{label}</div>
      {hint && <div className="w-64 shrink-0 text-xs text-subtle italic">{hint}</div>}
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  );
}

/** Section divider used within stories to group related rows. */
function Section({ title }: { title: string }) {
  return (
    <div className="mt-8 mb-3">
      <Text size="caption" color="subtle" mono transform="uppercase">
        {title}
      </Text>
    </div>
  );
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * All four heading variants. Names reflect their role in the UI hierarchy —
 * title → section → subsection → label. The HTML element follows the variant
 * by default and can be swapped independently via `asChild`.
 */
export const Headings: Story = {
  render: () => (
    <div>
      <Section title="Heading — variant" />
      <Row label="title" hint="Single page-level landmark per view — one per route">
        <Heading variant="title">Deploy autonomous agents at scale</Heading>
      </Row>
      <Row label="section" hint="Major section heading — the most common heading level">
        <Heading variant="section">Agent configuration</Heading>
      </Row>
      <Row label="subsection" hint="Secondary grouping within a section">
        <Heading variant="subsection">Identity providers</Heading>
      </Row>
      <Row label="label" hint="Card titles, group labels, sidebar item headers">
        <Heading variant="label">OAuth 2.0 settings</Heading>
      </Row>

      <Section title="Heading — color" />
      <Row label="foreground" hint="Default — primary content headings">
        <Heading variant="section">Orbit Control Plane</Heading>
      </Row>
      <Row label="muted" hint="Secondary or subordinate headings">
        <Heading variant="section" color="muted">
          Secondary heading
        </Heading>
      </Row>
      <Row label="accent" hint="Highlighted headings — use sparingly">
        <Heading variant="section" color="accent">
          Highlighted section
        </Heading>
      </Row>
    </div>
  ),
};

/**
 * Body copy at all four semantic sizes, with color and weight modifiers.
 * Every prop describes purpose — not a scale position.
 */
export const BodyText: Story = {
  name: 'Body Text',
  render: () => (
    <div>
      <Section title="Text — size" />
      <Row label="lead" hint="Introductory paragraphs above sections or features">
        <Text size="lead">
          Orbit coordinates autonomous agents across your infrastructure with zero trust and full
          auditability.
        </Text>
      </Row>
      <Row label="body" hint="Default — the majority of UI text lives here">
        <Text>
          Every action an agent takes is tied to a verified identity, governed by policy, and
          recorded in the event spine.
        </Text>
      </Row>
      <Row label="supporting" hint="Descriptions, helper text, table metadata">
        <Text size="supporting">
          Credential brokering ensures agents never hold raw secrets — only scoped, time-limited
          access tokens.
        </Text>
      </Row>
      <Row label="caption" hint="Timestamps, status labels, fine-print auxiliary data">
        <Text size="caption">Last updated 3 minutes ago · Orbit v0.4.1</Text>
      </Row>

      <Section title="Text — color" />
      <Row label="foreground" hint="Primary content — full-brightness">
        <Text>Primary content text</Text>
      </Row>
      <Row label="muted" hint="Secondary content — supporting information">
        <Text color="muted">Secondary / supporting text</Text>
      </Row>
      <Row label="subtle" hint="Tertiary content — timestamps, metadata, placeholders">
        <Text color="subtle">Tertiary, timestamps, metadata</Text>
      </Row>
      <Row label="accent" hint="Highlighted or interactive text — links, active states">
        <Text color="accent">Highlighted or interactive text</Text>
      </Row>

      <Section title="Text — weight" />
      <Row label="regular" hint="Normal prose — default reading weight">
        <Text weight="regular">Regular — normal prose reading weight</Text>
      </Row>
      <Row label="emphasis" hint="Mild emphasis — labels, form field values">
        <Text weight="emphasis">Emphasis — labels and form fields</Text>
      </Row>
      <Row label="strong" hint="Strong emphasis — column headers, key–value pairs">
        <Text weight="strong">Strong — column headers, key values</Text>
      </Row>
      <Row label="heavy" hint="Maximum weight — deliberate typographic impact only">
        <Text weight="heavy">Heavy — reserved for typographic impact</Text>
      </Row>
    </div>
  ),
};

/**
 * Inline monospace code spans for identifiers, token names, environment
 * variables, and file paths. Variants describe context of use, not style.
 */
export const InlineCode: Story = {
  name: 'Inline Code',
  render: () => (
    <div>
      <Section title="Code — variant" />
      <Row label="inline" hint="Inside sentences and paragraphs — default">
        <Text>
          Set the <Code>ORBIT_API_KEY</Code> environment variable before running the agent.
        </Text>
      </Row>
      <Row label="bare" hint="Inside tables or containers that already provide styling">
        <Text>
          Reference: <Code variant="bare">--color-accent</Code> and{' '}
          <Code variant="bare">--font-mono</Code>
        </Text>
      </Row>
      <Row label="highlight" hint="Named identifiers deserving deliberate visual emphasis">
        <Text>
          The <Code variant="highlight">agent-7f2a3b</Code> identity resolved successfully.
        </Text>
      </Row>

      <Section title="Code — in context" />
      <Row label="path" hint="Use inline — prose sentence, file path">
        <Text size="supporting" color="muted">
          Config loaded from <Code>frontend/src/packages/terra/src/tokens/tokens.css</Code>
        </Text>
      </Row>
      <Row label="env var" hint="Use highlight — critical named variable">
        <Text size="supporting">
          Requires <Code variant="highlight">ORBIT_POLICY_ENGINE_URL</Code> to be set in your
          environment.
        </Text>
      </Row>
      <Row label="id" hint="Use bare — identifier inside a metadata row">
        <Text size="supporting" color="muted">
          Agent ID: <Code variant="bare">agt_01hzk9p3m2v8x4q7n0j5r6wc</Code>
        </Text>
      </Row>
    </div>
  ),
};

/**
 * All three components composed together in a realistic page fragment —
 * a single agent detail panel header.
 */
export const Composed: Story = {
  render: () => (
    <div className="max-w-lg space-y-3 rounded-lg border border-border bg-surface p-6">
      <div className="flex items-center gap-3">
        <Heading variant="section">deploy-agent</Heading>
        <Code variant="bare">agt_01hzk9p3m2v8x4q7n0j5r6wc</Code>
      </div>
      <Text color="muted">
        Responsible for pulling the latest release artifact and rolling it out across the staging
        fleet. Runs on a 5-minute cron schedule.
      </Text>
      <div className="flex gap-6 pt-2">
        <div>
          <Text size="caption" color="subtle" weight="emphasis">
            Policy
          </Text>
          <Text size="supporting">
            <Code variant="bare">deploy:staging:*</Code>
          </Text>
        </div>
        <div>
          <Text size="caption" color="subtle" weight="emphasis">
            Identity
          </Text>
          <Text size="supporting">
            <Code variant="bare">ci-runner@orbit.internal</Code>
          </Text>
        </div>
        <div>
          <Text size="caption" color="subtle" weight="emphasis">
            Last run
          </Text>
          <Text size="supporting" color="muted">
            4 minutes ago
          </Text>
        </div>
      </div>
    </div>
  ),
};
