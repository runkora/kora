import type { Meta, StoryObj } from '@storybook/react';

import { SidebarLayout } from './sidebar-layout';

const meta: Meta<typeof SidebarLayout> = {
  title: 'Terra/Layout/SidebarLayout',
  component: SidebarLayout,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  argTypes: {
    sidebarPosition: { control: 'select', options: ['start', 'end'] },
    gap: {
      control: 'select',
      options: ['none', 'tight', 'snug', 'cozy', 'comfortable', 'relaxed', 'loose', 'spacious'],
    },
    align: { control: 'select', options: ['start', 'center', 'end', 'stretch'] },
  },
};
export default meta;

type Story = StoryObj<typeof SidebarLayout>;

// ── Shared placeholder blocks ─────────────────────────────────────────────────

function MainPlaceholder() {
  return (
    <div
      style={{
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 'var(--radius-lg)',
        padding: 24,
        minHeight: 200,
        color: 'var(--color-text-subtle)',
        fontSize: 13,
      }}
    >
      Main content (fluid — 1fr)
    </div>
  );
}

function SidebarPlaceholder() {
  return (
    <div
      style={{
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 'var(--radius-lg)',
        padding: 24,
        color: 'var(--color-text-subtle)',
        fontSize: 13,
      }}
    >
      Sidebar (fixed — detail-panel / 372px)
    </div>
  );
}

// ── Stories ───────────────────────────────────────────────────────────────────

/** Default: `detail-panel` token, sidebar on the right, relaxed gap. */
export const Default: Story = {
  args: {
    sidebarWidth: 'detail-panel',
    sidebarPosition: 'end',
    gap: 'relaxed',
    align: 'start',
  },
  render: (args) => (
    <SidebarLayout {...args}>
      <SidebarLayout.Main>
        <MainPlaceholder />
      </SidebarLayout.Main>
      <SidebarLayout.Sidebar>
        <SidebarPlaceholder />
      </SidebarLayout.Sidebar>
    </SidebarLayout>
  ),
};

/** Sidebar on the left — `sidebarPosition="start"`. */
export const SidebarLeft: Story = {
  args: {
    sidebarWidth: 'detail-panel',
    sidebarPosition: 'start',
    gap: 'relaxed',
    align: 'start',
  },
  render: (args) => (
    <SidebarLayout {...args}>
      <SidebarLayout.Sidebar>
        <SidebarPlaceholder />
      </SidebarLayout.Sidebar>
      <SidebarLayout.Main>
        <MainPlaceholder />
      </SidebarLayout.Main>
    </SidebarLayout>
  ),
};

/** Numeric escape hatch — `sidebarWidth={300}` when no token fits. */
export const CustomWidth: Story = {
  args: {
    sidebarWidth: 300,
    sidebarPosition: 'end',
    gap: 'comfortable',
    align: 'start',
  },
  render: (args) => (
    <SidebarLayout {...args}>
      <SidebarLayout.Main>
        <MainPlaceholder />
      </SidebarLayout.Main>
      <SidebarLayout.Sidebar>
        <div
          style={{
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border-subtle)',
            borderRadius: 'var(--radius-lg)',
            padding: 24,
            color: 'var(--color-text-subtle)',
            fontSize: 13,
          }}
        >
          Sidebar (custom — 300px)
        </div>
      </SidebarLayout.Sidebar>
    </SidebarLayout>
  ),
};

/** String escape hatch — `sidebarWidth="20rem"` for arbitrary CSS values. */
export const StringWidth: Story = {
  args: {
    sidebarWidth: '20rem',
    sidebarPosition: 'end',
    gap: 'comfortable',
    align: 'start',
  },
  render: (args) => (
    <SidebarLayout {...args}>
      <SidebarLayout.Main>
        <MainPlaceholder />
      </SidebarLayout.Main>
      <SidebarLayout.Sidebar>
        <div
          style={{
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border-subtle)',
            borderRadius: 'var(--radius-lg)',
            padding: 24,
            color: 'var(--color-text-subtle)',
            fontSize: 13,
          }}
        >
          Sidebar (string — 20rem)
        </div>
      </SidebarLayout.Sidebar>
    </SidebarLayout>
  ),
};

/** Sticky sidebar — apply `position: sticky` to the inner Stack, not the layout shell. */
export const StickySidebar: Story = {
  render: () => (
    <div
      style={{
        height: 400,
        overflow: 'auto',
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 'var(--radius-lg)',
      }}
    >
      <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
        <SidebarLayout.Main>
          <div
            style={{
              background: 'var(--color-surface)',
              border: '1px solid var(--color-border-subtle)',
              borderRadius: 'var(--radius-lg)',
              padding: 24,
              minHeight: 800,
              color: 'var(--color-text-subtle)',
              fontSize: 13,
            }}
          >
            Tall main content — scroll to see the sidebar stick
          </div>
        </SidebarLayout.Main>
        <SidebarLayout.Sidebar>
          <div
            style={{
              background: 'var(--color-surface)',
              border: '1px solid var(--color-border-subtle)',
              borderRadius: 'var(--radius-lg)',
              padding: 24,
              position: 'sticky',
              top: 0,
              color: 'var(--color-text-subtle)',
              fontSize: 13,
            }}
          >
            Sticky sidebar — stays visible while main scrolls
          </div>
        </SidebarLayout.Sidebar>
      </SidebarLayout>
    </div>
  ),
};

/** All `align` values. */
export const AllAlignments: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 32 }}>
      {(['start', 'center', 'end', 'stretch'] as const).map((align) => (
        <div key={align}>
          <div style={{ color: 'var(--color-text-subtle)', fontSize: 12, marginBottom: 8 }}>
            align=&quot;{align}&quot;
          </div>
          <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align={align}>
            <SidebarLayout.Main>
              <div
                style={{
                  background: 'var(--color-surface)',
                  border: '1px solid var(--color-border-subtle)',
                  borderRadius: 'var(--radius-lg)',
                  padding: 24,
                  minHeight: 120,
                  color: 'var(--color-text-subtle)',
                  fontSize: 13,
                }}
              >
                Main
              </div>
            </SidebarLayout.Main>
            <SidebarLayout.Sidebar>
              <div
                style={{
                  background: 'var(--color-surface)',
                  border: '1px solid var(--color-border-subtle)',
                  borderRadius: 'var(--radius-lg)',
                  padding: 24,
                  color: 'var(--color-text-subtle)',
                  fontSize: 13,
                }}
              >
                Sidebar
              </div>
            </SidebarLayout.Sidebar>
          </SidebarLayout>
        </div>
      ))}
    </div>
  ),
};
