'use client';

/** @tier 3 — triggers-scoped. The New Trigger flow: kind picker, the chosen
 * kind's steps, then the success screen. All three phases render in one frame —
 * same rail, same content measure, same footer band — so moving between them
 * never re-lays-out the modal. Everything kind-specific comes from ./kinds. */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Heading,
  Icon,
  Modal,
  ModalAccessibleTitle,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import { KindPicker } from './kind-picker';
import { WizardRail } from './wizard-rail';
import type { RailStep } from './wizard-rail';
import { SuccessPanel } from './success-panel';
import { PREVIEW_RAIL_TITLES, SHARED_STEPS } from './shared-steps';
import { TRIGGER_KINDS } from './kinds';
import { initialState, stepsFor, toDraft } from './wizard-state';
import type {
  StepValidation,
  TriggerKind,
  TriggerWizardProps,
  WizardContext,
  WizardState,
} from './types';

type Phase = 'picker' | 'steps' | 'done';

interface Created {
  triggerId: string;
  endpoint?: string;
  secret?: string;
}

/** What the rail shows before a kind is chosen: a generic shape of the flow. */
const PREVIEW_STEPS: RailStep[] = PREVIEW_RAIL_TITLES.map((title, i) => ({
  id: `preview-${i}`,
  title,
}));

