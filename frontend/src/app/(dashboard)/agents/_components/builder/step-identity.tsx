'use client';

/** @tier 3 — agents-scoped. Builder step 1: name, slug, description, identity model. */

import {
  FormField,
  Grid,
  Input,
  SectionHeading,
  Separator,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { IdentityModeCards } from '../motif/identity-mode-cards';

export function StepIdentity() {
  const { fields, setName, setSlug, setField, identityModeAccess } = useAgentBuilderContext();

  // Mode-qualified create permissions (agents:types:{mode}:write, resolved
  // with prefix covers so agents:types:write enables both). Until the scope
  // set loads, nothing is disabled; the backend check is authoritative.
  const disabledModes = {
    ...(identityModeAccess.loaded && !identityModeAccess.autonomous
      ? { autonomous: 'You do not have permission to create autonomous agents.' }
      : null),
    ...(identityModeAccess.loaded && !identityModeAccess.delegated
      ? { delegated: 'You do not have permission to create delegated agents.' }
      : null),
  };

  return (
    <Stack gap="relaxed">
      <SectionHeading
        icon="user"
        title="Identity"
        description="Give the agent a verified identity. This is who it is on the control plane, bound to your grants from the first call."
      >
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

        <Separator span="full" />

        {/* ── Identity model ─────────────────────────────────────────────────── */}
        <Stack gap="cozy">
          <Stack direction="horizontal" align="center">
            <Text as="span" size="caption" weight="heavy" color="muted">
              Identity model
            </Text>
            <Stack grow />
            <Text as="span" size="micro" color="subtle">
              Permanent: cannot be changed after creation.
            </Text>
          </Stack>
          <IdentityModeCards
            value={fields.identityMode}
            onChange={(v) => setField('identityMode', v)}
            disabledModes={disabledModes}
          />
        </Stack>
      </SectionHeading>
    </Stack>
  );
}
