/** @tier 3 — skills-scoped. Preview of the overview loading layout. */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { MarketplaceResultsSkeleton } from './skills-results-skeleton';

const meta: Meta<typeof MarketplaceResultsSkeleton> = {
  title: 'Pages/Marketplace/ResultsSkeleton',
  component: MarketplaceResultsSkeleton,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof MarketplaceResultsSkeleton>;

export const Default: Story = {
  render: () => (
    <Stack gap="comfortable">
      <MarketplaceResultsSkeleton />
    </Stack>
  ),
};
