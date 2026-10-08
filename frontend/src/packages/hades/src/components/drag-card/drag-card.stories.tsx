import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { DragCard } from './drag-card';

/**
 * DragCard
 *
 * A card that can be dragged to reorder a list. It shows whether it is being dragged or is
 * the place a dragged card would land. Drag events come from the browser as usual.
 */
const meta: Meta<typeof DragCard> = {
  title: 'Hades/Atoms/DragCard',
  component: DragCard,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof DragCard>;

function Row({ title, detail }: { title: string; detail: string }) {
  return (
    <>
      <Icon name="grip-vertical" color="muted" cursorGrab />
      <Stack gap="none">
        <Text size="supporting" weight="emphasis" mono>
          {title}
        </Text>
        <Text size="caption" color="muted">
          {detail}
        </Text>
      </Stack>
    </>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-md">
      <Example title="A priority list">
        <Stack gap="snug">
          {[
            { title: 'AWS Bedrock', detail: 'anthropic.claude-3-haiku' },
            { title: 'Vertex AI', detail: 'claude-3-5-sonnet' },
            { title: 'Azure OpenAI', detail: 'gpt-4o' },
          ].map((route) => (
            <DragCard key={route.title} draggable>
              <Row {...route} />
            </DragCard>
          ))}
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
        note="Set both from your own drag handlers. At most one card is dragging and one is the drop target at a time."
      >
        <Option name="default" use="A card at rest.">
          <DragCard>
            <Row title="AWS Bedrock" detail="Priority 1" />
          </DragCard>
        </Option>
        <Option name="dragging" use="The card being carried. It is dimmed and slightly smaller.">
          <DragCard dragging>
            <Row title="Vertex AI" detail="Priority 2" />
          </DragCard>
        </Option>
        <Option
          name="dropTarget"
          use="The card the dragged one is over. It gets an accent border and tint."
        >
          <DragCard dropTarget>
            <Row title="Azure OpenAI" detail="Priority 3" />
          </DragCard>
        </Option>
      </OptionTable>
    </div>
  ),
};
