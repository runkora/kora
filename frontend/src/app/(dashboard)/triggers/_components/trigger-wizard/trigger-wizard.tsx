'use client';

/** @tier 3 — triggers-scoped. The New Trigger flow: kind picker, then the
 * chosen kind's steps, then the success screen, all in one builder modal.
 * Everything kind-specific comes from the registry in ./kinds. */

import { useCallback, useMemo, useState } from 'react';
import { Card, CardContent, Heading, Icon, Stack, Text, Wizard } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import { KindPicker } from './kind-picker';
import { SuccessPanel } from './success-panel';
import { SHARED_STEPS } from './shared-steps';
import { TRIGGER_KINDS } from './kinds';
import { initialState, stepsFor, toDraft } from './wizard-state';
import {
  brandedCopy,
  type TriggerKind,
  type TriggerWizardProps,
  type WizardContext,
  type WizardState,
} from './types';

interface Created {
  triggerId: string;
  endpoint?: string;
  secret?: string;
}

export function TriggerWizard({
  initialKind,
  agents,
  agentsError = null,
  sources,
  ingestHost,
  onCreate,
  onClose,
}: TriggerWizardProps) {
  const { productName } = useBrand();
  const [kind, setKind] = useState<TriggerKind | null>(initialKind ?? null);
  const [state, setState] = useState<WizardState | null>(() =>
    initialKind ? initialState(TRIGGER_KINDS[initialKind], sources) : null,
  );
  const [step, setStep] = useState(0);
  const [maxVisited, setMaxVisited] = useState(0);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<Created | null>(null);

  const context = useMemo<WizardContext>(
    () => ({ agents, agentsError, sources, ingestHost }),
    [agents, agentsError, sources, ingestHost],
  );

  const definition = kind ? TRIGGER_KINDS[kind] : null;
  const steps = useMemo(() => (definition ? stepsFor(definition, SHARED_STEPS) : []), [definition]);

  const wizardSteps = useMemo(
    () => steps.map((s) => ({ id: s.id, title: s.railTitle, sub: s.railSub })),
    [steps],
  );

  const patch = useCallback(
    (next: Partial<WizardState>) => setState((prev) => (prev ? { ...prev, ...next } : prev)),
    [],
  );

  const chooseKind = useCallback(
    (next: TriggerKind) => {
      setKind(next);
      setState(initialState(TRIGGER_KINDS[next], sources));
      setStep(0);
      setMaxVisited(0);
      setError(null);
      setCreated(null);
    },
    [sources],
  );

  const backToPicker = useCallback(() => {
    setKind(null);
    setState(null);
    setStep(0);
    setMaxVisited(0);
    setError(null);
    setCreated(null);
  }, []);

  const handleBack = useCallback(() => {
    if (step === 0) {
      backToPicker();
      return;
    }
    setStep((s) => s - 1);
  }, [step, backToPicker]);

  const currentStep = steps[step];
  const validation =
    definition && state && currentStep ? currentStep.validate(state, context) : { valid: false };
  const isLast = steps.length > 0 && step === steps.length - 1;

  const handleNext = useCallback(async () => {
    if (!definition || !state || !validation.valid || pending) return;
    if (!isLast) {
      const next = step + 1;
      setStep(next);
      setMaxVisited((m) => Math.max(m, next));
      return;
    }
    setPending(true);
    setError(null);
    const result = await onCreate(toDraft(definition, state));
    setPending(false);
    if (!result.ok) {
      setError(result.error);
      return;
    }
    setCreated({
      triggerId: result.triggerId,
      endpoint: result.endpoint,
      secret: result.secret,
    });
  }, [definition, state, validation.valid, pending, isLast, step, onCreate]);

  const StepBody = currentStep?.Component;
  const hasKind = definition !== null && state !== null;

  return (
    <Wizard
      open
      onClose={onClose}
      icon={definition?.icon ?? 'zap'}
      title="New trigger"
      subtitle={definition ? definition.id : undefined}
      hint={validation.hint}
      hintVariant={validation.valid ? 'warning' : 'error'}
      rail={
        hasKind && !created ? (
          <Wizard.Rail
            label="New trigger"
            steps={wizardSteps}
            current={step}
            maxVisited={maxVisited}
            onGoTo={(i) => {
              setStep(i);
              setError(null);
            }}
            footer={<PolicyCheckedInfo />}
          />
        ) : undefined
      }
      footer={
        hasKind && !created ? (
          <Wizard.Footer
            step={step}
            totalSteps={steps.length}
            onBack={handleBack}
            onNext={() => void handleNext()}
            nextDisabled={!validation.valid}
            onSubmit={isLast ? () => void handleNext() : undefined}
            submitLabel="Create trigger"
            pending={pending}
            pendingLabel="Creating…"
          />
        ) : undefined
      }
    >
      {created && definition && state ? (
        <Wizard.Step fullPage>
          <SuccessPanel
            definition={definition}
            state={state}
            context={context}
            triggerId={created.triggerId}
            endpoint={created.endpoint}
            secret={created.secret}
            onClose={onClose}
            onCreateAnother={backToPicker}
          />
        </Wizard.Step>
      ) : !hasKind ? (
        /* ── Phase one: choose what starts the run ─────────────────────────── */
        <Wizard.Step fullPage>
          <Stack grow overflowY="auto" align="center">
            <Stack
              minWidth={{ lg: 'document' }}
              maxWidth={{ lg: 'document' }}
              padding="loose"
              gap="relaxed"
            >
              <Stack gap="snug">
                <Heading variant="subsection">New trigger</Heading>
                <Text size="supporting" color="muted">
                  Choose what starts it. Every trigger names one target and is policy-checked before
                  it fires.
                </Text>
              </Stack>
              <KindPicker onSelect={chooseKind} />
            </Stack>
          </Stack>
        </Wizard.Step>
      ) : (
        <Wizard.Step>
          <Stack gap="snug">
            <Heading variant="subsection">{currentStep?.railTitle}</Heading>
            <Text size="supporting" color="muted">
              {currentStep && brandedCopy(currentStep.description, productName)}
            </Text>
          </Stack>
          {StepBody && (
            <StepBody
              state={state}
              patch={patch}
              context={context}
              definition={definition}
              error={error}
              onGoTo={(stepId) => {
                const target = steps.findIndex((s) => s.id === stepId);
                if (target >= 0 && target <= maxVisited) setStep(target);
              }}
            />
          )}
        </Wizard.Step>
      )}
    </Wizard>
  );
}

function PolicyCheckedInfo() {
  return (
    <Card subtle>
      <CardContent padded compact>
        <Stack gap="tight">
          <Stack direction="horizontal" align="center" gap="snug">
            <Icon name="info" size="xs" color="accent" label="" />
            <Text as="span" size="caption" weight="heavy" color="muted">
              Policy-checked
            </Text>
          </Stack>
          <Text size="micro" color="subtle">
            A trigger starts work as the agent it targets. Every run it starts is checked against
            that agent&apos;s grants before anything executes.
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}
