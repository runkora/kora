import type { Meta, StoryObj } from '@storybook/react';
import { Text } from '../typography/typography';
import { PageLayout, Section } from './page-layout';

const meta: Meta = {
  title: 'Terra/Organisms/PageLayout',
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj;

const SidebarContent = () => (
  <nav className="flex flex-col gap-1 p-3">
    {['Overview', 'Agents', 'Policies', 'Credentials', 'Events', 'Settings'].map((item) => (
      <a
        key={item}
        href="#"
        className="px-3 py-2 rounded-md text-sm text-muted hover:text-foreground hover:bg-surface-elevated transition-colors"
      >
        {item}
      </a>
    ))}
  </nav>
);

const HeaderContent = () => (
  <div className="flex items-center justify-between w-full">
    <span className="text-sm font-semibold text-foreground tracking-tight">Orbit</span>
    <span className="text-xs text-muted">v0.1.0</span>
  </div>
);

/** Full shell — sidebar + header + main content */
export const FullShell: Story = {
  render: () => (
    <div className="h-screen">
      <PageLayout sidebar={<SidebarContent />} header={<HeaderContent />}>
        <Section
          heading="Active agents"
          description="Agents currently running in your control plane."
        >
          <div className="rounded-lg border border-border-subtle bg-surface h-40 flex items-center justify-center">
            <span className="text-sm text-muted">Agent table placeholder</span>
          </div>
        </Section>
        <Section heading="Recent events" description="Last 50 events from the event spine.">
          <div className="rounded-lg border border-border-subtle bg-surface h-40 flex items-center justify-center">
            <span className="text-sm text-muted">Event log placeholder</span>
          </div>
        </Section>
      </PageLayout>
    </div>
  ),
};

/** No sidebar — full-width content layout */
export const NoSidebar: Story = {
  render: () => (
    <div className="h-screen">
      <PageLayout header={<HeaderContent />}>
        <Section heading="Settings" description="Manage your Orbit control plane configuration.">
          <div className="rounded-lg border border-border-subtle bg-surface h-48 flex items-center justify-center">
            <span className="text-sm text-muted">Settings panel placeholder</span>
          </div>
        </Section>
      </PageLayout>
    </div>
  ),
};

/** Section in isolation — heading + description + children */
export const SectionOnly: Story = {
  render: () => (
    <div className="bg-background min-h-screen">
      <Section
        heading="Credential broker"
        description="Scoped, time-limited credentials issued to verified agents."
      >
        <div className="grid grid-cols-3 gap-4">
          {['AWS', 'GCP', 'Azure'].map((provider) => (
            <div key={provider} className="rounded-lg border border-border-subtle bg-surface p-4">
              <Text size="supporting" weight="emphasis">
                {provider}
              </Text>
              <Text size="caption" color="muted" className="mt-1">
                12 active leases
              </Text>
            </div>
          ))}
        </div>
      </Section>
    </div>
  ),
};

/** Section with no heading — pure content wrapper */
export const SectionNoHeading: Story = {
  render: () => (
    <div className="bg-background min-h-screen">
      <Section>
        <Text size="supporting" color="muted">
          This section has no heading — useful for hero regions or content that supplies its own
          heading hierarchy.
        </Text>
      </Section>
    </div>
  ),
};
