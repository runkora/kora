import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import type { ModelObject } from '@orbit/shared/api';
import { ModelPicker } from './model-picker';

const meta: Meta<typeof ModelPicker> = {
  title: 'App/Shared/ModelPicker',
  component: ModelPicker,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
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

function Demo({ models }: { models: ModelObject[] }) {
  const [value, setValue] = useState(models[0]?.id ?? '');
  return <ModelPicker models={models} value={value} onChange={setValue} />;
}

export const FamilyGroups: Story = {
  render: () => <Demo models={MODELS} />,
};

export const WithSearch: Story = {
  name: 'With search (above threshold)',
  render: () => (
    <Demo
      models={Array.from({ length: 12 }, (_, i) =>
        model({
          id: `provider-${i % 3}/model-${i}`,
          family: `provider-${i % 3}`,
          display_name: `Model ${i}`,
        }),
      )}
    />
  ),
};
