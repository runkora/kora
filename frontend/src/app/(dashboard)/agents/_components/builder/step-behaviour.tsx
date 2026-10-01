'use client';

/** @tier 3 — agents-scoped. Builder step 2: system instructions, framework, model. */

import { Alert, FormField, SectionHeading, Separator, Stack, Text, Textarea } from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { FrameworkCards } from '../motif/framework-cards';
import { ModelPicker } from '@orbit/components';

export function StepBehaviour() {
  const { fields, setField, catalogues } = useAgentBuilderContext();

  return (
    <Stack gap="relaxed">
      <SectionHeading
        icon="bot"
        title="Behaviour"
        description="Describe how the agent should behave, then choose the model and framework it runs on."
      >
        <FormField label="System instructions" required>
          <Textarea
            value={fields.systemPrompt}
            onChange={(e) => setField('systemPrompt', e.target.value)}
            rows={5}
            resize="vertical"
            placeholder="You are a deployment assistant for the platform team. You help engineers roll out services safely. You always run policy checks before applying changes and summarise the blast radius first."
          />
        </FormField>

        <Separator />

        {/* ── Framework ──────────────────────────────────────────────────────── */}
        <Stack gap="cozy">
          <Text as="span" size="caption" weight="heavy" color="muted">
            Framework
          </Text>
          <FrameworkCards value={fields.framework} onChange={(v) => setField('framework', v)} />
        </Stack>

        {/* ── Model ──────────────────────────────────────────────────────────── */}
        <Stack gap="cozy">
          <Stack direction="horizontal" align="center">
            <Text as="span" size="caption" weight="heavy" color="muted">
              Model
            </Text>
            <Stack grow />
            <Text as="span" size="micro" color="subtle">
              {catalogues.models.length} enabled by your platform
            </Text>
          </Stack>

          {catalogues.status.models === 'error' && (
            <Alert variant="error">Models failed to load: {catalogues.errors.models}</Alert>
          )}

          <ModelPicker
            models={catalogues.models}
            value={fields.modelId}
            onChange={(id) => setField('modelId', id)}
          />
        </Stack>
      </SectionHeading>
    </Stack>
  );
}
