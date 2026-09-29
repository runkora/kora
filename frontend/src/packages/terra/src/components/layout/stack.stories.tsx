import type { Meta, StoryObj } from '@storybook/react';
import { Block, Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from './stack';

/**
 * Stack
 *
 * Lays children out in a line, with a set gap between them. Use it instead of
 * hand-written flex classes.
 */
const meta: Meta<typeof Stack> = {
  title: 'Terra/Layout/Stack',
  component: Stack,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Stack>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Vertical (default)">
        <Stack gap="comfortable">
          <Block>One</Block>
          <Block>Two</Block>
          <Block>Three</Block>
        </Stack>
      </Example>
      <Example title="Horizontal">
        <Stack direction="horizontal" gap="snug">
          <Block>One</Block>
          <Block>Two</Block>
          <Block>Three</Block>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Size options also take a value per breakpoint, for example direction={{ base: 'vertical', md: 'horizontal' }}."
      >
        <Option name='direction="vertical"' use="Children go top to bottom. The default.">
          <Stack gap="tight">
            <Block>One</Block>
            <Block>Two</Block>
          </Stack>
        </Option>
        <Option name='direction="horizontal"' use="Children sit in a row.">
          <Stack direction="horizontal" gap="tight">
            <Block>One</Block>
            <Block>Two</Block>
          </Stack>
        </Option>
        <Option
          name='gap="tight" / "loose"'
          use="Space between children. The names are on the Spacing page."
        >
          <Stack gap="snug">
            <Stack direction="horizontal" gap="tight">
              <Block>tight</Block>
              <Block>tight</Block>
            </Stack>
            <Stack direction="horizontal" gap="loose">
              <Block>loose</Block>
              <Block>loose</Block>
            </Stack>
          </Stack>
        </Option>
        <Option name='align="center"' use="Line children up across the stack. Default is stretch.">
          <Stack direction="horizontal" gap="tight" align="center">
            <Block>Short</Block>
            <div className="flex h-16 items-center rounded-md border border-border-subtle bg-surface-elevated px-3">
              <span className="text-xs text-muted">Tall</span>
            </div>
          </Stack>
        </Option>
        <Option name='justify="between"' use="Push children to the two ends of the row.">
          <Frame>
            <Stack direction="horizontal" justify="between">
              <Block>Left</Block>
              <Block>Right</Block>
            </Stack>
          </Frame>
        </Option>
        <Option name="wrap" use="Let children move to a new line when the row is full.">
          <div className="w-52">
            <Frame>
              <Stack direction="horizontal" gap="tight" wrap>
                {['One', 'Two', 'Three', 'Four', 'Five'].map((n) => (
                  <Block key={n}>{n}</Block>
                ))}
              </Stack>
            </Frame>
          </div>
        </Option>
        <Option name="grow" use="Let one child fill the space left over.">
          <Frame>
            <Stack direction="horizontal" gap="tight">
              <Block>Fixed</Block>
              <Stack grow>
                <Block>Grows</Block>
              </Stack>
            </Stack>
          </Frame>
        </Option>
        <Option name="asChild" use="Render your own element, such as a list, in place of a div.">
          <Stack asChild gap="tight">
            <ul className="list-none">
              <li>
                <Block>List item</Block>
              </li>
              <li>
                <Block>List item</Block>
              </li>
            </ul>
          </Stack>
        </Option>
      </OptionTable>
    </div>
  ),
};
