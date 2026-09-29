import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Textarea } from './textarea';
import { Label } from '../label/label';

/**
 * Textarea
 *
 * A multi-line text box. It can grow as the person types.
 */
const meta: Meta<typeof Textarea> = {
  title: 'Terra/Atoms/Textarea',
  component: Textarea,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Textarea>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="With a label">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-description">Description</Label>
          <Textarea id="overview-description" rows={4} placeholder="What does this agent do?" />
        </div>
      </Example>
      <Example title="Grows as you type">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-notes">Notes</Label>
          <Textarea id="overview-notes" autoResize maxRows={6} rows={2} placeholder="Type here" />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='variant="default"' use="A bordered field for forms.">
          <Textarea aria-label="Description" rows={3} placeholder="Description" />
        </Option>
        <Option
          name='variant="bare"'
          use="No border or background. Use inside a box that draws its own border, such as a chat input."
        >
          <div className="rounded-2xl border border-border focus-within:border-accent">
            <Textarea
              variant="bare"
              aria-label="Message"
              rows={2}
              placeholder="Message the agent"
            />
          </div>
        </Option>
        <Option name="disabled" use="The field cannot be edited.">
          <Textarea aria-label="Description, disabled" rows={2} disabled defaultValue="Locked" />
        </Option>
        <Option name="hasError" use="Marks the value as invalid. Show a message next to it.">
          <Textarea aria-label="Description, invalid" rows={2} hasError defaultValue="Too short" />
        </Option>
        <Option name="autoResize" use="Grows with the text, up to maxRows lines, then scrolls.">
          <Textarea
            aria-label="Notes"
            autoResize
            maxRows={4}
            rows={2}
            placeholder="Type several lines"
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
