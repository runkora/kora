import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { SectionNav } from './section-nav';
import type { SectionNavSection } from './section-nav';

const meta: Meta<typeof SectionNav> = {
  title: 'Terra/Molecules/SectionNav',
  component: SectionNav,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SectionNav>;

const SECTIONS: SectionNavSection[] = [
  { key: 'identity', title: 'Identity', sub: 'Name & description', icon: 'user' },
  { key: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
  { key: 'tools', title: 'Tools & access', sub: 'Servers & scopes', icon: 'wrench' },
  { key: 'network', title: 'Network access', sub: 'Connectors & egress', icon: 'network' },
  { key: 'guardrails', title: 'Guardrails', sub: 'Limits & capacity', icon: 'shield' },
  { key: 'danger', title: 'Danger zone', sub: 'Delete agent', icon: 'triangle-alert' },
];

function ConfigurationRailDemo() {
  const [active, setActive] = React.useState('identity');
  return (
    <Stack minWidth="compact" maxWidth="compact">
      <SectionNav sections={SECTIONS} active={active} onSelect={setActive} />
    </Stack>
  );
}

/** The configuration rail anatomy: numbered entries, accent icon tile on the active one. */
export const ConfigurationRail: Story = {
  render: () => <ConfigurationRailDemo />,
};

function WithoutSubtitlesDemo() {
  const [active, setActive] = React.useState('general');
  return (
    <Stack minWidth="compact" maxWidth="compact">
      <SectionNav
        sections={[
          { key: 'general', title: 'General', icon: 'settings' },
          { key: 'members', title: 'Members', icon: 'user' },
          { key: 'security', title: 'Security', icon: 'shield' },
        ]}
        active={active}
        onSelect={setActive}
      />
    </Stack>
  );
}

/** Entries without the `sub` line stay compact. */
export const WithoutSubtitles: Story = {
  render: () => <WithoutSubtitlesDemo />,
};
