import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Input } from '../input/input';
import { NativeSelect } from '../native-select/native-select';
import { Textarea } from '../textarea/textarea';
import { FormField } from './form-field';

/**
 * FormField
 *
 * Puts a label, a control and optional help or error text together as one field. Use it for every input in a form.
 */
const meta: Meta<typeof FormField> = {
  title: 'Hades/Molecules/FormField',
  component: FormField,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof FormField>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="A form built from fields">
        <div className="flex flex-col gap-4">
          <FormField label="Agent name" id="overview-name" required>
            <Input id="overview-name" placeholder="policy-auditor" />
          </FormField>
          <FormField
            label="Inference endpoint"
            id="overview-endpoint"
            hint="Use an HTTPS address the platform can reach."
          >
            <Input id="overview-endpoint" placeholder="https://api.example.com/v1" />
          </FormField>
          <FormField label="Provider" id="overview-provider">
            <NativeSelect id="overview-provider" defaultValue="bedrock">
              <option value="bedrock">AWS Bedrock</option>
              <option value="anthropic">Anthropic</option>
            </NativeSelect>
          </FormField>
          <FormField label="Description" id="overview-description">
            <Textarea id="overview-description" rows={3} placeholder="What does this agent do?" />
          </FormField>
        </div>
      </Example>
      <Example title="A field with an error">
        <FormField label="Token name" id="overview-token" error="Token name is required." required>
          <Input id="overview-token" hasError />
        </FormField>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="A label above a control.">
          <FormField label="Agent name" id="opt-default">
            <Input id="opt-default" placeholder="policy-auditor" />
          </FormField>
        </Option>
        <Option
          name="hint"
          use="Help text under the control. It is replaced when there is an error."
        >
          <FormField label="Scopes" id="opt-hint" hint="Separate scopes with a space.">
            <Input id="opt-hint" placeholder="agents:read agents:write" />
          </FormField>
        </Option>
        <Option
          name="error"
          use="Shows an error message in place of the hint. Also set hasError on the control."
        >
          <FormField
            label="Model ID"
            id="opt-error"
            hint="The full model name."
            error="Model ID is required."
          >
            <Input id="opt-error" hasError />
          </FormField>
        </Option>
        <Option name="required" use="Adds a star to the label and marks the control as required.">
          <FormField label="Principal" id="opt-required" required>
            <Input id="opt-required" placeholder="agent:policy-auditor" />
          </FormField>
        </Option>
        <Option name='labelSize="xs"' use="A smaller label for dense cards and side panels.">
          <FormField label="Agent name" id="opt-xs" labelSize="xs">
            <Input id="opt-xs" compact placeholder="policy-auditor" />
          </FormField>
        </Option>
        <Option name="any control" use="Works with Input, Textarea and NativeSelect.">
          <FormField label="Policy description" id="opt-textarea">
            <Textarea id="opt-textarea" rows={3} placeholder="What this policy allows" />
          </FormField>
        </Option>
      </OptionTable>
    </div>
  ),
};
