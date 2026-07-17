'use client';

/** @tier 3 — agents-scoped. The agent builder's modal chrome: header, progress, rail, footer. */

import { useEffect, useState } from 'react';
import Link from 'next/link';
import {
  Button,
  Card,
  CardContent,
  Heading,
  Icon,
  Modal,
  ModalAccessibleTitle,
  Progress,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import { fetchAgentType } from '../../_services/agents-api';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { BUILDER_STEPS } from '../../_hooks/use-agent-builder';
import { FRAMEWORK_LABELS } from '../motif/framework-cards';
import { StepRail } from './step-rail';
import { StepIdentity } from './step-identity';
import { StepBehaviour } from './step-behaviour';
import { StepTools } from './step-tools';
import { StepGuardrails } from './step-guardrails';
import { StepReview } from './step-review';

const SAVE_LABEL: Record<string, string> = {
  unsaved: 'Unsaved changes',
  saving: 'Saving…',
  saved: 'Draft saved',
  error: 'Save failed',
};

export function AgentBuilderModal({ onClose }: { onClose: () => void }) {
  const builder = useAgentBuilderContext();
  const { step, saveStatus, draftId, created, submitting, stepValid, createBlockers } = builder;
  const isLast = step === BUILDER_STEPS.length - 1;

  return (
    <Modal open onClose={onClose} size="builder" dismissOnOutsideClick={false}>
      <ModalAccessibleTitle>New agent</ModalAccessibleTitle>

      {/* ── Header ─────────────────────────────────────────────────────────── */}
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        height="header"
        paddingInline="comfortable"
        shrink={false}
      >
        <Icon name="zap" size="sm" container containerVariant="accent" label="" />
        <Stack direction="horizontal" align="baseline" gap="snug">
          <Text as="span" size="supporting" weight="heavy">
            New agent
          </Text>
          <Text as="span" size="micro" mono color="subtle">
            {created ? created.slug : draftId ? `draft · ${draftId}` : 'draft'}
          </Text>
        </Stack>
        <Stack grow />
        {/* Draft chrome is meaningless once the agent exists. A pristine
            builder (no draft, nothing typed) shows no save state either. */}
        {!created && (
          <>
            {(draftId || saveStatus !== 'unsaved') && (
              <Stack direction="horizontal" align="center" gap="snug">
                <StatusDot
                  size="sm"
                  status={
                    saveStatus === 'error' ? 'error' : saveStatus === 'saved' ? 'healthy' : 'idle'
                  }
                />
                <Text as="span" size="caption" color="subtle">
                  {SAVE_LABEL[saveStatus]}
                </Text>
              </Stack>
            )}
            <Button variant="secondary" size="sm" onClick={builder.saveNow}>
              Save draft
            </Button>
          </>
        )}
        <Button variant="ghost" size="icon-sm" aria-label="Close builder" onClick={onClose}>
          <Icon name="close" />
        </Button>
      </Stack>

      {/* ── Step progress ──────────────────────────────────────────────────── */}
      <Progress
        size="hairline"
        label="Builder progress"
        value={created ? 100 : ((step + 1) / BUILDER_STEPS.length) * 100}
      />

      {created ? (
        <SuccessPanel onClose={onClose} />
      ) : (
        <Stack direction="horizontal" grow overflow="hidden">
          <StepRail />
          <Separator orientation="vertical" span="full" />

          {/* ── Content column ─────────────────────────────────────────────── */}
          <Stack grow overflow="hidden">
            <Stack grow overflowY="auto" align="center">
              <Stack minWidth="document" maxWidth="document" padding="loose" gap="relaxed">
                {step === 0 && <StepIdentity />}
                {step === 1 && <StepBehaviour />}
                {step === 2 && <StepTools />}
                {step === 3 && <StepGuardrails />}
                {step === 4 && <StepReview />}
              </Stack>
            </Stack>

            {/* ── Footer ─────────────────────────────────────────────────── */}
            <Separator span="full" />
            <Stack
              direction="horizontal"
              align="center"
              gap="comfortable"
              padding="comfortable"
              paddingInline="relaxed"
              shrink={false}
            >
              {step > 0 && (
                <Button variant="secondary" onClick={builder.back}>
                  <Icon name="chevron-left" />
                  Back
                </Button>
              )}
              <Stack grow />
              <Text as="span" size="caption" color="subtle">
                Step {step + 1} of {BUILDER_STEPS.length}
              </Text>
              {isLast ? (
                <Button
                  onClick={() => void builder.submit()}
                  disabled={submitting || createBlockers.length > 0}
                >
                  {submitting ? 'Creating…' : 'Create agent'}
                  <Icon name="rocket" />
                </Button>
              ) : (
                <Button onClick={builder.next} disabled={!stepValid}>
                  Continue
                  <Icon name="arrow-right" />
                </Button>
              )}
            </Stack>
          </Stack>
        </Stack>
      )}
    </Modal>
  );
}

const PROVISION_POLL_MS = 2000;
const PROVISION_POLL_LIMIT = 45; // ~90s, then stop polling and say so

/** Shown in place of the builder body once the agent exists. */
function SuccessPanel({ onClose }: { onClose: () => void }) {
  const { created, fields, catalogues } = useAgentBuilderContext();
  const [provisionState, setProvisionState] = useState<
    'provisioning' | 'active' | 'deleting' | 'unknown'
  >('provisioning');

  // Poll the agent type until its workload is ready. State comes from the
  // control plane (provisioning -> active), so this reflects the real
  // workload coming up, not a timer.
  useEffect(() => {
    if (!created) return;
    let polls = 0;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      polls += 1;
      const { agentType } = await fetchAgentType(created.id);
      if (cancelled) return;
      if (agentType?.state === 'active') {
        setProvisionState('active');
        return;
      }
      if (agentType?.state === 'deleting') {
        setProvisionState('deleting');
        return;
      }
      if (polls >= PROVISION_POLL_LIMIT) {
        setProvisionState('unknown');
        return;
      }
      timer = setTimeout(() => void tick(), PROVISION_POLL_MS);
    };
    timer = setTimeout(() => void tick(), PROVISION_POLL_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [created]);

  if (!created) return null;

  const model = catalogues.models.find((m) => m.id === fields.modelId);
  const frameworkName = FRAMEWORK_LABELS[fields.framework];
  const summary: { icon: IconName; label: string; value: string }[] = [
    { icon: 'cpu', label: 'Model', value: model ? model.display_name || model.id : fields.modelId },
    { icon: 'square-terminal', label: 'Framework', value: frameworkName },
    {
      icon: 'wrench',
      label: 'Access',
      value: `${fields.mcpTools.length} MCP tools · ${fields.toolScopes.length} platform · ${fields.connectors.length} connections`,
    },
    {
      icon: 'dollar-sign',
      label: 'Budget',
      value: fields.agentBudgetOn
        ? `$${fields.agentBudgetUsd} / ${fields.agentBudgetCadence}${fields.runBudgetOn ? ` · $${fields.runBudgetUsd} per run` : ''}`
        : fields.runBudgetOn
          ? `$${fields.runBudgetUsd} per run`
          : 'No spend ceiling',
    },
  ];

  const status: { dot: React.ComponentProps<typeof StatusDot>['status']; text: string } =
    provisionState === 'active'
      ? { dot: 'healthy', text: 'Active and ready to run' }
      : provisionState === 'deleting'
        ? { dot: 'error', text: 'Deleting' }
        : provisionState === 'unknown'
          ? { dot: 'idle', text: 'Still provisioning. Check the agent page.' }
          : { dot: 'pending', text: 'Provisioning workload…' };

  return (
    <Stack grow align="center" justify="center" gap="comfortable" padding="loose">
      <Stack align="center" gap="snug">
        <Icon
          name="check-circle"
          size="xl"
          color="accent"
          container
          containerVariant="accent"
          circle
          label=""
        />
        <Heading variant="subsection">Agent created</Heading>
        <Text size="supporting" color="muted" align="center">
          <Text as="span" size="supporting" weight="heavy">
            {fields.name || 'Untitled agent'}
          </Text>{' '}
          has a verified identity. Access is checked against its grants before every tool call.
        </Text>
      </Stack>

      <Stack minWidth="prose" maxWidth="prose">
        <Card subtle>
          {/* Identity + live provisioning state, then the shape of what was granted. */}
          <CardContent flush>
            <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
              <Icon
                name="zap"
                size="sm"
                color="accent"
                container
                containerVariant="accent"
                label=""
              />
              <Stack grow gap="tight">
                <Text as="span" size="supporting" weight="heavy">
                  {fields.name || 'Untitled agent'}
                </Text>
                <Text as="span" size="micro" mono color="subtle">
                  {created.slug}
                </Text>
              </Stack>
              <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
                <StatusDot
                  size="md"
                  status={status.dot}
                  glow={provisionState === 'provisioning'}
                  pulse={provisionState === 'provisioning'}
                />
                <Text
                  as="span"
                  size="caption"
                  color={provisionState === 'active' ? 'foreground' : 'subtle'}
                >
                  {status.text}
                </Text>
              </Stack>
            </Stack>
            {summary.map((row) => (
              <Stack key={row.label}>
                <Separator span="full" />
                <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                  <Icon name={row.icon} size="xs" color="subtle" label="" />
                  <Text as="span" size="caption" color="subtle">
                    {row.label}
                  </Text>
                  <Stack grow />
                  <Text as="span" size="caption" align="end">
                    {row.value}
                  </Text>
                </Stack>
              </Stack>
            ))}
          </CardContent>
        </Card>
      </Stack>

      <Stack direction="horizontal" gap="cozy">
        <Button variant="secondary" onClick={onClose}>
          Close
        </Button>
        <Button asChild>
          <Link href={APP_ROUTES.agents.detail(created.slug)} onClick={onClose}>
            View agent
            <Icon name="arrow-right" />
          </Link>
        </Button>
      </Stack>
    </Stack>
  );
}
