import type { Meta, StoryObj } from '@storybook/react';
import { ConfirmDialog } from './confirm-dialog';

const meta: Meta<typeof ConfirmDialog> = {
  title: 'App/Shared/ConfirmDialog',
  component: ConfirmDialog,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ConfirmDialog>;

export const Default: Story = {
  args: {
    open: true,
    title: 'Delete Role',
    description:
      'Remove the role "tenant-viewer" and all its scope mappings? This action cannot be undone.',
    confirmLabel: 'Delete',
    onConfirm: () => {},
    onClose: () => {},
  },
};

export const Confirming: Story = {
  args: {
    open: true,
    title: 'Revoke API key',
    description:
      'This key will be permanently revoked and any applications using it will lose access immediately.',
    confirmLabel: 'Revoke key',
    confirming: true,
    onConfirm: () => {},
    onClose: () => {},
  },
};

export const WithError: Story = {
  args: {
    open: true,
    title: 'Delete Grant',
    description: 'Remove this grant? This action cannot be undone.',
    confirmLabel: 'Delete',
    error: 'Failed to delete grant. The server returned an error.',
    onConfirm: () => {},
    onClose: () => {},
  },
};
