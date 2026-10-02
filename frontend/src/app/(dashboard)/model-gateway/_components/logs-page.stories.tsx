import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRoutes } from '@orbit/storybook/mock-api';
import ModelGatewayLayout from '../layout';
import { LogsTabShell } from './logs-tab-shell';
import {
  FAILED_LOG_RECORD,
  KEY_IDS,
  LOG_RECORDS,
  MANY_LOG_RECORDS,
  PATS,
} from './gateway-mock-data';

/** Answers the client-side refetch the logs tab makes when the key filter or Refresh changes. */
const usageRoutes: MockRoutes = {
  'GET /api/usage': ({ url }: { url: URL }) => {
    const key = url.searchParams.get('key');
    const records = key ? LOG_RECORDS.filter((r) => r.token_id === key) : LOG_RECORDS;
    return { records, hasNextPage: false, nextCursor: null, pats: PATS };
  },
};

/** The inference request log: a sortable table, outcome filter and a per-request detail modal. */
const meta: Meta<typeof LogsTabShell> = {
  title: 'Pages/Models/Logs',
  component: LogsTabShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/logs' } },
  },
  decorators: [dashboardPage(usageRoutes, ModelGatewayLayout)],
};

export default meta;
type Story = StoryObj<typeof LogsTabShell>;

const populated = {
  initialRecords: LOG_RECORDS,
  initialHasNextPage: false,
  initialNextCursor: null,
  initialPats: PATS,
  initialError: null,
};

export const Default: Story = { args: populated };

export const Empty: Story = {
  args: { ...populated, initialRecords: [] },
};

export const LoadFailed: Story = {
  args: {
    ...populated,
    initialRecords: [],
    initialError: 'Failed to reach inference gateway',
  },
};

/** Fifty rows with a next page, to see the table scroll and the pager. */
export const ManyRequests: Story = {
  args: {
    ...populated,
    initialRecords: MANY_LOG_RECORDS,
    initialHasNextPage: true,
    initialNextCursor: 'cur_9f2a41',
  },
};

/** Opens the detail modal for a request that failed upstream. */
export const FailedRequestDetail: Story = {
  args: populated,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByText(FAILED_LOG_RECORD.id));
    // The failure details sit below the fold of the modal, so scroll them into view.
    const dialog = await within(document.body).findByRole('dialog');
    dialog.querySelectorAll('*').forEach((el) => {
      if (el.scrollHeight > el.clientHeight + 20) el.scrollTop = el.scrollHeight;
    });
  },
};

/** Opens the detail modal for a successful, cached request. */
export const SuccessRequestDetail: Story = {
  args: populated,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const first = LOG_RECORDS[0];
    if (first !== undefined) await userEvent.click(await canvas.findByText(first.id));
  },
};

/** Narrows the table to failed requests with the outcome control. */
export const ErrorsOnly: Story = {
  args: populated,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: /^Error/ }));
  },
};

/** Deep link from the API keys page: logs for one key. */
export const FilteredToKey: Story = {
  args: {
    ...populated,
    selectedKeyId: KEY_IDS.codeReviewer,
    initialRecords: LOG_RECORDS.filter((r) => r.token_id === KEY_IDS.codeReviewer),
  },
};
