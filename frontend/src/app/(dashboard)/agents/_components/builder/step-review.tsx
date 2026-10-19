'use client';

/** @tier 3 — agents-scoped. Builder step 5: the complete configuration, grouped by owning step. */

import { useMemo } from 'react';
import { Alert, SectionHeading, Stack, Wizard } from '@runkora/hades';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { FRAMEWORK_LABELS } from '../motif/framework-cards';
import { IDENTITY_MODE_LABELS } from '../motif/identity-mode-cards';

export function StepReview() {
  const { fields, catalogues, goTo, createBlockers, submitError } = useAgentBuilderContext();

  const model = catalogues.models.find((m) => m.id === fields.modelId);
  const environmentsSummary = useMemo(() => {
    if (fields.environments.length === 0) return 'None. Runs on the kora base image.';
    if (catalogues.status.environments === 'loading') return 'Loading environments.';
    const name = (id: string) =>
      catalogues.environments.find((e) => e.id === id)?.name ?? 'Deleted environment';
    const def = fields.environments.find((e) => e.is_default);
    const names = fields.environments.map((e) => name(e.environment_id)).join(', ');
    return fields.environments.length > 1 && def
      ? `${names} (default: ${name(def.environment_id)})`
      : names;
  }, [fields.environments, catalogues.environments, catalogues.status.environments]);
  const grantedServers = useMemo(() => {
    const prefixes = new Set(fields.mcpTools.map((s) => s.split(':')[1]));
    return prefixes.size;
  }, [fields.mcpTools]);

  // Surface resource-scoped connectors so the review does not silently drop
  // the qualifier state (connection scope qualifiers ADR).
  const connectionsSummary = useMemo(() => {
    const scoped = fields.connectors.filter((c) => (c.resources?.length ?? 0) > 0).length;
    return scoped > 0
      ? `${fields.connectors.length} (${scoped} resource-scoped)`
      : String(fields.connectors.length);
  }, [fields.connectors]);

  const frameworkName = FRAMEWORK_LABELS[fields.framework];

  return (
    <Stack gap="relaxed">
      <SectionHeading
        icon="rocket"
        title="Review & create"
        description="Confirm the configuration and create the agent. Nothing exists until you do."
      />

      {createBlockers.length > 0 && <Alert variant="warning">{createBlockers.join(' ')}</Alert>}
      {submitError && <Alert variant="error">{submitError}</Alert>}

      <Wizard.ReviewStep>
        <Wizard.ReviewCard
          icon="user"
          title="Identity"
          onEdit={() => goTo(0)}
          rows={[
            { label: 'Name', value: fields.name || 'Not set' },
            { label: 'Slug', value: fields.slug || 'Not set', mono: fields.slug.length > 0 },
            { label: 'Identity model', value: IDENTITY_MODE_LABELS[fields.identityMode] },
          ]}
        />
        <Wizard.ReviewCard
          icon="bot"
          title="Behaviour"
          onEdit={() => goTo(1)}
          rows={[
            {
              label: 'Model',
              value: model ? `${model.display_name || model.id} · ${model.owned_by}` : 'Not set',
            },
            { label: 'Framework', value: frameworkName },
            {
              label: 'Instructions',
              value: fields.systemPrompt
                ? `${fields.systemPrompt.slice(0, 56)}${fields.systemPrompt.length > 56 ? '…' : ''}`
                : 'Not set',
            },
          ]}
        />
        <Wizard.ReviewCard
          icon="wrench"
          title="Tools & access"
          onEdit={() => goTo(2)}
          rows={[
            { label: 'MCP servers', value: String(grantedServers) },
            { label: 'MCP tools granted', value: String(fields.mcpTools.length), mono: true },
            { label: 'Connections', value: connectionsSummary },
            { label: 'Network egress', value: fields.connectivityMode },
            { label: 'Environments', value: environmentsSummary },
          ]}
        />
        <Wizard.ReviewCard
          icon="shield"
          title="Guardrails"
          onEdit={() => goTo(3)}
          rows={[
            {
              label: 'Agent budget',
              value: fields.agentBudgetOn
                ? `$${fields.agentBudgetUsd} / ${fields.agentBudgetCadence}`
                : 'Off',
              mono: fields.agentBudgetOn,
            },
            {
              label: 'Run budget',
              value: fields.runBudgetOn ? `$${fields.runBudgetUsd} / run` : 'Off',
              mono: fields.runBudgetOn,
            },
            {
              label: 'Capacity',
              value: fields.capacityOn ? `${fields.capacity} runs / pod` : 'Service default',
              mono: fields.capacityOn,
            },
            {
              label: 'Repeated calls',
              value: fields.repeatedCallOn
                ? `Warn after ${fields.repeatedCallWarn}, pause after ${fields.repeatedCallPause}`
                : 'Platform default',
              mono: fields.repeatedCallOn,
            },
            {
              label: 'Wait loops',
              value: fields.waitRearmOn
                ? `Warn after ${fields.waitRearmWarn}, pause after ${fields.waitRearmPause}`
                : 'Platform default',
              mono: fields.waitRearmOn,
            },
          ]}
        />
      </Wizard.ReviewStep>
    </Stack>
  );
}
