import type { Meta, StoryObj } from '@storybook/react';
import { Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Icon } from './icon';
import type { IconName } from './icon';
import { ICON_REGISTRY } from './registry';

/**
 * Icon
 *
 * Shows any icon from Hades' list of icons. Size and colour are set with props, not classes.
 */
const meta: Meta<typeof Icon> = {
  title: 'Hades/Atoms/Icon',
  component: Icon,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Icon>;

const NAMES = (Object.keys(ICON_REGISTRY) as IconName[]).sort();

export const Overview: Story = {
  render: () => (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-x-4 gap-y-6">
      {NAMES.map((name) => (
        <Stack key={name} align="center" gap="tight">
          <Icon name={name} size="md" />
          <Text size="micro" color="muted" mono>
            {name}
          </Text>
        </Stack>
      ))}
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Icons are hidden from screen readers unless you pass a label."
      >
        <Option name='size="2xs" to "xl"' use="Six sizes, from 14px to 32px. The default is sm.">
          <Stack direction="horizontal" gap="comfortable" align="center">
            {(['2xs', 'xs', 'sm', 'md', 'lg', 'xl'] as const).map((size) => (
              <Icon key={size} name="shield" size={size} />
            ))}
          </Stack>
        </Option>
        <Option name='color="default" / "accent"' use="Normal text colour, or the brand colour.">
          <Stack direction="horizontal" gap="snug">
            <Icon name="shield" size="md" color="default" />
            <Icon name="shield" size="md" color="accent" />
          </Stack>
        </Option>
        <Option name='color="muted" / "subtle"' use="Softer colours for supporting icons.">
          <Stack direction="horizontal" gap="snug">
            <Icon name="shield" size="md" color="muted" />
            <Icon name="shield" size="md" color="subtle" />
          </Stack>
        </Option>
        <Option
          name='color="status-healthy" and other status colours'
          use="Show the state of something."
        >
          <Stack direction="horizontal" gap="snug">
            <Icon name="circle-check" size="md" color="status-healthy" />
            <Icon name="triangle-alert" size="md" color="status-degraded" />
            <Icon name="circle-x" size="md" color="status-error" />
            <Icon name="clock" size="md" color="status-pending" />
          </Stack>
        </Option>
        <Option name='color="rose"' use="A rose tint, for example a liked heart.">
          <Icon name="heart" size="md" color="rose" />
        </Option>
        <Option name="filled" use="Fill the shape in. Works best on shapes like the heart.">
          <Icon name="heart" size="md" color="rose" filled />
        </Option>
        <Option name="container" use="Put the icon in a rounded box. The box sets the colour.">
          <Icon name="bot" size="md" container />
        </Option>
        <Option
          name='containerVariant="accent" / "surface" / "surface-subtle" / "muted"'
          use="The box style: tinted, bordered, bordered and quiet, or plain."
        >
          <Stack direction="horizontal" gap="snug">
            <Icon name="bot" size="md" container containerVariant="accent" />
            <Icon name="bot" size="md" container containerVariant="surface" />
            <Icon name="bot" size="md" container containerVariant="surface-subtle" />
            <Icon name="bot" size="md" container containerVariant="muted" />
          </Stack>
        </Option>
        <Option name="circle" use="Make the box round. Use it for empty states and avatars.">
          <Icon name="bot" size="md" container containerVariant="surface-subtle" circle />
        </Option>
        <Option name="rotate={90}" use="Turn the icon, for example a chevron on an open row.">
          <Stack direction="horizontal" gap="snug">
            <Icon name="chevron-right" size="md" rotate={0} />
            <Icon name="chevron-right" size="md" rotate={90} />
          </Stack>
        </Option>
        <Option
          name='label="Delete agent"'
          use="Give a screen reader a name when the icon stands alone."
        >
          <Icon name="trash" size="md" label="Delete agent" />
        </Option>
      </OptionTable>
    </div>
  ),
};
