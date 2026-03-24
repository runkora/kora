import type { Meta, StoryObj } from '@storybook/react';
import { Section } from './section';

const meta: Meta<typeof Section> = {
  title: 'Terra/Layout/Section',
  component: Section,
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
    docs: {
      description: {
        component:
          'Content grouping block for use inside `PageLayout`. Provides consistent horizontal padding (`px-6`), vertical rhythm (`py-8`), and an optional heading + description header. Stack multiple sections to build a structured page without ad-hoc spacing overrides.',
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof Section>;

// ── Stories ───────────────────────────────────────────────────────────────────

/**
 * A section with both a heading and a description. This is the full treatment —
 * used when the content group needs a clear label and supporting context.
 */
export const WithHeadingAndDescription: Story = {
  name: 'Heading + description',
  args: {
    heading: 'Active agents',
    description: 'Agents currently running in this workspace.',
    children: (
      <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
        Agent table placeholder
      </div>
    ),
  },
};

/**
 * A section with a heading only — no description. The most common variant
 * in dashboard pages where the heading alone is sufficient context.
 */
export const HeadingOnly: Story = {
  name: 'Heading only',
  args: {
    heading: 'Overview',
    children: (
      <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
        Stats grid placeholder
      </div>
    ),
  },
};

/**
 * A section with no heading or description. Still provides the standard
 * `px-6 py-8` padding — useful for top-of-page content like empty states,
 * onboarding banners, or full-bleed components that supply their own heading.
 */
export const ContentOnly: Story = {
  name: 'Content only (no heading)',
  args: {
    children: (
      <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
        Content with consistent padding, no heading
      </div>
    ),
  },
};

/**
 * Multiple sections stacked vertically inside a simulated page shell. Shows
 * how sections build rhythm in a real `PageLayout` main area.
 */
export const MultipleStacked: Story = {
  name: 'Multiple sections stacked',
  render: () => (
    <div className="flex flex-col">
      <Section heading="Overview" description="High-level metrics for this workspace.">
        <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
          Stats grid
        </div>
      </Section>
      <Section heading="Active agents">
        <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
          Agent table
        </div>
      </Section>
      <Section>
        <div className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted">
          Trailing content (no heading)
        </div>
      </Section>
    </div>
  ),
};
