/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * (`triggers/_components/triggers-page.tsx`) and the agent detail page
 * (`agents/[slug]/_components/agent-triggers-tab.tsx`), which imports this
 * subtree across feature areas.
 *
 * The placement tree would put a component with consumers in two feature areas
 * at Tier 2, and this stays at Tier 3 deliberately: the wizard is twenty files
 * of trigger-domain logic (kind registry, cron builder, field catalog) that
 * would carry the whole Triggers vocabulary into `src/components/`. The
 * cross-feature reach is recorded here instead of being invisible.
 *
 * Public surface of the New Trigger wizard.
 */

export { TriggerWizard } from './trigger-wizard';
export { TRIGGER_KINDS, KIND_ORDER, PLANNED_KINDS } from './kinds';
export type {
  TriggerDraft,
  TriggerWizardProps,
  TriggerWizardAgent,
  TriggerWizardSource,
  TriggerCreateResult,
  TriggerKindDefinition,
  PlannedKind,
  KindStep,
  ReviewRow,
  StepComponentProps,
  StepValidation,
  DoneContentProps,
  WizardContext,
  WizardState,
} from './types';
