import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import type { ModelObject } from '@runkora/shared/api';
import { Stack } from '@runkora/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { ModelPicker } from './model-picker';

/**
 * ModelPicker
 *
 * Choose one model from the list an admin has approved. Models are grouped by family and show
 * their price, and a filter box appears when the list is long.
 */
const meta: Meta<typeof ModelPicker> = {
  title: 'App/Molecules/ModelPicker',
  component: ModelPicker,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ModelPicker>;

function model(partial: Partial<ModelObject> & Pick<ModelObject, 'id' | 'family'>): ModelObject {
  return {
    object: 'model',
    created: 0,
    owned_by: partial.family,
    display_name: partial.id.split('/')[1] ?? partial.id,
    status: 'available',
    aliases: [],
    input_modalities: ['text'],
    output_modalities: ['text'],
    dialects: ['openai.chat'],
    capabilities: ['chat'],
    ...partial,
  } as ModelObject;
}

const MODELS: ModelObject[] = [
  model({
    id: 'anthropic/claude-haiku-4-5',
    family: 'anthropic',
    display_name: 'Claude Haiku 4.5',
    pricing: { effective_input_cost_per_1m: 1.1, effective_output_cost_per_1m: 5.5 },
  }),
  model({
    id: 'anthropic/claude-sonnet-4-6',
    family: 'anthropic',
    display_name: 'Claude Sonnet 4.6',
    pricing: { effective_input_cost_per_1m: 3, effective_output_cost_per_1m: 15 },
  }),
  model({
    id: 'amazon/nova-lite',
    family: 'amazon',
    display_name: 'Amazon Nova Lite',
    pricing: { effective_input_cost_per_1m: 0.06, effective_output_cost_per_1m: 0.24 },
  }),
];

/** Twelve models across three providers, enough to pass the threshold that shows the filter. */
const MANY_MODELS: ModelObject[] = Array.from({ length: 12 }, (_, i) =>
  model({
    id: `provider-${i % 3}/model-${i}`,
    family: `provider-${i % 3}`,
    display_name: `Model ${i}`,
  }),
);

function Demo({ models }: { models: ModelObject[] }) {
  const [value, setValue] = useState(models[0]?.id ?? '');
  return (
    <Stack maxWidth="card">
      <ModelPicker models={models} value={value} onChange={setValue} />
    </Stack>
  );
}

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Pick a model for an agent">
        <Demo models={MODELS} />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="models">
        <Option
          name="few models"
          use="Grouped by family with the price per million tokens. No filter box."
        >
          <Demo models={MODELS} />
        </Option>
        <Option
          name="many models"
          use="Past eight models a filter box appears. It matches name, id and provider."
        >
          <Demo models={MANY_MODELS} />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
