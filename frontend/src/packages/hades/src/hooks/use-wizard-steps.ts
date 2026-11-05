'use client';

/** @tier 1 — Zero kora domain knowledge. Step navigation and validation hints for a `Wizard`. */

import { useCallback, useState } from 'react';

export interface UseWizardStepsOptions {
  stepCount: number;
  /** One entry per step: why it cannot be left yet, or undefined when valid. */
  hints: (string | undefined)[];
  /** Navigation is ignored while true. The wizard passes its own `submitting`. */
  locked?: boolean;
  /**
   * The step that stands for every step before it. On it, `hint` is the first defined entry in
   * `hints`, so review cannot be reached or submitted with an earlier step invalid. Omit for
   * wizards whose review has no such rule.
   */
  reviewStep?: number;
  /** @default 0 */
  initialStep?: number;
  /** @default initialStep */
  initialMaxVisited?: number;
  /** Runs on every accepted navigation, e.g. to clear a submit error the wizard owns. */
  onNavigate?: () => void;
}

export interface UseWizardStepsReturn {
  step: number;
  /** Highest step index reached; it only rises until `reset`. */
  maxVisited: number;
  /** Ignored while locked. The target is clamped to `[0, stepCount - 1]`, so `next` on the last step and `back` on the first are no-ops that stay in range. */
  goTo: (index: number) => void;
  next: () => void;
  back: () => void;
  /** True on the first step: omit Back. */
  isFirst: boolean;
  /** True on the last step: pass `onSubmit`. */
  isLast: boolean;
  /** Why the current step cannot be left yet, or undefined when it is valid. */
  hint: string | undefined;
  /** True while `hint` is set. */
  nextDisabled: boolean;
  /** Back to `initialStep` and `initialMaxVisited`. */
  reset: () => void;
}

/**
 * Tracks the current step, the furthest step reached, and the hint for the current step. Pass the
 * result to `Wizard.Rail`, `Wizard.Footer` and the `hint` prop of `Wizard`. It does not own the
 * submit lifecycle, errors or the success screen.
 */
export function useWizardSteps({
  stepCount,
  hints,
  locked = false,
  reviewStep,
  initialStep = 0,
  initialMaxVisited = initialStep,
  onNavigate,
}: UseWizardStepsOptions): UseWizardStepsReturn {
  const [step, setStep] = useState(initialStep);
  const [maxVisited, setMaxVisited] = useState(initialMaxVisited);

  const goTo = useCallback(
    (index: number) => {
      if (locked) return;
      const target = Math.min(Math.max(index, 0), Math.max(stepCount - 1, 0));
      setStep(target);
      setMaxVisited((m) => Math.max(m, target));
      onNavigate?.();
    },
    [locked, stepCount, onNavigate],
  );

  const next = useCallback(() => goTo(step + 1), [goTo, step]);
  const back = useCallback(() => goTo(step - 1), [goTo, step]);

  const reset = useCallback(() => {
    setStep(initialStep);
    setMaxVisited(initialMaxVisited);
  }, [initialStep, initialMaxVisited]);

  const hint = step === reviewStep ? hints.find((h) => h !== undefined) : hints[step];

  return {
    step,
    maxVisited,
    goTo,
    next,
    back,
    isFirst: step === 0,
    isLast: step === stepCount - 1,
    hint,
    nextDisabled: hint !== undefined,
    reset,
  };
}
