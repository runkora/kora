import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { SaveBar } from './save-bar';

const meta: Meta<typeof SaveBar> = {
  title: 'Terra/Molecules/SaveBar',
  component: SaveBar,
  parameters: {
    // The bar is position:fixed, so give the canvas room and let it pin itself.
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SaveBar>;

export const Dirty: Story = {
  args: {
    visible: true,
    saving: false,
    success: false,
    error: null,
    onSave: () => {},
    onDiscard: () => {},
  },
};

export const Saving: Story = {
  args: { ...Dirty.args, saving: true },
};

export const Success: Story = {
  args: { ...Dirty.args, visible: false, success: true },
};

export const WithError: Story = {
  args: { ...Dirty.args, error: 'Provisioning failed — quota exceeded.' },
};

function LifecycleDemo() {
  const [dirty, setDirty] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [success, setSuccess] = React.useState(false);

  const save = () => {
    setSaving(true);
    setTimeout(() => {
      setSaving(false);
      setDirty(false);
      setSuccess(true);
      setTimeout(() => setSuccess(false), 2000);
    }, 1200);
  };

  return (
    <Stack gap="cozy" padding="relaxed" align="start">
      <Text size="supporting" color="muted">
        Make a change to raise the bar, then save and watch it settle.
      </Text>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          setDirty(true);
          setSuccess(false);
        }}
      >
        Make a change
      </Button>
      <SaveBar
        visible={dirty}
        saving={saving}
        success={success}
        onSave={save}
        onDiscard={() => setDirty(false)}
      />
    </Stack>
  );
}

/** Full lifecycle: dirty → saving → saved → gone. */
export const Lifecycle: Story = {
  render: () => <LifecycleDemo />,
};
