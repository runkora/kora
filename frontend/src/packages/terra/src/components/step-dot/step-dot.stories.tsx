/**
 * StepDot + StepConnector
 *
 * A pair of primitives for building vertical step-by-step flows (onboarding
 * wizards, setup guides). `StepDot` renders a circular badge for the step
 * number; `StepConnector` renders a 1 px vertical line that grows to fill
 * the space between dots.
 *
 * Compose them inside a vertical `Stack` with no gap:
 *
 * ```tsx
 * <Stack align="center" gap="none">
 *   <StepDot>1</StepDot>
 *   <StepConnector />
 *   <StepDot>2</StepDot>
 *   <StepConnector />
 *   <StepDot variant="surface">3</StepDot>
 * </Stack>
 * ```
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { StepDot, StepConnector } from './index';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof StepDot> = {
  title: 'Terra/Atoms/StepDot',
  component: StepDot,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'radio',
      options: ['accent', 'surface'],
      description: 'accent = filled accent colour (current step); surface = muted (inactive step)',
    },
    size: {
      control: 'radio',
      options: ['md', 'sm'],
      description: 'md = 24 px for onboarding steps; sm = 16 px for compact priority indicators',
    },
  },
};

export default meta;
type Story = StoryObj<typeof StepDot>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Interactive story — use Controls to explore variant and size. */
export const Default: Story = {
  args: {
    children: '1',
    variant: 'accent',
    size: 'md',
  },
};

/**
 * Both variants side-by-side at the default (md) size.
 * `accent` marks the active / current step; `surface` marks upcoming steps.
 */
export const Variants: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Stack gap="tight" align="center">
        <StepDot variant="accent">1</StepDot>
        <Text size="micro" color="muted">
          accent
        </Text>
      </Stack>
      <Stack gap="tight" align="center">
        <StepDot variant="surface">2</StepDot>
        <Text size="micro" color="muted">
          surface
        </Text>
      </Stack>
    </Stack>
  ),
};

/**
 * Both sizes available. `md` (24 px) is the default for onboarding wizard
 * steps. `sm` (16 px) is used for compact inline priority indicators such as
 * the route drag card in the Model Gateway catalog.
 */
export const Sizes: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Stack gap="tight" align="center">
        <StepDot size="md">1</StepDot>
        <Text size="micro" color="muted">
          md — 24 px
        </Text>
      </Stack>
      <Stack gap="tight" align="center">
        <StepDot size="sm">1</StepDot>
        <Text size="micro" color="muted">
          sm — 16 px
        </Text>
      </Stack>
    </Stack>
  ),
};

/**
 * A complete three-step flow. `StepConnector` fills the space between dots;
 * the outer `Stack` is `align="center" gap="none"` so connectors sit flush.
 *
 * Pair the dot column with a content column in a horizontal Stack to build
 * the full setup guide layout.
 */
export const StepFlow: Story = {
  name: 'Step flow — with StepConnector',
  render: () => (
    <Stack direction="horizontal" gap="comfortable" className="w-[360px]">
      {/* Dot + connector column */}
      <Stack align="center" gap="none">
        <StepDot>1</StepDot>
        <StepConnector />
        <StepDot>2</StepDot>
        <StepConnector />
        <StepDot variant="surface">3</StepDot>
      </Stack>

      {/* Content column */}
      <Stack gap="relaxed">
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis">
            Create an API key
          </Text>
          <Text size="caption" color="muted">
            Head to the Keys tab and generate a scoped credential for the gateway.
          </Text>
        </Stack>
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis">
            Point your agent at the gateway
          </Text>
          <Text size="caption" color="muted">
            Set <code className="font-mono text-xs">OPENAI_BASE_URL</code> to the Orbit endpoint.
          </Text>
        </Stack>
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis">
            Verify the connection
          </Text>
          <Text size="caption" color="muted">
            Run a quick curl to confirm routing and credential validity.
          </Text>
        </Stack>
      </Stack>
    </Stack>
  ),
};
