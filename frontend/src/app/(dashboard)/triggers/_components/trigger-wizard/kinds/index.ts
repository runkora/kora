/**
 * @tier 3 — triggers-scoped. The trigger-kind registry.
 *
 * The wizard reads this and nothing else: the picker lists it, the step order
 * comes from it, review and the success screen delegate to it. Adding a kind is
 * one module here plus one line in `TRIGGER_KINDS`.
 */

import type { ConsoleKind, PlannedKind, TriggerKindDefinition } from '../types';
import { chatKind, webhookKind } from './webhook';
import { scheduledKind } from './scheduled';
import { manualKind } from './manual';

export const TRIGGER_KINDS: Record<ConsoleKind, TriggerKindDefinition> = {
  webhook: webhookKind,
  chat: chatKind,
  scheduled: scheduledKind,
  manual: manualKind,
};

/** Picker order. Explicit, so it does not depend on object key order. */
export const KIND_ORDER: ConsoleKind[] = ['webhook', 'chat', 'scheduled', 'manual'];

/**
 * Kinds the model accommodates but that are not built.
 *
 * Shown disabled rather than hidden: it says where the product is going without
 * promising a date, and it keeps the design honest. If adding one of these
 * would mean reworking the delivery model, the model is wrong now rather than
 * later.
 */
export const PLANNED_KINDS: PlannedKind[] = [
  {
    icon: 'activity',
    title: 'Control-plane event',
    description: (productName) =>
      `When something happens inside ${productName}: a run finishes, a budget is crossed.`,
  },
  {
    icon: 'route',
    title: 'Chained run',
    description: "When another agent's run reaches a given outcome.",
  },
  {
    icon: 'layers',
    title: 'Queue or stream',
    description: 'When a message lands on a subscribed queue or topic.',
  },
  {
    icon: 'database',
    title: 'Object storage',
    description: 'When a file appears or changes in a watched bucket.',
  },
  {
    icon: 'bell',
    title: 'Monitor or alert',
    description: 'When an external monitoring system fires an alert.',
  },
];

export { webhookKind, chatKind, scheduledKind, manualKind };
