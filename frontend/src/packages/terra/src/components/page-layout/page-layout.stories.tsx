import type { Meta, StoryObj } from '@storybook/react';
import { Text } from '../typography/typography';
import { Stack } from '../layout/stack';
import { Navigation } from '../navigation/navigation';
import { NavItem } from '../nav-item/nav-item';
import { PageLayout } from './page-layout';
import { Section } from '../section/section';

const meta: Meta = {
  title: 'Terra/Layout/PageLayout',
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

const NAV_ITEMS = ['Overview', 'Agents', 'Policies', 'Credentials', 'Events', 'Settings'];

const SidebarContent = () => (
  <Navigation>
    <Stack gap="none" paddingInline="snug" paddingBlock="snug">
      {NAV_ITEMS.map((item) => (
        <NavItem key={item} asChild>
          <a href="#">{item}</a>
        </NavItem>
      ))}
    </Stack>
  </Navigation>
);

const HeaderContent = () => (
  <Stack direction="horizontal" align="center" justify="between">
    <Text size="supporting" weight="strong" color="foreground">
      Orbit
    </Text>
    <Text size="micro" color="muted">
      v0.1.0
    </Text>
  </Stack>
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
            <Text size="supporting" color="muted">
              Agent table placeholder
            </Text>
          </div>
        </Section>
        <Section heading="Recent events" description="Last 50 events from the event spine.">
          <div className="rounded-lg border border-border-subtle bg-surface h-40 flex items-center justify-center">
            <Text size="supporting" color="muted">
              Event log placeholder
            </Text>
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
            <Text size="supporting" color="muted">
              Settings panel placeholder
            </Text>
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

/** Full shell with sidebar footer — user identity row pinned below the nav */
export const WithSidebarFooter: Story = {
  render: () => (
    <div className="h-screen">
      <PageLayout
        sidebar={<SidebarContent />}
        sidebarFooter={
          <div className="px-4 py-3 flex items-center gap-2">
            <div className="size-6 rounded-full bg-surface-elevated flex items-center justify-center">
              <Text size="micro" color="muted">U</Text>
            </div>
            <Text size="caption" color="muted" className="flex-1 truncate">user@example.com</Text>
          </div>
        }
        header={<HeaderContent />}
      >
        <Section heading="Dashboard">
          <div className="rounded-lg border border-border-subtle bg-surface h-40 flex items-center justify-center">
            <Text size="supporting" color="muted">Main content</Text>
          </div>
        </Section>
      </PageLayout>
    </div>
  ),
};
