import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon';
import { Text } from '../typography';
import { FieldRow } from './field-row';

/**
 * FieldRow
 *
 * A sunken row for a value people read or copy, such as a URL or a masked secret. It looks different from an input so nobody tries to edit it.
 */
const meta: Meta<typeof FieldRow> = {
  title: 'Hades/Molecules/FieldRow',
  component: FieldRow,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof FieldRow>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-lg space-y-8">
      <Example title="A value to copy">
        <FieldRow
          leading={<Icon name="webhook" size="2xs" color="subtle" />}
          actions={
            <Button variant="ghost" size="xs">
              <Icon name="copy" size="2xs" />
              Copy
            </Button>
          }
        >
          <Text size="caption" mono>
            https://api.orbit.example/v1/ingest/gh-main
          </Text>
        </FieldRow>
      </Example>
      <Example title="A secret">
        <FieldRow
          leading={<Icon name="lock" size="2xs" color="subtle" />}
          actions={
            <>
              <Button variant="ghost" size="xs">
                <Icon name="eye" size="2xs" />
                Reveal
              </Button>
              <Button variant="ghost" size="xs">
                <Icon name="refresh" size="2xs" />
                Rotate
              </Button>
            </>
          }
        >
          <Text size="caption" mono color="muted">
            whsec_••••••••••••••••••••
          </Text>
        </FieldRow>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="Only the value.">
          <FieldRow>
            <Text size="caption" mono>
              trg_01HZ9K3VQ8N2P4
            </Text>
          </FieldRow>
        </Option>
        <Option name="leading" use="An icon or short marker before the value.">
          <FieldRow leading={<Icon name="globe" size="2xs" color="subtle" />}>
            <Text size="caption" mono>
              https://api.orbit.example/v1
            </Text>
          </FieldRow>
        </Option>
        <Option name="actions" use="Buttons after the value, such as Copy.">
          <FieldRow
            actions={
              <Button variant="ghost" size="xs">
                <Icon name="copy" size="2xs" />
                Copy
              </Button>
            }
          >
            <Text size="caption" mono>
              trg_01HZ9K3VQ8N2P4
            </Text>
          </FieldRow>
        </Option>
        <Option name="long value" use="A value that does not fit is cut off with an ellipsis.">
          <FieldRow leading={<Icon name="key" size="2xs" color="subtle" />}>
            <Text size="caption" mono>
              https://api.orbit.example/v1/ingest/a-very-long-source-name-that-will-not-fit-here
            </Text>
          </FieldRow>
        </Option>
      </OptionTable>
    </div>
  ),
};
