import type { Meta, StoryObj } from '@storybook/react';
import { Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Field, FieldHint } from '../field/field';
import { Input } from '../input/input';
import { Label } from '../label/label';
import { Form } from './form';

/**
 * Form
 *
 * Wraps a set of fields and a submit button in a form element. Fields are
 * stacked with an even gap.
 */
const meta: Meta<typeof Form> = {
  title: 'Hades/Atoms/Form',
  component: Form,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Form>;

function SampleFields({ prefix }: { prefix: string }) {
  return (
    <>
      <Field>
        <Label htmlFor={`${prefix}-name`}>Agent name</Label>
        <Input id={`${prefix}-name`} defaultValue="Support triage" />
      </Field>
      <Field>
        <Label htmlFor={`${prefix}-owner`}>Owner</Label>
        <Input id={`${prefix}-owner`} placeholder="Team or person" />
        <FieldHint>The owner is told when the agent fails.</FieldHint>
      </Field>
    </>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-md">
      <Example title="Default">
        <Form aria-label="New agent" gap="comfortable" onSubmit={(event) => event.preventDefault()}>
          <SampleFields prefix="overview" />
          <div>
            <Button type="submit">Deploy agent</Button>
          </div>
        </Form>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='gap="tight"' use="Fields sit close together. Good for short forms.">
          <Form aria-label="Tight gap" gap="tight" onSubmit={(event) => event.preventDefault()}>
            <SampleFields prefix="tight" />
          </Form>
        </Option>
        <Option name='gap="loose"' use="More room between fields. Good for long forms.">
          <Form aria-label="Loose gap" gap="loose" onSubmit={(event) => event.preventDefault()}>
            <SampleFields prefix="loose" />
          </Form>
        </Option>
        <Option name='maxWidth="card"' use="Limits the form width and centres it in its container.">
          <Frame>
            <Form
              aria-label="Card width"
              maxWidth="card"
              onSubmit={(event) => event.preventDefault()}
            >
              <SampleFields prefix="card" />
            </Form>
          </Frame>
        </Option>
        <Option
          name="onSubmit"
          use="Runs when the form is sent. Press Enter in a field or use the submit button."
        >
          <Form aria-label="Submit" onSubmit={(event) => event.preventDefault()}>
            <Field>
              <Label htmlFor="submit-name">Agent name</Label>
              <Input id="submit-name" defaultValue="Support triage" />
            </Field>
            <div>
              <Button type="submit">Deploy agent</Button>
            </div>
          </Form>
        </Option>
        <Option
          name="noValidate"
          use="Turns off the browser's own checks so your code can show its own messages."
        >
          <Form aria-label="No validation" noValidate onSubmit={(event) => event.preventDefault()}>
            <Field>
              <Label htmlFor="validate-name">Agent name</Label>
              <Input id="validate-name" required />
            </Field>
            <div>
              <Button type="submit">Deploy agent</Button>
            </div>
          </Form>
        </Option>
      </OptionTable>
    </div>
  ),
};
