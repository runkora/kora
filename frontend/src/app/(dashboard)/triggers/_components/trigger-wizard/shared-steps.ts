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
  railSub: 'Which agent',
  description: 'The agent this trigger runs.',
  Component: StepTarget,
  validate: (state) => validateTarget(state),
};

export const DELIVERY_STEP: KindStep = {
  id: 'delivery',
  railTitle: 'Delivery',
  railSub: 'Capacity and safety',
  description:
    'What happens between a delivery arriving and its run starting: capacity, overlap, duplicates, and failure.',
  Component: StepDelivery,
  validate: (state) => validateDelivery(state),
};

export const REVIEW_STEP: KindStep = {
  id: 'review',
  railTitle: 'Review',
  railSub: 'Confirm and create',
  description: 'Confirm the trigger. Nothing fires until you create it.',
  Component: StepReview,
  validate: () => VALID,
};

/** Appended, in order, to every kind's own steps. */
export const SHARED_STEPS: KindStep[] = [TARGET_STEP, DELIVERY_STEP, REVIEW_STEP];
