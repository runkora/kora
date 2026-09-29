import type { Meta, StoryObj } from '@storybook/react';
import { Code, Heading, Text } from './typography';
import { readFontFamily, useLive } from '../../tokens/foundation-ui';

/**
 * Typography
 *
 * Font families, headings, text and code. Sizes and weights shown are read
 * from the rendered samples, so they match the active brand.
 */
const meta = {
  title: 'Terra/Foundation/Typography',
  parameters: { layout: 'padded' },
} satisfies Meta;

export default meta;
type Story = StoryObj;

// ─── Helpers ──────────────────────────────────────────────────────────────────

const GRID = 'grid grid-cols-[120px_1fr_120px_260px] items-baseline gap-6';

const readSpec = (el: HTMLElement) => {
  const style = getComputedStyle(el.firstElementChild ?? el);
  return `${style.fontSize} / ${style.fontWeight}`;
};

/** One table row: name, live sample, live size and weight, and when to use it. */
function Row({ name, use, children }: { name: string; use: string; children: React.ReactNode }) {
  const [ref, spec] = useLive(readSpec);
  return (
    <div className={`${GRID} border-b border-border-subtle py-4 last:border-0`}>
      <Text size="supporting" mono color="muted">
        {name}
      </Text>
      <div ref={ref} className="min-w-0">
        {children}
      </div>
      <Text size="caption" mono color="subtle">
        {spec}
      </Text>
      <Text size="supporting" color="muted">
        {use}
      </Text>
    </div>
  );
}

function Table({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <Heading variant="label">{title}</Heading>
      <div>
        <div className={`${GRID} border-b border-border py-2`}>
          <Text size="caption" color="subtle">
            Name
          </Text>
          <Text size="caption" color="subtle">
            Sample
          </Text>
          <Text size="caption" color="subtle">
            Size / weight
          </Text>
          <Text size="caption" color="subtle">
            Use
          </Text>
        </div>
        {children}
      </div>
    </section>
  );
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/** One font family with sample text and the font stack it resolves to. */
function Family({ cssVar, utility, sample }: { cssVar: string; utility: string; sample: string }) {
  const [ref, stack] = useLive(readFontFamily);
  return (
    <div className="overflow-hidden rounded-lg border border-border-subtle">
      <div ref={ref} className={`space-y-2 bg-surface-elevated p-6 ${utility}`}>
        <div className="text-4xl">{sample}</div>
        <div className="text-sm text-muted">
          ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz 0123456789
        </div>
      </div>
      <div className="space-y-1 p-3 font-mono text-[11px] leading-snug text-subtle">
        <div className="text-muted">{stack}</div>
        <div>{cssVar}</div>
        <div>{utility}</div>
      </div>
    </div>
  );
}

/** The two font families and the font stack each one uses under the active brand. */
export const Families: Story = {
  render: () => (
    <div className="grid max-w-3xl gap-6">
      <Family
        cssVar="--font-sans"
        utility="font-sans"
        sample="The quick brown fox jumps over the lazy dog"
      />
      <Family cssVar="--font-mono" utility="font-mono" sample="agent.id = agt_x7k2m" />
    </div>
  ),
};

/** The six heading variants and the three heading colours. */
export const Headings: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <Table title="Variants">
        <Row name="display" use="Hero title on a landing or sign-in screen.">
          <Heading variant="display">Run agents in production</Heading>
        </Row>
        <Row name="title" use="Page title. One per page.">
          <Heading variant="title">Agents</Heading>
        </Row>
        <Row name="section" use="Main sections of a page. The most common heading.">
          <Heading variant="section">Agent configuration</Heading>
        </Row>
        <Row name="subsection" use="A group inside a section.">
          <Heading variant="subsection">Identity providers</Heading>
        </Row>
        <Row name="label" use="Card titles and sidebar group names.">
          <Heading variant="label">OAuth 2.0 settings</Heading>
        </Row>
        <Row name="metric" use="Large numbers on stat cards.">
          <Heading variant="metric">4,821</Heading>
        </Row>
      </Table>

      <Table title="Colours">
        <Row name="foreground" use="Default.">
          <Heading variant="section">Agent configuration</Heading>
        </Row>
        <Row name="muted" use="Secondary headings.">
          <Heading variant="section" color="muted">
            Agent configuration
          </Heading>
        </Row>
        <Row name="accent" use="Emphasis. Use sparingly.">
          <Heading variant="section" color="accent">
            Agent configuration
          </Heading>
        </Row>
      </Table>
    </div>
  ),
};

