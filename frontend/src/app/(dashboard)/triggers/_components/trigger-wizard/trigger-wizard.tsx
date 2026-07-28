'use client';

/** @tier 3 — triggers-scoped. The New Trigger flow: kind picker, then the
 * chosen kind's steps, then the success screen, all in one builder modal.
 * Everything kind-specific comes from the registry in ./kinds. */

import { useCallback, useMemo, useState } from 'react';
import {
  Button,
  Heading,
  Icon,
  Modal,
  ModalAccessibleTitle,
  Progress,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import { KindPicker } from './kind-picker';
import { WizardRail } from './wizard-rail';
import { SuccessPanel } from './success-panel';
import { SHARED_STEPS } from './shared-steps';
import { TRIGGER_KINDS } from './kinds';
import { initialState, stepsFor, toDraft } from './wizard-state';
import type { TriggerKind, TriggerWizardProps, WizardContext, WizardState } from './types';

interface Created {
  triggerId: string;
  endpoint?: string;
  secret?: string;
}

export function TriggerWizard({
  initialKind,
  agents,
  sources,
  ingestHost,
  onCreate,
  onClose,
}: TriggerWizardProps) {
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
    () => ({ agents, sources, ingestHost }),
    [agents, sources, ingestHost],
  );

  const definition = kind ? TRIGGER_KINDS[kind] : null;
  const steps = useMemo(() => (definition ? stepsFor(definition, SHARED_STEPS) : []), [definition]);

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

  const progress = created ? 100 : steps.length ? ((step + 1) / steps.length) * 100 : 0;
  const StepBody = currentStep?.Component;

  return (
    <Modal open onClose={onClose} size="builder" dismissOnOutsideClick={false}>
      <ModalAccessibleTitle>New trigger</ModalAccessibleTitle>

      {/* ── Header ─────────────────────────────────────────────────────────── */}
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        height="header"
        paddingInline="comfortable"
        shrink={false}
      >
        <Icon
          name={definition?.icon ?? 'zap'}
          size="sm"
          container
          containerVariant="accent"
          label=""
        />
        <Stack direction="horizontal" align="baseline" gap="snug">
          <Text as="span" size="supporting" weight="heavy">
            New trigger
          </Text>
          <Text as="span" size="micro" mono color="subtle">
            {definition ? definition.id : 'choose a kind'}
          </Text>
        </Stack>
        <Stack grow />
        <Button variant="ghost" size="icon-sm" aria-label="Close wizard" onClick={onClose}>
          <Icon name="close" />
        </Button>
      </Stack>

      {/* ── Step progress ──────────────────────────────────────────────────── */}
      <Progress size="hairline" label="Trigger progress" value={progress} />

      {created && definition && state ? (
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
      ) : !definition || !state ? (
        /* ── Phase one: choose what starts the run ─────────────────────────── */
        <Stack grow overflowY="auto" align="center">
          <Stack minWidth="document" maxWidth="document" padding="loose" gap="relaxed">
            <Stack gap="snug">
              <Heading variant="subsection">New trigger</Heading>
              <Text size="supporting" color="muted">
                Choose what starts the run. Every trigger targets one agent and is policy-checked
                before it fires.
              </Text>
            </Stack>
            <KindPicker onSelect={chooseKind} />
          </Stack>
        </Stack>
      ) : (
        <Stack direction="horizontal" grow overflow="hidden">
          <WizardRail
            steps={steps}
            current={step}
            maxVisited={maxVisited}
            onGoTo={(i) => {
              setStep(i);
              setError(null);
            }}
          />
          <Separator orientation="vertical" span="full" />

          {/* ── Content column ─────────────────────────────────────────────── */}
          <Stack grow overflow="hidden">
            <Stack grow overflowY="auto" align="center">
              <Stack minWidth="document" maxWidth="document" padding="loose" gap="relaxed">
                <Stack gap="snug">
                  <Heading variant="subsection">{currentStep?.railTitle}</Heading>
                  <Text size="supporting" color="muted">
                    {currentStep?.description}
                  </Text>
                </Stack>
                {StepBody && (
                  <StepBody
                    state={state}
                    patch={patch}
                    context={context}
                    definition={definition}
                    error={error}
                  />
                )}
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
              <Button variant="secondary" onClick={handleBack} disabled={pending}>
                <Icon name="chevron-left" />
                {step === 0 ? 'Choose kind' : 'Back'}
              </Button>
              <Stack grow />
              {!validation.valid && validation.hint && (
                <Text as="span" size="caption" color="subtle">
                  {validation.hint}
                </Text>
              )}
              <Text as="span" size="caption" color="subtle">
                Step {step + 1} of {steps.length}
              </Text>
              {isLast ? (
                <Button onClick={() => void handleNext()} disabled={pending}>
                  {pending ? 'Creating…' : 'Create trigger'}
                  <Icon name="rocket" />
                </Button>
              ) : (
                <Button onClick={() => void handleNext()} disabled={!validation.valid}>
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
