import type { Meta, StoryObj } from '@storybook/react';
import { Field, FieldHint, FieldError } from './field';
import { Label } from '../label/label';
import { Input } from '../input/input';
import { Textarea } from '../textarea/textarea';

const meta: Meta<typeof Field> = {
  title: 'Terra/Atoms/Field',
  component: Field,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Field>;

export const Default: Story = {
  render: () => (
    <div className="w-80">
      <Field>
        <Label htmlFor="email">Email address</Label>
        <Input id="email" type="email" placeholder="you@example.com" />
      </Field>
    </div>
  ),
};

export const WithHint: Story = {
  render: () => (
    <div className="w-80">
      <Field>
        <Label htmlFor="agent-name">Agent name</Label>
        <Input id="agent-name" placeholder="e.g. apollo-3" />
        <FieldHint>Must be lowercase letters, numbers and hyphens only.</FieldHint>
      </Field>
    </div>
  ),
};

export const WithError: Story = {
  render: () => (
    <div className="w-80">
      <Field>
        <Label htmlFor="password">Password</Label>
        <Input id="password" type="password" hasError defaultValue="short" />
        <FieldError>Password must be at least 8 characters.</FieldError>
      </Field>
    </div>
  ),
};

export const HiddenError: Story = {
  render: () => (
    <div className="w-80">
      <Field>
        <Label htmlFor="token">API token</Label>
        <Input id="token" placeholder="sk-..." />
        <FieldError visible={false}>Invalid token format.</FieldError>
      </Field>
    </div>
  ),
};

export const TextareaField: Story = {
  render: () => (
    <div className="w-80">
      <Field>
        <Label htmlFor="description">Description</Label>
        <Textarea id="description" placeholder="Describe what this agent does…" rows={4} />
        <FieldHint>Keep it concise — under 200 characters.</FieldHint>
      </Field>
    </div>
  ),
};

export const CompleteForm: Story = {
  render: () => (
    <div className="w-80 flex flex-col gap-5">
      <Field>
        <Label htmlFor="form-name">Agent name</Label>
        <Input id="form-name" placeholder="e.g. policy-auditor" />
        <FieldHint>Unique identifier within your workspace.</FieldHint>
      </Field>
      <Field>
        <Label htmlFor="form-desc">Description</Label>
        <Textarea id="form-desc" placeholder="What does this agent do?" rows={3} />
      </Field>
      <Field>
        <Label htmlFor="form-token">Service token</Label>
        <Input id="form-token" type="password" hasError defaultValue="bad" />
        <FieldError>Token is invalid or expired.</FieldError>
      </Field>
    </div>
  ),
};
