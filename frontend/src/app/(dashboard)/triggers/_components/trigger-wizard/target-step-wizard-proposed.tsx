'use client';

/** @tier 3 — triggers-scoped. The New Trigger modal held on its Target step,
 * rendering the proposed routing step. Storybook only: the real wizard keeps
 * its own Target step until the design is accepted. */

import { useEffect, useMemo, useState } from 'react';
import { Heading, Stack, Text, Wizard } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { ModelObject } from '@runkora/shared/api';
import { TRIGGER_KINDS } from './kinds';
import { SHARED_STEPS } from './shared-steps';
import { initialState, stepsFor, validateTarget } from './wizard-state';
import {
  DEFAULT_ROUTING,
  StepTargetProposed,
  decisionModelsOf,
  validateRouting,
} from './step-target-proposed';
import type { DecisionCatalog, RoutingAgent, RoutingDraft } from './step-target-proposed';
import type { TriggerKind, TriggerWizardSource, WizardContext, WizardState } from './types';

export interface TargetStepWizardProposedProps {
  kind: TriggerKind;
  agents: RoutingAgent[];
  sources: TriggerWizardSource[];
  /** Starting state for the step, so each story opens on the case it shows. */
  routing?: Partial<RoutingDraft>;
  agentTypeId?: string;
}

/** Reads the tenant catalog the way the agent builder does, through the BFF. */
function useDecisionCatalog(): DecisionCatalog {
  const [catalog, setCatalog] = useState<DecisionCatalog>({ status: 'loading' });
  useEffect(() => {
    let live = true;
    void fetch(ApiRoute.models.GET(), { cache: 'no-store' })
      .then(async (res) => {
        if (!res.ok) throw new Error(`Could not load the model catalog (${res.status}).`);
        const body = (await res.json()) as { data?: ModelObject[] };
        if (live) setCatalog({ status: 'ready', models: decisionModelsOf(body.data ?? []) });
      })
      .catch((err: unknown) => {
        if (live)
          setCatalog({
            status: 'error',
            message: err instanceof Error ? err.message : 'Could not load the model catalog.',
          });
      });
    return () => {
      live = false;
    };
  }, []);
  return catalog;
}

export function TargetStepWizardProposed({
  kind,
  agents,
  sources,
  routing: initialRouting,
  agentTypeId = '',
}: TargetStepWizardProposedProps) {
  const { productName } = useBrand();
  const definition = TRIGGER_KINDS[kind];
  const steps = useMemo(() => stepsFor(definition, SHARED_STEPS), [definition]);
  const targetIndex = steps.findIndex((s) => s.id === 'target');
  const [state, setState] = useState<WizardState>(() => ({
    ...initialState(definition, sources),
    name: 'route-github-work',
    promptTemplate: 'Handle this {{event.type}} on {{repository.full_name}}.',
    agentTypeId,
  }));
  const [routing, setRouting] = useState<RoutingDraft>({ ...DEFAULT_ROUTING, ...initialRouting });
  const catalog = useDecisionCatalog();
  const context = useMemo<WizardContext>(
    () => ({ agents, sources, ingestHost: 'api.kora.example' }),
    [agents, sources],
  );

  const validation =
    routing.mode === 'one' ? validateTarget(state) : validateRouting(routing, catalog);

  return (
    <Wizard
      open
      onClose={() => undefined}
      icon={definition.icon}
      title="New trigger"
      subtitle={definition.id}
      hint={validation.hint}
      hintVariant={validation.valid ? 'warning' : 'error'}
      rail={
        <Wizard.Rail
          label="New trigger"
          steps={steps.map((s) => ({ id: s.id, title: s.railTitle, sub: s.railSub }))}
          current={targetIndex}
          maxVisited={targetIndex}
          onGoTo={() => undefined}
        />
      }
      footer={
        <Wizard.Footer
          step={targetIndex}
          totalSteps={steps.length}
          onBack={() => undefined}
          onNext={() => undefined}
          nextDisabled={!validation.valid}
        />
      }
    >
      <Wizard.Step>
        <Stack gap="snug">
          <Heading variant="subsection">Target</Heading>
          <Text size="supporting" color="muted">
            The agent this trigger runs, or the agents {productName} chooses between.
          </Text>
        </Stack>
        <StepTargetProposed
          state={state}
          patch={(next) => setState((prev) => ({ ...prev, ...next }))}
          context={context}
          definition={definition}
          error={null}
          routing={routing}
          onRouting={(next) => setRouting((prev) => ({ ...prev, ...next }))}
          agents={agents}
          catalog={catalog}
        />
      </Wizard.Step>
    </Wizard>
  );
}
