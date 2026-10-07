import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, waitFor, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import ChatPage from './page';

/** The chat assistant page. Replies are canned in the page itself, so no API mocks are needed. */
const meta: Meta<typeof ChatPage> = {
  title: 'Pages/Chat',
  component: ChatPage,
  decorators: [dashboardPage()],
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/chat' } },
  },
};

export default meta;
type Story = StoryObj<typeof ChatPage>;

/** The welcome screen with its suggestion cards. */
export const Empty: Story = {};

/** A suggestion picked and waiting in the composer. */
export const SuggestionSelected: Story = {
  play: async ({ canvasElement }) => {
    const card = await within(canvasElement).findByRole('button', {
      name: /Policy engine deep dive/,
    });
    await userEvent.click(card);
  },
};

/** A prompt sent and the full reply streamed in. */
export const Conversation: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const card = await canvas.findByRole('button', { name: /Policy engine deep dive/ });
    await userEvent.click(card);
    const send = await canvas.findByRole('button', { name: /send/i });
    await userEvent.click(send);
    await waitFor(() => canvas.getAllByRole('button', { name: 'Copy message' }), {
      timeout: 15000,
    });
    // Let the page's own auto-scroll settle first.
    await new Promise((resolve) => setTimeout(resolve, 300));
    // The thread auto-scrolls to the end of the reply; return to the top so the prompt is visible.
    for (const el of canvasElement.querySelectorAll('*')) {
      if (el.scrollHeight > el.clientHeight && getComputedStyle(el).overflowY === 'auto') {
        el.scrollTop = 0;
      }
    }
  },
};
