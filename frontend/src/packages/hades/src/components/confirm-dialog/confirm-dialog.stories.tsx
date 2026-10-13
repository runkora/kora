import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Text } from '../typography/typography';
import { ConfirmDialog } from './confirm-dialog';
import type { ConfirmDialogProps } from './confirm-dialog';

/**
 * A modal that asks the user to confirm an action that cannot be undone. It has a cancel button
 * and a red confirm button.
 */
const meta: Meta<typeof ConfirmDialog> = {
  title: 'Hades/Molecules/ConfirmDialog',
  component: ConfirmDialog,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ConfirmDialog>;

type SampleProps = Partial<
  Pick<ConfirmDialogProps, 'confirming' | 'error' | 'size' | 'children'>
> & { label: string };

function Sample({ label, ...rest }: SampleProps) {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <ConfirmDialog
        open={open}
        onClose={() => setOpen(false)}
        onConfirm={() => setOpen(false)}
        title="Delete role?"
        description="The viewer role and its settings will be removed. This cannot be undone."
        confirmLabel="Delete"
        {...rest}
      />
    </>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Confirming a delete">
        <Text size="supporting" color="muted">
          The dialog opens over the page. Select the button to see it.
        </Text>
        <Sample label="Delete role" />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Each button opens its own sample. Title, description, and confirmLabel are required."
      >
        <Option name="Default" use="For a short confirmation with one sentence of explanation.">
          <Sample label="Open dialog" />
        </Option>
        <Option
          name="children"
          use="Shows extra detail under the description, such as what will go."
        >
          <Sample label="Open with detail">
            <Text size="supporting" mono>
              viewer
            </Text>
          </Sample>
        </Option>
        <Option name="confirming" use="Disables both buttons while the action runs.">
          <Sample label="Open while working" confirming />
        </Option>
        <Option name="error" use="Shows a red message when the action fails.">
          <Sample label="Open with error" error="The role could not be deleted. Try again." />
        </Option>
        <Option name='size="md"' use="A wider dialog for longer text. The default is sm.">
          <Sample label="Open wide dialog" size="md" />
        </Option>
      </OptionTable>
    </div>
  ),
};
