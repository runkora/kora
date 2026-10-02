import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { VocabularyTab } from './vocabulary-tab';
import { VOCABULARY } from './policy-mock-data';
import { policyParameters, withPolicy } from './policy-story-setup';

const meta: Meta<typeof VocabularyTab> = {
  title: 'Pages/Policy/Vocabulary',
  component: VocabularyTab,
  parameters: policyParameters('/policy/vocabulary'),
  decorators: withPolicy(),
  args: { initialScopes: VOCABULARY, initialError: null },
};

export default meta;
type Story = StoryObj<typeof VocabularyTab>;

export const Default: Story = {};

export const Empty: Story = {
  args: { initialScopes: [] },
};

export const LoadError: Story = {
  args: { initialScopes: [], initialError: 'policy-service unavailable (503)' },
};

/** The mcp resource type chip selected. */
export const FilteredByResourceType: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: /^mcp/ }));
  },
};

/** A search with no hits. */
export const NoMatches: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.type(
      await within(canvasElement).findByPlaceholderText('Filter scopes...'),
      'zzz',
    );
  },
};
