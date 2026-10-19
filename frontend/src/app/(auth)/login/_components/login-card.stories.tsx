import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { Stack } from '@runkora/hades';
import { mockApi } from '@runkora/storybook/mock-api';
import { AlreadySignedIn } from './already-signed-in';
import { LoginCard } from './login-card';

/** The sign-in screen, centred on the page the way the auth layout does it. */
const meta: Meta<typeof LoginCard> = {
  title: 'Pages/Login',
  component: LoginCard,
  parameters: { layout: 'fullscreen', nextjs: { appDirectory: true } },
  decorators: [
    (Story) => (
      <Stack
        align="center"
        justify="center"
        minHeight="screen"
        padding="comfortable"
        style={{
          // The same glow the auth layout draws behind the card.
          background:
            'radial-gradient(60rem 28rem at 50% 0%, color-mix(in oklch, var(--color-accent) 9%, transparent), transparent)',
        }}
      >
        <Story />
      </Stack>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof LoginCard>;

const submit = async (canvasElement: HTMLElement, email: string) => {
  const canvas = within(canvasElement);
  const input = canvas.getByLabelText('Email');
  if (email) await userEvent.type(input, email);
  await userEvent.click(canvas.getByRole('button', { name: /Continue|Looking up/ }));
  return canvas;
};

const notFound = new Response(JSON.stringify({ code: 'not_found' }), { status: 404 });
const serverError = new Response(JSON.stringify({ code: 'server_error' }), { status: 500 });

export const Default: Story = { args: { message: null, next: '/' } };

/** Submitting with nothing typed. */
export const EmptyEmail: Story = {
  args: { message: null, next: '/' },
  play: async ({ canvasElement }) => {
    const canvas = await submit(canvasElement, '');
    await expect(canvas.getByText('Please enter your email address.')).toBeInTheDocument();
  },
};

/** An address without an @. */
export const InvalidEmail: Story = {
  args: { message: null, next: '/' },
  play: async ({ canvasElement }) => {
    const canvas = await submit(canvasElement, 'dev.example.com');
    await expect(canvas.getByText('Please enter a valid email address.')).toBeInTheDocument();
  },
};

/** The lookup is in flight. The request never answers, so the button stays busy. */
export const LookingUp: Story = {
  args: { message: null, next: '/' },
  decorators: [mockApi({ 'GET /api/auth/discover': () => new Promise(() => {}) })],
  play: async ({ canvasElement }) => {
    await submit(canvasElement, 'dev@acme.example');
  },
};

/** No organisation uses that email domain. */
export const UnknownOrganisation: Story = {
  args: { message: null, next: '/' },
  decorators: [mockApi({ 'GET /api/auth/discover': () => notFound.clone() })],
  play: async ({ canvasElement }) => {
    const canvas = await submit(canvasElement, 'dev@unknown.example');
    await expect(await canvas.findByText(/No organisation found/)).toBeInTheDocument();
  },
};

/** The lookup failed on the server. */
export const LookupFailed: Story = {
  args: { message: null, next: '/' },
  decorators: [mockApi({ 'GET /api/auth/discover': () => serverError.clone() })],
  play: async ({ canvasElement }) => {
    const canvas = await submit(canvasElement, 'dev@acme.example');
    await expect(
      await canvas.findByText(/Unable to look up your organisation/),
    ).toBeInTheDocument();
  },
};

/** Sent back to the sign-in page after the session ended. */
export const SessionExpired: Story = {
  args: { message: 'Your session expired. Please sign in again.', next: '/agents' },
};

/** The identity provider refused the request. */
export const AccessDenied: Story = {
  args: { message: 'Access was denied by the identity provider.', next: '/' },
};

/** Shown instead of the card when the visitor already has a session. */
export const AlreadySignedInCard: Story = {
  name: 'Already signed in',
  render: () => <AlreadySignedIn />,
};
