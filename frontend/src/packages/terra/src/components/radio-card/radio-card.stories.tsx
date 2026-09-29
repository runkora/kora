import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Grid } from '../layout/grid';
import { RadioGroup } from '../radio-group/radio-group';
import { RadioCard } from './radio-card';

const meta: Meta<typeof RadioCard> = {
  title: 'Terra/Molecules/RadioCard',
  component: RadioCard,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof RadioCard>;

const FRAMEWORKS = [
  { id: 'claude-code', name: 'Claude Code', desc: "Anthropic's agentic coding harness." },
  { id: 'codex', name: 'Codex', desc: "OpenAI's coding agent runtime." },
  { id: 'opencode', name: 'Open Code', desc: 'Open-source, model-agnostic agent.' },
];

function FrameworkPickerDemo() {
  const [value, setValue] = React.useState('claude-code');
  return (
    <RadioGroup value={value} onValueChange={setValue} aria-label="Framework">
      <Grid cols={3} gap="cozy">
        {FRAMEWORKS.map((f) => (
          <RadioCard
            key={f.id}
            value={f.id}
            selected={value === f.id}
            onSelect={setValue}
            icon="square-terminal"
            title={f.name}
            description={f.desc}
          />
        ))}
      </Grid>
    </RadioGroup>
  );
}

/** Three option cards in a 3-column grid inside a RadioGroup — the framework picker anatomy. */
export const FrameworkPicker: Story = {
  render: () => <FrameworkPickerDemo />,
};

function WithoutIconsDemo() {
  const [value, setValue] = React.useState('standard');
  return (
    <RadioGroup value={value} onValueChange={setValue} aria-label="Egress mode">
      <Grid cols={2} gap="cozy">
        <RadioCard
          value="standard"
          selected={value === 'standard'}
          onSelect={setValue}
          title="Standard egress"
          description="Only the declared connector hosts are reachable."
        />
        <RadioCard
          value="open"
          selected={value === 'open'}
          onSelect={setValue}
          title="Open egress"
          description="All outbound hosts allowed. Requires approval."
        />
      </Grid>
    </RadioGroup>
  );
}

/** Icon-less variant — the radio control alone anchors the top row. */
export const WithoutIcons: Story = {
  render: () => <WithoutIconsDemo />,
};

function DisabledOptionDemo() {
  const [value, setValue] = React.useState('delegated');
  return (
    <RadioGroup value={value} onValueChange={setValue} aria-label="Identity model">
      <Grid cols={2} gap="cozy">
        <RadioCard
          value="delegated"
          selected={value === 'delegated'}
          onSelect={setValue}
          icon="user-check"
          title="Delegated"
          description="Acts on behalf of the person who runs it. A much longer description to show that both cards keep the same structural height."
        />
        <RadioCard
          value="autonomous"
          selected={value === 'autonomous'}
          onSelect={setValue}
          icon="bot"
          title="Autonomous"
          description="Short description."
          disabled
        />
      </Grid>
    </RadioGroup>
  );
}

/** A disabled option dims, ignores clicks, and keeps equal height with its
 * siblings regardless of description length. */
export const DisabledOption: Story = {
  render: () => <DisabledOptionDemo />,
};
