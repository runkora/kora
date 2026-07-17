'use client';

/** @tier 3 — agents-scoped. Builder step 1: name, slug, description. */

import { FormField, Grid, Input, Stack, Textarea } from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { StepHeader } from './step-header';

export function StepIdentity() {
  const { fields, setName, setSlug, setField } = useAgentBuilderContext();

  return (
    <Stack gap="relaxed">
      <StepHeader
        icon="user"
        title="Identity"
        description="Give the agent a verified identity. This is who it is on the control plane, bound to your grants from the first call."
      />

      <Grid cols={2} gap="comfortable">
        <FormField label="Display name" required>
          <Input
            value={fields.name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Deployment Assistant"
            maxLength={128}
          />
        </FormField>
        <FormField label="Slug" required>
          <Input
            mono
            value={fields.slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder="deployment-assistant"
          />
        </FormField>
      </Grid>

      <FormField
        label="Description"
        hint="Optional. What this agent is responsible for, shown in the catalogue and on its identity card."
      >
        <Textarea
          value={fields.description}
          onChange={(e) => setField('description', e.target.value)}
          rows={2}
          placeholder="What this agent is responsible for."
        />
      </FormField>
    </Stack>
  );
}