export function TriggerWizard({
  initialKind,
  agents,
  sources,
  ingestHost,
  onCreate,
  onViewTrigger,
  onClose,
}: TriggerWizardProps) {
  const [phase, setPhase] = useState<Phase>(initialKind ? 'steps' : 'picker');
  const [kind, setKind] = useState<TriggerKind | null>(initialKind ?? null);
  const [state, setState] = useState<WizardState | null>(() =>
    initialKind ? initialState(TRIGGER_KINDS[initialKind], sources) : null,
  );
  const [step, setStep] = useState(0);
  const [maxVisited, setMaxVisited] = useState(0);
  const [attempted, setAttempted] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<Created | null>(null);

  const firstFieldRef = useRef<HTMLInputElement>(null);

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

  // Focus the step's first control on arrival, so typing can start immediately
  // rather than landing on the close button. Deferred by a tick: the dialog
  // claims focus in its own mount effect, which runs after this one.
  const firstCardRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
      if (phase === 'picker') firstCardRef.current?.focus();
      else if (phase === 'steps') firstFieldRef.current?.focus();
    }, 0);
    return () => clearTimeout(timer);
  }, [phase, step]);

  const chooseKind = useCallback(
    (next: TriggerKind) => {
      // Re-picking the same kind keeps everything already typed. Only a real
      // change of kind resets, because only then is the state meaningless.
      setState((prev) =>
        prev && next === kind ? prev : initialState(TRIGGER_KINDS[next], sources),
      );
      if (next !== kind) {
        setStep(0);
        setMaxVisited(0);
      }
      setKind(next);
      setAttempted(false);
      setError(null);
      setPhase('steps');
    },
    [kind, sources],
  );

  /** Back to the picker, keeping the draft so a wrong turn costs nothing. */
  const backToPicker = useCallback(() => {
    setPhase('picker');
    setAttempted(false);
    setError(null);
  }, []);

  /** A fresh trigger: nothing from the last one carries over. */
  const createAnother = useCallback(() => {
    setPhase('picker');
    setKind(null);
    setState(null);
    setStep(0);
    setMaxVisited(0);
    setAttempted(false);
    setError(null);
    setCreated(null);
  }, []);

  const currentStep = steps[step];
  const validation = useMemo<StepValidation>(
    () =>
      definition && state && currentStep ? currentStep.validate(state, context) : { valid: false },
    [definition, state, currentStep, context],
  );
  const isLast = steps.length > 0 && step === steps.length - 1;

  const fieldError = useCallback(
    (id: string) =>
      attempted && !validation.valid && validation.fieldId === id ? validation.blocker : undefined,
    [attempted, validation],
  );

  const handleBack = useCallback(() => {
    setAttempted(false);
    if (step === 0) {
      backToPicker();
      return;
    }
    setStep((s) => s - 1);
  }, [step, backToPicker]);

  const handleNext = useCallback(async () => {
    if (!definition || !state || pending) return;
    if (!validation.valid) {
      // Continue is never disabled: pressing it is how a person asks what is
      // missing, so answer by naming it and putting the cursor on it.
      setAttempted(true);
      if (validation.fieldId) {
        const el = document.getElementById(validation.fieldId);
        if (el instanceof HTMLElement) {
          el.scrollIntoView?.({ block: 'center' });
          el.focus();
        }
      }
      return;
    }
    setAttempted(false);
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
    setPhase('done');
  }, [definition, state, validation, pending, isLast, step, onCreate]);

  // The rail always leads with the kind, so its indices run one ahead of the
  // step array. Before a kind exists the remaining entries are a generic
  // preview, which keeps the rail the same shape in every phase.
  const railSteps: RailStep[] = [
    { id: 'kind', title: 'Kind' },
    ...(definition && steps.length
      ? steps.map((s) => ({ id: s.id, title: s.railTitle }))
      : PREVIEW_STEPS),
  ];
  const railCurrent = phase === 'picker' ? 0 : phase === 'done' ? railSteps.length : step + 1;
  const railMaxVisited =
    phase === 'picker' ? 0 : phase === 'done' ? railSteps.length : maxVisited + 1;

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

      <Separator span="full" />

      <Stack direction="horizontal" grow overflow="hidden">
        <WizardRail
          steps={railSteps}
          current={railCurrent}
          maxVisited={railMaxVisited}
          onGoTo={(i) => {
            if (phase !== 'steps') return;
            if (i === 0) {
              backToPicker();
              return;
            }
            setStep(i - 1);
            setAttempted(false);
            setError(null);
          }}
          kindIcon={definition?.icon}
          kindTitle={definition?.title}
          onChangeKind={phase === 'steps' ? backToPicker : undefined}
        />
        <Separator orientation="vertical" span="full" />

        {/* ── Content column ─────────────────────────────────────────────── */}
        <Stack grow overflow="hidden">
          <Stack grow overflowY="auto" align="center">
            <Stack
              minWidth="document"
              maxWidth="document"
              paddingInline="loose"
              paddingBlock="loose"
              gap="relaxed"
            >
              {phase === 'picker' && (
                <KindPicker onSelect={chooseKind} firstCardRef={firstCardRef} />
              )}

              {phase === 'steps' && definition && state && (
                <>
                  {(currentStep?.railTitle || currentStep?.description) && (
                    <Stack gap="snug">
                      <Heading variant="subsection">{currentStep?.railTitle}</Heading>
                      {currentStep?.description && (
                        <Text size="supporting" color="muted">
                          {currentStep.description}
                        </Text>
                      )}
                    </Stack>
                  )}
                  {StepBody && (
                    <StepBody
                      state={state}
                      patch={patch}
                      context={context}
                      definition={definition}
                      error={error}
                      fieldError={fieldError}
                      firstFieldRef={firstFieldRef}
                    />
                  )}
                </>
              )}

              {phase === 'done' && definition && state && created && (
                <SuccessPanel
                  definition={definition}
                  state={state}
                  context={context}
                  triggerId={created.triggerId}
                  endpoint={created.endpoint}
                  secret={created.secret}
                />
              )}
            </Stack>
          </Stack>

          {/* ── Footer band ───────────────────────────────────────────────── */}
          <Separator span="full" />
          <Stack align="center" shrink={false}>
            <Stack
              direction="horizontal"
              align="center"
              gap="comfortable"
              minWidth="document"
              maxWidth="document"
              paddingInline="loose"
              paddingBlock="comfortable"
            >
              {phase === 'steps' && (
                <Button variant="secondary" onClick={handleBack} disabled={pending}>
                  <Icon name="chevron-left" />
                  Back
                </Button>
              )}
              {phase === 'picker' && (
                <Text as="span" size="caption" color="subtle">
                  Pick a kind
                </Text>
              )}
              {phase === 'steps' && attempted && !validation.valid && validation.blocker && (
                <Text as="span" size="caption" color="status-error">
                  {validation.blocker}
                </Text>
              )}
              <Stack grow />

              {phase === 'steps' &&
                (isLast ? (
                  <Button onClick={() => void handleNext()} disabled={pending}>
                    {pending ? 'Creating…' : 'Create trigger'}
                    <Icon name="rocket" />
                  </Button>
                ) : (
                  <Button onClick={() => void handleNext()}>
                    Continue
                    <Icon name="arrow-right" />
                  </Button>
                ))}

              {phase === 'done' && created && (
                <>
                  <Button variant="ghost" onClick={onClose}>
                    Done
                  </Button>
                  <Button variant="secondary" onClick={createAnother}>
                    Create another
                    <Icon name="plus" />
                  </Button>
                  {onViewTrigger && (
                    <Button onClick={() => onViewTrigger(created.triggerId)}>
                      View trigger
                      <Icon name="arrow-right" />
                    </Button>
                  )}
                </>
              )}
            </Stack>
          </Stack>
        </Stack>
      </Stack>
    </Modal>
  );
}
