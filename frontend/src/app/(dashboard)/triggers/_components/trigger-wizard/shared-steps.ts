/**
 * @tier 3 — triggers-scoped. The three steps every kind ends with. A kind
 * contributes only the steps that describe how it fires; what it runs, how it
 * is delivered, and confirming it are the same regardless.
 */

import { StepTarget } from './step-target';
import { StepDelivery } from './step-delivery';
import { StepReview } from './step-review';
import { VALID, validateDelivery, validateTarget } from './wizard-state';
import type { KindStep } from './types';

export const TARGET_STEP: KindStep = {
  id: 'target',
  railTitle: 'Target',
  description: 'The agent this trigger invokes, and how many of its runs may overlap.',
  Component: StepTarget,
  validate: (state) => validateTarget(state),
};

export const DELIVERY_STEP: KindStep = {
  id: 'delivery',
  railTitle: 'Delivery',
  // No description: a timeout and two switches describe themselves.
  Component: StepDelivery,
  validate: (state) => validateDelivery(state),
};

export const REVIEW_STEP: KindStep = {
  id: 'review',
  railTitle: 'Review',
  description: 'Confirm the trigger.',
  Component: StepReview,
  validate: () => VALID,
};

/** Appended, in order, to every kind's own steps. */
export const SHARED_STEPS: KindStep[] = [TARGET_STEP, DELIVERY_STEP, REVIEW_STEP];

/**
 * Rail titles shown on the picker phase, before a kind exists. The first step
 * is the kind's own, so it is named generically; the rest are the shared three.
 */
export const PREVIEW_RAIL_TITLES: string[] = ['Configure', ...SHARED_STEPS.map((s) => s.railTitle)];
