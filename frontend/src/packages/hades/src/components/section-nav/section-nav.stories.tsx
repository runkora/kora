import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { SectionNav } from './section-nav';
import type { SectionNavSection } from './section-nav';

/**
 * SectionNav
 *
 * A vertical list of the sections on one page, with the current section
 * highlighted. Use it as the side rail of a settings or setup page.
 */
const meta: Meta<typeof SectionNav> = {
  title: 'Hades/Molecules/SectionNav',
  component: SectionNav,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
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

function Rail({ sections, initial }: { sections: SectionNavSection[]; initial: string }) {
  const [active, setActive] = React.useState(initial);
  return (
    <Stack maxWidth="compact">
      <SectionNav sections={sections} active={active} onSelect={setActive} />
    </Stack>
  );
}

function ConfigurationRail() {
  const [active, setActive] = React.useState('behaviour');
  const current = SECTIONS.find((s) => s.key === active);
  return (
    <Stack direction="horizontal" gap="relaxed" align="start">
      <Stack minWidth="compact" maxWidth="compact">
        <SectionNav sections={SECTIONS} active={active} onSelect={setActive} />
      </Stack>
      <Stack gap="tight">
        <Text weight="heavy">{current?.title}</Text>
        <Text size="supporting" color="muted">
          {current?.sub}
        </Text>
      </Stack>
    </Stack>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Agent configuration rail">
        <ConfigurationRail />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Every sample is live. Inactive entries show their number. The active entry shows its icon."
      >
        <Option name="sub" use="A second line under the title. Optional.">
          <Rail sections={SECTIONS.slice(0, 3)} initial="identity" />
        </Option>
        <Option name="no sub" use="Titles only, for short and obvious sections.">
          <Rail
            initial="general"
            sections={[
              { key: 'general', title: 'General', icon: 'settings' },
              { key: 'members', title: 'Members', icon: 'user' },
              { key: 'security', title: 'Security', icon: 'shield' },
            ]}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
