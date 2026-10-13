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
  Separator,
  Stack,
  StatusDot,
  Text,
  Wizard,
} from '@clear-route/hades';
import type { IconName } from '@clear-route/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import { fetchAgentType } from '../../_services/agents-api';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { BUILDER_STEPS } from '../../_hooks/use-agent-builder';
import { FRAMEWORK_LABELS } from '../motif/framework-cards';
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

const WIZARD_STEPS = BUILDER_STEPS.map((s) => ({
  id: s.key,
  title: s.title,
  shortTitle: 'shortTitle' in s ? (s.shortTitle as string) : undefined,
  sub: s.sub,
}));

export function AgentBuilderModal({ onClose }: { onClose: () => void }) {
  const builder = useAgentBuilderContext();
  const {
    step,
    maxVisited,
    goTo,
    saveStatus,
    draftId,
    created,
    submitting,
    stepValid,
    stepHint,
    createBlockers,
  } = builder;
  const isLast = step === BUILDER_STEPS.length - 1;

  const saveIndicator =
    draftId || saveStatus !== 'unsaved' ? (
      <Stack direction="horizontal" align="center" gap="snug">
        <StatusDot
          size="sm"
          status={saveStatus === 'error' ? 'error' : saveStatus === 'saved' ? 'healthy' : 'idle'}
        />
        <Text as="span" size="caption" color="subtle">
          {SAVE_LABEL[saveStatus]}
        </Text>
      </Stack>
    ) : undefined;

  return (
    <Wizard
      open
      onClose={onClose}
      icon="zap"
      title="New agent"
      subtitle={created ? undefined : (draftId ?? undefined)}
      actions={created ? undefined : saveIndicator}
      hint={created ? undefined : stepHint}
      hintVariant={step === 0 ? 'error' : 'warning'}
      rail={
        created ? undefined : (
          <Wizard.Rail
            label="Create agent"
            steps={WIZARD_STEPS}
            current={step}
            maxVisited={maxVisited}
            onGoTo={goTo}
            footer={<IdentityFirstInfo />}
          />
        )
      }
      footer={
        created ? undefined : (
          <Wizard.Footer
            step={step}
            totalSteps={BUILDER_STEPS.length}
            onBack={step > 0 ? builder.back : undefined}
            onNext={builder.next}
            nextDisabled={!stepValid || (isLast && createBlockers.length > 0)}
            onSubmit={isLast ? () => void builder.submit() : undefined}
            submitLabel="Create agent"
            pending={submitting}
            pendingLabel="Creating…"
          />
        )
      }
    >
      {created ? (
        <Wizard.Step fullPage>
          <SuccessPanel onClose={onClose} />
        </Wizard.Step>
      ) : (
        <>
          {step === 0 && (
            <Wizard.Step>
              <StepIdentity />
            </Wizard.Step>
          )}
          {step === 1 && (
            <Wizard.Step>
              <StepBehaviour />
            </Wizard.Step>
          )}
          {step === 2 && (
            <Wizard.Step>
              <StepTools />
            </Wizard.Step>
          )}
          {step === 3 && (
            <Wizard.Step>
              <StepGuardrails />
            </Wizard.Step>
          )}
          {step === 4 && (
            <Wizard.Step>
              <StepReview />
            </Wizard.Step>
          )}
        </>
      )}
    </Wizard>
  );
}

function IdentityFirstInfo() {
  return (
    <Card subtle>
      <CardContent padded compact>
        <Stack gap="tight">
          <Stack direction="horizontal" align="center" gap="snug">
            <Icon name="info" size="xs" color="accent" label="" />
            <Text as="span" size="caption" weight="heavy" color="muted">
              Identity-first
            </Text>
          </Stack>
          <Text size="micro" color="subtle">
            Every agent gets a verified identity before it can touch a single tool. Access is
            checked before execution, never after.
          </Text>
        </Stack>
      </CardContent>
    </Card>
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
      value: `${fields.mcpTools.length} MCP tools · ${fields.connectors.length} connections`,
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
              <Stack key={row.label} gap="none">
                <Separator />
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
