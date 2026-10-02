import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Input } from '../input/input';
import { Label } from '../label/label';
import { Textarea } from '../textarea/textarea';
import { Field, FieldError, FieldHint } from './field';

/**
 * Field
 *
 * Groups a label, a control and its help or error text with even spacing.
 */
const meta: Meta<typeof Field> = {
  title: 'Terra/Atoms/Field',
  component: Field,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Field>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="A complete form">
        <div className="flex flex-col gap-5">
          <Field>
            <Label htmlFor="form-name">Agent name</Label>
            <Input id="form-name" placeholder="policy-auditor" />
            <FieldHint>Lowercase letters, numbers and hyphens.</FieldHint>
          </Field>
          <Field>
            <Label htmlFor="form-desc">Description</Label>
            <Textarea id="form-desc" rows={3} placeholder="What does this agent do?" />
          </Field>
          <Field>
            <Label htmlFor="form-token">Service token</Label>
            <Input id="form-token" type="password" hasError defaultValue="bad" />
            <FieldError>Token is invalid or expired.</FieldError>
          </Field>
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="Field" use="A label and a control, stacked.">
          <Field className="max-w-xs">
            <Label htmlFor="opt-email">Email</Label>
            <Input id="opt-email" type="email" placeholder="you@example.com" />
          </Field>
        </Option>
        <Option
          name="FieldHint"
          use="Help text under the control, shown before the person submits."
        >
          <Field className="max-w-xs">
            <Label htmlFor="opt-hint">Agent name</Label>
            <Input id="opt-hint" placeholder="policy-auditor" />
            <FieldHint>Lowercase letters, numbers and hyphens.</FieldHint>
          </Field>
        </Option>
        <Option name="FieldError" use="Says what is wrong. Pair it with hasError on the control.">
          <Field className="max-w-xs">
            <Label htmlFor="opt-error">Password</Label>
            <Input id="opt-error" type="password" hasError defaultValue="short" />
            <FieldError>Password must be at least 8 characters.</FieldError>
          </Field>
        </Option>
        <Option
          name="FieldError visible={false}"
          use="Hides the message but keeps its space, so the form does not jump when an error appears."
        >
          <Field className="max-w-xs">
            <Label htmlFor="opt-hidden">API token</Label>
            <Input id="opt-hidden" placeholder="sk-..." />
            <FieldError visible={false}>Invalid token format.</FieldError>
          </Field>
        </Option>
      </OptionTable>
    </div>
  ),
};