const SENTENCE = 'Every action is checked before it runs.';

/** Text sizes, colours and weights, plus the options for numbers, ids and long text. */
export const TextStyles: Story = {
  name: 'Text',
  render: () => (
    <div className="max-w-5xl space-y-10">
      <Table title="Sizes">
        <Row name="micro" use="Tiny tags and counters.">
          <Text size="micro">{SENTENCE}</Text>
        </Row>
        <Row name="caption" use="Timestamps and fine print.">
          <Text size="caption">{SENTENCE}</Text>
        </Row>
        <Row name="supporting" use="Descriptions, helper text and table details.">
          <Text size="supporting">{SENTENCE}</Text>
        </Row>
        <Row name="body" use="Default text for most of the interface.">
          <Text size="body">{SENTENCE}</Text>
        </Row>
        <Row name="lead" use="Introduction under a page or section title.">
          <Text size="lead">{SENTENCE}</Text>
        </Row>
      </Table>

      <Table title="Colours">
        <Row name="foreground" use="Main text.">
          <Text>{SENTENCE}</Text>
        </Row>
        <Row name="muted" use="Secondary text.">
          <Text color="muted">{SENTENCE}</Text>
        </Row>
        <Row name="subtle" use="Placeholders and captions.">
          <Text color="subtle">{SENTENCE}</Text>
        </Row>
        <Row name="accent" use="Links and active items.">
          <Text color="accent">{SENTENCE}</Text>
        </Row>
        <Row name="status-healthy" use="Healthy or successful values.">
          <Text color="status-healthy">{SENTENCE}</Text>
        </Row>
        <Row name="status-degraded" use="Warnings.">
          <Text color="status-degraded">{SENTENCE}</Text>
        </Row>
        <Row name="status-error" use="Errors and failures.">
          <Text color="status-error">{SENTENCE}</Text>
        </Row>
      </Table>

      <Table title="Weights">
        <Row name="regular" use="Body text.">
          <Text weight="regular">{SENTENCE}</Text>
        </Row>
        <Row name="emphasis" use="Labels and field values.">
          <Text weight="emphasis">{SENTENCE}</Text>
        </Row>
        <Row name="strong" use="Column headings and key names.">
          <Text weight="strong">{SENTENCE}</Text>
        </Row>
        <Row name="heavy" use="Rare. Strong emphasis only.">
          <Text weight="heavy">{SENTENCE}</Text>
        </Row>
      </Table>

      <Table title="Options">
        <Row name="mono" use="Ids, versions and other machine values.">
          <Text mono>agt_x7k2m</Text>
        </Row>
        <Row name="tabular" use="Numbers that change, so digits stay aligned.">
          <Text tabular>1,234,567</Text>
        </Row>
        <Row name="uppercase" use="Small labels above a group of content.">
          <Text size="caption" weight="strong" transform="uppercase" color="subtle">
            Recent runs
          </Text>
        </Row>
        <Row name="truncate" use="Cuts long text with an ellipsis. The container needs a width.">
          <div className="w-48">
            <Text truncate>agent-run-2026-09-29-very-long-identifier</Text>
          </div>
        </Row>
      </Table>
    </div>
  ),
};

/** Inline code styles. */
export const CodeStyles: Story = {
  name: 'Code',
  render: () => (
    <div className="max-w-5xl">
      <Table title="Variants">
        <Row name="inline" use="Inside a sentence. The default.">
          <Code>config/dev.env</Code>
        </Row>
        <Row name="bare" use="Inside a table or container that already has styling.">
          <Code variant="bare">agt_x7k2m</Code>
        </Row>
        <Row
          name="highlight"
          use="A named value that needs attention, such as an environment variable."
        >
          <Code variant="highlight">ORBIT_API_KEY</Code>
        </Row>
      </Table>
    </div>
  ),
};
