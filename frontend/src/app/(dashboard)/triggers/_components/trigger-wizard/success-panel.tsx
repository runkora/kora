'use client';

/** @tier 3 — triggers-scoped. Shown in the content column once the trigger
 * exists: the shared confirmation, then whatever the kind needs the operator to
 * take away before closing. Its actions live in the persistent footer. */

import { Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DoneContentProps, TriggerKindDefinition, WizardContext, WizardState } from './types';

export interface SuccessPanelProps {
  definition: TriggerKindDefinition;
  state: WizardState;
  context: WizardContext;
  triggerId: string;
  endpoint?: string;
  secret?: string;
}

export function SuccessPanel({
  definition,
  state,
  context,
  triggerId,
  endpoint,
  secret,
}: SuccessPanelProps) {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  const agentLabel = agent?.name?.trim() || agent?.slug || 'the agent';
  const KindDone: React.ComponentType<DoneContentProps> = definition.doneContent;

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="center" gap="cozy">
        <Icon
          name="check-circle"
          size="md"
          color="accent"
          container
          containerVariant="accent"
          circle
          label=""
        />
        <Stack gap="tight">
          <Heading variant="subsection">Trigger created</Heading>
          <Text size="supporting" color="muted">
            <Text as="span" size="supporting" mono weight="heavy">
              {state.name}
            </Text>{' '}
            is live and will run {agentLabel}.
          </Text>
        </Stack>
      </Stack>

      <KindDone
        state={state}
        context={context}
        triggerId={triggerId}
        endpoint={endpoint}
        secret={secret}
      />
    </Stack>
  );
}
