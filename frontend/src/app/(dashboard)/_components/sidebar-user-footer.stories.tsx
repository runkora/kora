import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { Text } from '@runkora/hades';
import { dashboardPage, MOCK_USER, sessionRoutes } from '@runkora/storybook/dashboard-frame';
import type { MockRoutes } from '@runkora/storybook/mock-api';

/**
 * The account menu in the sidebar footer. With two or more tenants the trigger names the active
 * tenant under the email, and the menu lists the tenants between "Signed in as" and the session
 * actions. The session comes from the mock API, as on the real page.
 */
const meta: Meta = {
  title: 'Pages/Dashboard/Account menu',
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true },
    viewport: {
      options: { phone: { name: 'Phone 390', styles: { width: '390px', height: '844px' } } },
    },
  },
  render: () => <Text color="muted">Page content</Text>,
};
export default meta;
type Story = StoryObj;

const NAMES = [
  'Platform',
  'Data',
  'Machine Learning',
  'Payments',
  'Search',
  'Mobile',
  'Security',
  'Growth',
];

function tenants(names: string[]) {
  return names.map((name, i) => ({
    id: `kora-tenant-example-${i}`,
    displayName: `Example ${name}`,
    active: i === 0,
  }));
}

/** The session with `list` as its eligible set, plus any extra routes. */
function withTenants(list: ReturnType<typeof tenants>, extra: MockRoutes = {}) {
  const session = sessionRoutes()['GET /api/auth/session'] as object;
  return dashboardPage({ 'GET /api/auth/session': { ...session, tenants: list }, ...extra });
}

/** Opens the account menu from the footer trigger. */
async function openMenu(canvasElement: HTMLElement) {
  const page = within(canvasElement.ownerDocument.body);
  await userEvent.click(await page.findByRole('button', { name: new RegExp(MOCK_USER.email) }));
  // The scripted open leaves a focus ring on the first item, which a real pointer open does not show.
  await page.findByRole('menuitem', { name: 'View profile' });
  await new Promise(requestAnimationFrame);
  (canvasElement.ownerDocument.activeElement as HTMLElement | null)?.blur();
  return page;
}

/** Opens the menu and picks the second tenant. */
async function pickSecond(canvasElement: HTMLElement) {
  const page = await openMenu(canvasElement);
  await userEvent.click(await page.findByRole('button', { name: 'Example Data' }));
  (canvasElement.ownerDocument.activeElement as HTMLElement | null)?.blur();
}

export const OneTenant: Story = {
  decorators: [withTenants(tenants(['Platform']))],
  play: ({ canvasElement }) => openMenu(canvasElement).then(() => undefined),
};

export const TwoTenants: Story = {
  decorators: [withTenants(tenants(NAMES.slice(0, 2)))],
  play: ({ canvasElement }) => openMenu(canvasElement).then(() => undefined),
};

export const ManyTenants: Story = {
  decorators: [withTenants(tenants(NAMES))],
  play: ({ canvasElement }) => openMenu(canvasElement).then(() => undefined),
};

export const LongName: Story = {
  decorators: [withTenants(tenants(['Platform Engineering and Developer Experience', 'Data']))],
  play: ({ canvasElement }) => openMenu(canvasElement).then(() => undefined),
};

/** Discovery never answers and the switch stays starting. */
export const Switching: Story = {
  decorators: [
    withTenants(tenants(NAMES.slice(0, 2)), {
      'GET /api/auth/discover': () => new Promise(() => {}),
    }),
  ],
  play: ({ canvasElement }) => pickSecond(canvasElement),
};

/** Discovery fails and the switch never starts. */
export const SwitchFailed: Story = {
  decorators: [
    withTenants(tenants(NAMES.slice(0, 2)), {
      'GET /api/auth/discover': () =>
        new Response(JSON.stringify({ code: 'server_error' }), { status: 500 }),
    }),
  ],
  play: ({ canvasElement }) => pickSecond(canvasElement),
};

/** At 390px the footer lives in the navigation drawer and the menu opens as a bottom sheet. */
export const MobileSheet: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  decorators: [withTenants(tenants(NAMES.slice(0, 3)))],
  play: async ({ canvasElement }) => {
    const page = within(canvasElement.ownerDocument.body);
    await userEvent.click(await page.findByRole('button', { name: 'Open navigation' }));
    await userEvent.click(await page.findByRole('button', { name: new RegExp(MOCK_USER.email) }));
  },
};
