import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { SaveBar, type SaveBarProps } from './save-bar';

/**
 * A bar that tells the user they have unsaved changes and lets them save or
 * discard. It sits at the bottom centre of the screen; here it is held inside
 * a box so you can see it.
 */
const meta: Meta<typeof SaveBar> = {
  title: 'Terra/Molecules/SaveBar',
  component: SaveBar,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SaveBar>;

const noop = () => {};

/** A transform makes the box the reference for the bar's fixed position. The bar keeps its natural width, so its text does not wrap. */
function Stage({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="relative h-24 overflow-hidden rounded-md border border-dashed border-border [&>*]:w-max"
      style={{ transform: 'translate(0)' }}
    >
      {children}
    </div>
  );
}

function Sample(props: Partial<SaveBarProps>) {
  return (
    <Stage>
      <SaveBar visible onSave={noop} onDiscard={noop} {...props} />
    </Stage>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Unsaved changes">
        <Sample />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="The bar shows when visible or success is true. Otherwise it renders nothing."
      >
        <Option name="visible" use="The user has unsaved changes. Shows Save and Discard.">
          <Sample />
        </Option>
        <Option name="saving" use="A save is running. Both buttons are disabled.">
          <Sample saving />
        </Option>
        <Option
          name="error"
          use="The last save failed. The message shows in the bar and the buttons stay available."
        >
          <Sample error="Could not save. Try again." />
        </Option>
        <Option name="success" use="The last save worked. The buttons are hidden.">
          <Sample visible={false} success />
        </Option>
        <Option
          name="dirtyLabel, saveLabel, discardLabel"
          use="Change the wording to match the page."
        >
          <Sample
            dirtyLabel="Draft not published"
            saveLabel="Publish"
            discardLabel="Delete draft"
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
