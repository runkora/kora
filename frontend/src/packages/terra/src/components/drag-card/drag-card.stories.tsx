/**
 * DragCard
 *
 * A draggable card that expresses visual drag-state through typed boolean
 * props rather than dynamic className strings. Pass standard HTML drag event
 * handlers (`draggable`, `onDragStart`, `onDragOver`, `onDrop`, etc.) via
 * the normal props spread.
 *
 * The three visual states:
 *
 * - **Default** — neutral surface with a subtle border.
 * - **Dragging** — the card being dragged: dimmed (40% opacity) and
 *   scaled down slightly (98%) to signal it is "in flight".
 * - **Drop target** — a stationary card that the dragged item is hovering
 *   over: accent border with a faint accent tint to indicate it accepts a
 *   drop.
 *
 * ```tsx
 * <DragCard
 *   draggable
 *   dragging={draggingIdx === idx}
 *   dropTarget={overIdx === idx && draggingIdx !== null}
 *   onDragStart={() => handleDragStart(idx)}
 *   onDrop={() => handleDrop(idx)}
 * >
 *   …content…
 * </DragCard>
 * ```
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { DragCard } from './drag-card';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof DragCard> = {
  title: 'Terra/Atoms/DragCard',
  component: DragCard,
  parameters: {
    layout: 'padded',
  },
  tags: ['autodocs'],
  argTypes: {
    dragging: {
      control: 'boolean',
      description: 'True while this card is being dragged — dims and scales it.',
    },
    dropTarget: {
      control: 'boolean',
      description: 'True when a dragged card is hovering over this one — shows accent border/tint.',
    },
  },
};

export default meta;
type Story = StoryObj<typeof DragCard>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Interactive story — toggle `dragging` and `dropTarget` in the Controls panel. */
export const Default: Story = {
  args: {
    dragging: false,
    dropTarget: false,
  },
  render: (args) => (
    <DragCard {...args} className="max-w-sm">
      <Icon name="grip-vertical" color="muted" cursorGrab />
      <Stack gap="none">
        <Text size="supporting" weight="emphasis" mono>
          anthropic / claude-3-5-haiku
        </Text>
        <Text size="caption" color="muted">
          Priority 1 · Enabled
        </Text>
      </Stack>
    </DragCard>
  ),
};

/**
 * All three visual states rendered simultaneously for comparison.
 * In real usage only one card is `dragging` and at most one is `dropTarget`
 * at any given time.
 */
export const States: Story = {
  render: () => (
    <Stack gap="snug" className="max-w-sm">
      <DragCard>
        <Icon name="grip-vertical" color="muted" cursorGrab />
        <Stack gap="none">
          <Text size="supporting" weight="emphasis" mono>
            Default
          </Text>
          <Text size="caption" color="muted">
            Idle — not being dragged
          </Text>
        </Stack>
      </DragCard>

      <DragCard dragging>
        <Icon name="grip-vertical" color="muted" cursorGrab />
        <Stack gap="none">
          <Text size="supporting" weight="emphasis" mono>
            Dragging
          </Text>
          <Text size="caption" color="muted">
            This card is in flight
          </Text>
        </Stack>
      </DragCard>

      <DragCard dropTarget>
        <Icon name="grip-vertical" color="muted" cursorGrab />
        <Stack gap="none">
          <Text size="supporting" weight="emphasis" mono>
            Drop target
          </Text>
          <Text size="caption" color="muted">
            Dragged card hovering over this slot
          </Text>
        </Stack>
      </DragCard>
    </Stack>
  ),
};

/**
 * A realistic route priority list with three entries, illustrating how
 * DragCard composes with StepDot (priority number), Toggle, and action
 * buttons.
 */
export const RouteList: Story = {
  name: 'Route list composition',
  render: () => (
    <Stack gap="snug" className="max-w-md">
      {[
        { provider: 'AWS Bedrock', model: 'anthropic.claude-3-haiku', idx: 1 },
        { provider: 'Vertex AI', model: 'claude-3-5-sonnet@20241022', idx: 2 },
        { provider: 'Azure OpenAI', model: 'gpt-4o', idx: 3 },
      ].map(({ provider, model, idx }) => (
        <DragCard key={idx}>
          <Stack direction="vertical" align="center" gap="tight" shrink paddingTop="snug">
            <Icon name="grip-vertical" color="muted" cursorGrab />
            <Text size="micro" color="muted" mono>
              {idx}
            </Text>
          </Stack>
          <Stack direction="vertical" grow gap="tight" minWidth="none">
            <Text size="supporting" weight="emphasis">
              {provider}
            </Text>
            <Text size="caption" color="muted" mono>
              {model}
            </Text>
          </Stack>
        </DragCard>
      ))}
    </Stack>
  ),
};
