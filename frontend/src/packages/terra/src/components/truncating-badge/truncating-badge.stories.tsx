import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { TruncatingBadge } from './truncating-badge';

/**
 * TruncatingBadge
 *
 * A badge whose label shrinks to the space it has. A long label ends in an ellipsis and the full
 * text shows on hover. Use it in a table cell or a narrow row where names vary in length.
 */
const meta: Meta<typeof TruncatingBadge> = {
  title: 'Terra/Molecules/TruncatingBadge',
  component: TruncatingBadge,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof TruncatingBadge>;

const LONG = 'Automated Deployment Safety Reviewer';

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose" gap="relaxed">
      <Example title="Room to spare">
        <Stack direction="horizontal" minWidth="none">
          <TruncatingBadge label="Kubernetes" variant="secondary" textColor="muted" />
        </Stack>
      </Example>
      <Example title="Squeezed into a narrow row">
        <Stack direction="horizontal" maxWidth="card" minWidth="none">
          <TruncatingBadge label={LONG} variant="outline" textColor="foreground" />
        </Stack>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="variant">
        <Option name="secondary" use="A filled badge on a quiet background.">
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge label="Kubernetes" variant="secondary" textColor="muted" />
          </Stack>
        </Option>
        <Option name="outline" use="A badge with a border and no fill.">
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge label="Kubernetes" variant="outline" textColor="foreground" />
          </Stack>
        </Option>
        <Option name="elevated" use="A badge raised one step above its surface.">
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge label="Kubernetes" variant="elevated" textColor="foreground" />
          </Stack>
        </Option>
      </OptionTable>
      <OptionTable title="textColor">
        <Option name="muted" use="Secondary text.">
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge label="Kubernetes" variant="secondary" textColor="muted" />
          </Stack>
        </Option>
        <Option name="foreground" use="Main text.">
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge label="Kubernetes" variant="secondary" textColor="foreground" />
          </Stack>
        </Option>
      </OptionTable>
    </Stack>
  ),
};
