'use client';

/** @tier 3 — triggers-scoped. Shown in place of the wizard body once the
 * trigger exists: the shared confirmation, then whatever the kind needs the
 * operator to take away before closing. */

import { Button, Heading, Icon, Stack, Text } from '@orbit/hades';
import type { DoneContentProps, TriggerKindDefinition, WizardContext, WizardState } from './types';

export interface SuccessPanelProps {
  definition: TriggerKindDefinition;
  state: WizardState;
  context: WizardContext;
  triggerId: string;
  endpoint?: string;
  secret?: string;
  onClose: () => void;
  onCreateAnother: () => void;
}

export function SuccessPanel({
  definition,
  state,
  context,
  triggerId,
  endpoint,
  secret,
  onClose,
  onCreateAnother,
}: SuccessPanelProps) {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  const agentLabel = agent?.name?.trim() || agent?.slug || 'the agent';
  const KindDone: React.ComponentType<DoneContentProps> = definition.doneContent;

  return (
    <Stack grow align="center" justify="center" gap="comfortable" padding="loose" overflowY="auto">
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
        <Heading variant="subsection">Trigger created</Heading>
        <Text size="supporting" color="muted" align="center">
          <Text as="span" size="supporting" mono weight="heavy">
            {state.name}
          </Text>{' '}
          is live and will run {agentLabel}.
        </Text>
      </Stack>

      <KindDone
        state={state}
        context={context}
        triggerId={triggerId}
        endpoint={endpoint}
        secret={secret}
      />

      <Stack direction="horizontal" gap="cozy">
        <Button variant="secondary" onClick={onClose}>
          Done
        </Button>
        <Button onClick={onCreateAnother}>
          Create another
          <Icon name="plus" />
        </Button>
      </Stack>
    </Stack>
  );
}
