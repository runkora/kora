'use client';

/** @tier 3 — agents-scoped. Builder step 5: the complete configuration, grouped by owning step. */

import { useMemo } from 'react';
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  SectionHeading,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { FRAMEWORK_LABELS } from '../motif/framework-cards';

interface ReviewRow {
  k: string;
  v: string;
  mono?: boolean;
}

function ReviewGroup({
  icon,
  title,
  rows,
  onEdit,
}: {
  icon: IconName;
  title: string;
  rows: ReviewRow[];
  onEdit: () => void;
}) {
  return (
    <Card>
      <CardHeader
        compact
        divider
        leadingIcon={icon}
        title={title}
        action={
          <Button variant="ghost" size="xs" onClick={onEdit}>
            Edit
          </Button>
        }
      />
      <CardContent padded compact>
        <Stack gap="none">
          {rows.map((row, i) => (
            <Stack key={row.k}>
              {i > 0 && <Separator span="full" />}
              <Stack direction="horizontal" align="center" gap="comfortable" paddingBlock="snug">
                <Text as="span" size="caption" color="subtle">
                  {row.k}
                </Text>
                <Stack grow />
                <Text as="span" size="caption" mono={row.mono ?? false} align="end">
                  {row.v}
                </Text>
              </Stack>
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}

export function StepReview() {
  const { fields, catalogues, goTo, createBlockers, submitError } = useAgentBuilderContext();

  const model = catalogues.models.find((m) => m.id === fields.modelId);
  const environment = catalogues.environments.find((e) => e.id === fields.environmentId);
  const grantedServers = useMemo(() => {
    const prefixes = new Set(fields.mcpTools.map((s) => s.split(':')[1]));
    return prefixes.size;
  }, [fields.mcpTools]);

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

      <Stack gap="cozy">
        <ReviewGroup
          icon="user"
          title="Identity"
          onEdit={() => goTo(0)}
          rows={[
            { k: 'Name', v: fields.name || 'Not set' },
            { k: 'Slug', v: fields.slug || 'Not set', mono: fields.slug.length > 0 },
          ]}
        />
        <ReviewGroup
          icon="bot"
          title="Behaviour"
          onEdit={() => goTo(1)}
          rows={[
            {
              k: 'Model',
              v: model ? `${model.display_name || model.id} · ${model.owned_by}` : 'Not set',
            },
            { k: 'Framework', v: frameworkName },
            {
              k: 'Instructions',
              v: fields.systemPrompt
                ? `${fields.systemPrompt.slice(0, 56)}${fields.systemPrompt.length > 56 ? '…' : ''}`
                : 'Not set',
            },
          ]}
        />
        <ReviewGroup
          icon="wrench"
          title="Tools & access"
          onEdit={() => goTo(2)}
          rows={[
            { k: 'MCP servers', v: String(grantedServers) },
            { k: 'MCP tools granted', v: String(fields.mcpTools.length), mono: true },
            { k: 'Platform scopes', v: String(fields.toolScopes.length), mono: true },
            { k: 'Connections', v: String(fields.connectors.length) },
            { k: 'Network egress', v: fields.connectivityMode },
            { k: 'Environment', v: environment?.name ?? 'None' },
          ]}
        />
        <ReviewGroup
          icon="shield"
          title="Guardrails"
          onEdit={() => goTo(3)}
          rows={[
            {
              k: 'Agent budget',
              v: fields.agentBudgetOn
                ? `$${fields.agentBudgetUsd} / ${fields.agentBudgetCadence}`
                : 'Off',
              mono: fields.agentBudgetOn,
            },
            {
              k: 'Run budget',
              v: fields.runBudgetOn ? `$${fields.runBudgetUsd} / run` : 'Off',
              mono: fields.runBudgetOn,
            },
            {
              k: 'Capacity',
              v: fields.capacityOn ? `${fields.capacity} runs / pod` : 'Service default',
              mono: fields.capacityOn,
            },
          ]}
        />
      </Stack>
    </Stack>
  );
}
