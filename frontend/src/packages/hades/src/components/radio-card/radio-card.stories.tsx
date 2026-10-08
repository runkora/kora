import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Grid } from '../layout/grid';
import { RadioGroup } from '../radio-group/radio-group';
import { RadioCard } from './radio-card';

/**
 * RadioCard
 *
 * A card for choosing one option out of a few. It must sit inside a RadioGroup.
 */
const meta: Meta<typeof RadioCard> = {
  title: 'Hades/Molecules/RadioCard',
  component: RadioCard,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof RadioCard>;

const FRAMEWORKS = [
  { id: 'claude-code', name: 'Claude Code', desc: 'Anthropic coding agent.' },
  { id: 'codex', name: 'Codex', desc: 'OpenAI coding agent.' },
  { id: 'opencode', name: 'Open Code', desc: 'Open source, works with any model.' },
];

function FrameworkPicker() {
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

/** One card in its own group, so each row in the option table can be tried on its own. */
function Single({
  name,
  start,
  ...card
}: {
  name: string;
  start: boolean;
  title: string;
  description?: string;
  icon?: NonNullable<React.ComponentProps<typeof RadioCard>['icon']>;
  disabled?: boolean;
}) {
  const [value, setValue] = React.useState(start ? 'a' : '');
  return (
    <div className="max-w-xs">
      <RadioGroup value={value} onValueChange={setValue} aria-label={name}>
        <RadioCard value="a" selected={value === 'a'} onSelect={setValue} {...card} />
      </RadioGroup>
    </div>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-3xl">
      <Example title="Pick a framework">
        <FrameworkPicker />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="selected: false" use="Not chosen.">
          <Single
            name="Not selected"
            start={false}
            icon="bot"
            title="Autonomous"
            description="Runs on its own."
          />
        </Option>
        <Option name="selected: true" use="Chosen. The card is tinted and the icon is coloured.">
          <Single
            name="Selected"
            start
            icon="user-check"
            title="Delegated"
            description="Acts for the person who runs it."
          />
        </Option>
        <Option name="icon" use="A small icon at the top left. Leave it out for a plainer card.">
          <Single
            name="No icon"
            start={false}
            title="Standard egress"
            description="Only listed hosts are reachable."
          />
        </Option>
        <Option name="description" use="One line of help under the title.">
          <Single name="Title only" start={false} icon="bot" title="Autonomous" />
        </Option>
        <Option name="disabled" use="Dimmed. It cannot be chosen.">
          <Single
            name="Disabled"
            start={false}
            disabled
            icon="bot"
            title="Autonomous"
            description="Not available on this plan."
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
