import type { Meta, StoryObj } from '@storybook/react';
import { NativeSelect } from './native-select';

const meta: Meta<typeof NativeSelect> = {
  title: 'Terra/Atoms/NativeSelect',
  component: NativeSelect,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    hasError: { control: 'boolean' },
    disabled: { control: 'boolean' },
    mono: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof NativeSelect>;

export const Default: Story = {
  render: () => (
    <NativeSelect defaultValue="bedrock" className="w-56">
      <option value="bedrock">AWS Bedrock</option>
      <option value="vertexai">Vertex AI</option>
      <option value="openai">OpenAI</option>
    </NativeSelect>
  ),
};

/** optgroup support — useful for model catalogs grouped by provider */
export const WithOptgroup: Story = {
  render: () => (
    <NativeSelect defaultValue="claude-3" className="w-56">
      <optgroup label="Anthropic">
        <option value="claude-3">claude-3-opus</option>
        <option value="claude-haiku">claude-haiku</option>
      </optgroup>
      <optgroup label="OpenAI">
        <option value="gpt-4">gpt-4o</option>
        <option value="gpt-3.5">gpt-3.5-turbo</option>
      </optgroup>
    </NativeSelect>
  ),
};

export const ErrorState: Story = {
  render: () => (
    <NativeSelect hasError defaultValue="" className="w-56">
      <option value="" disabled>
        Select a provider
      </option>
      <option value="bedrock">AWS Bedrock</option>
      <option value="vertexai">Vertex AI</option>
    </NativeSelect>
  ),
};

/** Mono — use for region or identifier values */
export const Mono: Story = {
  render: () => (
    <NativeSelect mono defaultValue="us-east-1" className="w-56">
      <option value="us-east-1">us-east-1</option>
      <option value="us-west-2">us-west-2</option>
      <option value="eu-west-1">eu-west-1</option>
    </NativeSelect>
  ),
};

export const Disabled: Story = {
  render: () => (
    <NativeSelect disabled defaultValue="bedrock" className="w-56">
      <option value="bedrock">AWS Bedrock</option>
      <option value="vertexai">Vertex AI</option>
    </NativeSelect>
  ),
};
