import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { NativeSelect } from './native-select';
import { Label } from '../label/label';

/**
 * NativeSelect
 *
 * A dropdown that uses the browser's own menu. Use it for short lists, or when
 * you need groups.
 */
const meta: Meta<typeof NativeSelect> = {
  title: 'Hades/Atoms/NativeSelect',
  component: NativeSelect,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof NativeSelect>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="With a label">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-provider">Provider</Label>
          <NativeSelect id="overview-provider" defaultValue="bedrock">
            <option value="bedrock">AWS Bedrock</option>
            <option value="vertexai">Vertex AI</option>
            <option value="anthropic">Anthropic</option>
          </NativeSelect>
        </div>
      </Example>
      <Example title="With groups">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-model">Model</Label>
          <NativeSelect id="overview-model" defaultValue="sonnet">
            <optgroup label="Claude">
              <option value="sonnet">Sonnet</option>
              <option value="haiku">Haiku</option>
            </optgroup>
            <optgroup label="Llama">
              <option value="llama-70b">Llama 70B</option>
            </optgroup>
          </NativeSelect>
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="A closed dropdown showing the chosen option.">
          <NativeSelect aria-label="Provider" defaultValue="bedrock">
            <option value="bedrock">AWS Bedrock</option>
            <option value="vertexai">Vertex AI</option>
          </NativeSelect>
        </Option>
        <Option name="disabled" use="The choice cannot be changed.">
          <NativeSelect aria-label="Provider, disabled" disabled defaultValue="bedrock">
            <option value="bedrock">AWS Bedrock</option>
          </NativeSelect>
        </Option>
        <Option name="hasError" use="Marks the choice as invalid. Show a message next to it.">
          <NativeSelect aria-label="Provider, invalid" hasError defaultValue="">
            <option value="" disabled>
              Choose a provider
            </option>
            <option value="bedrock">AWS Bedrock</option>
          </NativeSelect>
        </Option>
        <Option name="mono" use="Fixed-width text for technical values such as model IDs.">
          <NativeSelect aria-label="Model ID" mono defaultValue="claude-sonnet-4-5">
            <option value="claude-sonnet-4-5">claude-sonnet-4-5</option>
            <option value="claude-haiku-4-5">claude-haiku-4-5</option>
          </NativeSelect>
        </Option>
      </OptionTable>
    </div>
  ),
};
