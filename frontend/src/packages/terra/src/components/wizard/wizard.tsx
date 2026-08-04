'use client';

/** @tier 1 — Zero Orbit domain knowledge. Multi-step builder modal chrome: rail, step, review card, footer. */

import * as React from 'react';
import { Button } from '../button/button';
import { Card, CardContent, CardHeader } from '../card/card';
import { Eyebrow } from '../eyebrow/eyebrow';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Progress } from '../progress/progress';
import { Separator } from '../separator/separator';
import { StepConnector } from '../step-dot/step-connector';
import { StepDot } from '../step-dot/step-dot';
import { Text } from '../typography/typography';
import type { IconName } from '../icon/icon';

// ── Interfaces ────────────────────────────────────────────────────────────────

export interface WizardRailStep {
  id: string;
  title: string;
  sub: string;
}

export interface WizardRailProps {
  /** Eyebrow label at the top of the rail (e.g. "Create agent", "New trigger"). */
  label: string;
  steps: WizardRailStep[];
  current: number;
  /** Highest step index reached; steps beyond it are disabled. */
  maxVisited: number;
  onGoTo: (index: number) => void;
  /** Info card rendered at the bottom of the rail. Copy differs between wizards. */
  footer?: React.ReactNode;
}

export interface WizardHeaderProps {
  icon: IconName;
  title: string;
  /** Subtitle chip — the draft id or kind name. Omit when there is nothing meaningful to show. */
  subtitle?: string;
  /** Feature-specific controls between the subtitle and the close button (e.g. save-status indicator). */
  actions?: React.ReactNode;
  onClose: () => void;
}

export interface WizardFooterProps {
  step: number;
  totalSteps: number;
  /** Omit to hide the Back button (agent builder hides it at step 0). */
  onBack?: () => void;
  /** @default "Back" */
  backLabel?: string;
  onNext: () => void;
  nextDisabled?: boolean;
  /** Swaps Continue → Submit on the final step. */
  onSubmit?: () => void;
  /** @default "Create" */
  submitLabel?: string;
  /** Disables both buttons and shows pendingLabel on the primary action. */
  pending?: boolean;
  /** @default "Creating…" */
  pendingLabel?: string;
  /** Validation hint shown between the step counter and the primary action. */
  hint?: string;
}

export interface WizardReviewCardProps {
  icon: IconName;
  title: string;
  rows: { label: string; value: string; mono?: boolean }[];
  /** Renders an Edit button in the card header when provided. */
  onEdit?: () => void;
  /** Block-level content below the rows (e.g. a multi-line prompt block). */
  children?: React.ReactNode;
}

export interface WizardProps {
  /** Must be a <Wizard.Rail> element. */
  rail: React.ReactElement<WizardRailProps>;
  /** Pass <Wizard.Footer> with the caller's step handlers. */
  footer: React.ReactElement<WizardFooterProps>;
  children: React.ReactNode;
}

// ── Sub-components ────────────────────────────────────────────────────────────

const WizardHeader = React.forwardRef<HTMLDivElement, WizardHeaderProps>(
  ({ icon, title, subtitle, actions, onClose }, ref) => (
    <Stack
      ref={ref}
      data-slot="wizard-header"
      direction="horizontal"
      align="center"
      gap="cozy"
      height="header"
      paddingInline="comfortable"
      shrink={false}
    >
      <Icon name={icon} size="sm" container containerVariant="accent" label="" />
      <Stack direction="horizontal" align="baseline" gap="snug">
        <Text as="span" size="supporting" weight="heavy">
          {title}
        </Text>
        {subtitle && (
          <Text as="span" size="micro" mono color="subtle">
            {subtitle}
          </Text>
        )}
      </Stack>
      <Stack grow />
      {actions}
      <Button variant="ghost" size="icon-sm" aria-label={`Close ${title}`} onClick={onClose}>
        <Icon name="close" />
      </Button>
    </Stack>
  ),
);
WizardHeader.displayName = 'WizardHeader';

const WizardRail = React.forwardRef<HTMLDivElement, WizardRailProps>(
  ({ label, steps, current, maxVisited, onGoTo, footer }, ref) => (
    <Stack
      ref={ref}
      data-slot="wizard-rail"
      minWidth="compact"
      maxWidth="compact"
      shrink={false}
      paddingBlock="relaxed"
      paddingInline="comfortable"
      overflowY="auto"
    >
      <Stack paddingInline="snug" paddingBottom="comfortable">
        <Eyebrow>{label}</Eyebrow>
      </Stack>

      <Stack>
        {steps.map((s, i) => {
          const done = i < current;
          const isCurrent = i === current;
          return (
            <Button
              key={s.id}
              variant="ghost"
              size="tile"
              fullWidth
              disabled={i > maxVisited}
              onClick={() => onGoTo(i)}
              aria-current={isCurrent ? 'step' : undefined}
            >
              <Stack direction="horizontal" gap="cozy" paddingInline="snug">
                <Stack align="center" gap="none" shrink={false}>
                  <StepDot size="lg" state={done ? 'complete' : isCurrent ? 'current' : 'pending'}>
                    {done ? <Icon name="check" size="xs" /> : i + 1}
                  </StepDot>
                  {i < steps.length - 1 && <StepConnector complete={done} />}
                </Stack>
                <Stack gap="tight" paddingBottom="relaxed" paddingTop="tight" textAlign="start">
                  <Text
                    as="span"
                    size="supporting"
                    weight="heavy"
                    color={isCurrent || done ? 'foreground' : 'subtle'}
                  >
                    {s.title}
                  </Text>
                  <Text as="span" size="micro" color="subtle">
                    {s.sub}
                  </Text>
                </Stack>
              </Stack>
            </Button>
          );
        })}
      </Stack>

      <Stack grow />

      {footer}
    </Stack>
  ),
);
WizardRail.displayName = 'WizardRail';

/**
 * Scrollable content region for a single step. Wraps children in the
 * overflowY="auto" scroll layer and the document-width constraint.
 * Render one per active step based on caller step state.
 */
const WizardStep = React.forwardRef<HTMLDivElement, { children: React.ReactNode }>(
  ({ children }, ref) => (
    <Stack ref={ref} data-slot="wizard-step" grow overflowY="auto" align="center">
      <Stack minWidth="document" maxWidth="document" padding="loose" gap="relaxed">
        {children}
      </Stack>
    </Stack>
  ),
);
WizardStep.displayName = 'WizardStep';

/** Gap container for a stack of Wizard.ReviewCard children. Used inside Wizard.Step on review screens. */
const WizardReviewStep = React.forwardRef<HTMLDivElement, { children: React.ReactNode }>(
  ({ children }, ref) => (
    <Stack ref={ref} data-slot="wizard-review-step" gap="cozy">
      {children}
    </Stack>
  ),
);
WizardReviewStep.displayName = 'WizardReviewStep';

const WizardReviewCard = React.forwardRef<HTMLDivElement, WizardReviewCardProps>(
  ({ icon, title, rows, onEdit, children }, ref) => (
    <Card ref={ref} data-slot="wizard-review-card">
      <CardHeader
        compact
        divider
        leadingIcon={icon}
        title={title}
        action={
          onEdit && (
            <Button variant="ghost" size="xs" onClick={onEdit}>
              Edit
            </Button>
          )
        }
      />
      <CardContent padded compact>
        <Stack gap="none">
          {rows.map((row, i) => (
            <Stack key={row.label} gap="none">
              {i > 0 && <Separator span="full" />}
              <Stack direction="horizontal" align="center" gap="comfortable" paddingBlock="snug">
                <Text as="span" size="caption" color="subtle">
                  {row.label}
                </Text>
                <Stack grow />
                <Text as="span" size="caption" mono={row.mono ?? false} align="end">
                  {row.value}
                </Text>
              </Stack>
            </Stack>
          ))}
          {children && (
            <>
              <Separator span="full" />
              {children}
            </>
          )}
        </Stack>
      </CardContent>
    </Card>
  ),
);
WizardReviewCard.displayName = 'WizardReviewCard';

const WizardFooter = React.forwardRef<HTMLDivElement, WizardFooterProps>(
  (
    {
      step,
      totalSteps,
      onBack,
      backLabel = 'Back',
      onNext,
      nextDisabled,
      onSubmit,
      submitLabel = 'Create',
      pending,
      pendingLabel = 'Creating…',
      hint,
    },
    ref,
  ) => (
    <Stack
      ref={ref}
      data-slot="wizard-footer"
      direction="horizontal"
      align="center"
      gap="comfortable"
      padding="comfortable"
      paddingInline="relaxed"
      shrink={false}
    >
      {onBack && (
        <Button variant="secondary" onClick={onBack} disabled={pending}>
          <Icon name="chevron-left" />
          {backLabel}
        </Button>
      )}
      <Stack grow />
      {hint && (
        <Text as="span" size="caption" color="subtle">
          {hint}
        </Text>
      )}
      <Text as="span" size="caption" color="subtle">
        Step {step + 1} of {totalSteps}
      </Text>
      {onSubmit ? (
        <Button onClick={onSubmit} disabled={pending || nextDisabled}>
          {pending ? pendingLabel : submitLabel}
          <Icon name="rocket" />
        </Button>
      ) : (
        <Button onClick={onNext} disabled={pending || nextDisabled}>
          {pending ? pendingLabel : 'Continue'}
          <Icon name="arrow-right" />
        </Button>
      )}
    </Stack>
  ),
);
WizardFooter.displayName = 'WizardFooter';

// ── Root ──────────────────────────────────────────────────────────────────────

/**
 * Renders Wizard.Header, the hairline Progress strip, and the two-column body:
 * rail on the left, then a scrollable content column + footer on the right.
 * Children should include exactly one Wizard.Header and one or more step
 * regions (Wizard.Step or Wizard.ReviewStep wrappers).
 */
const WizardRoot = React.forwardRef<HTMLDivElement, WizardProps>(
  ({ rail, footer, children }, ref) => {
    const footerEl = footer as React.ReactElement<WizardFooterProps>;
    const progressValue =
      ((footerEl.props.step + 1) / Math.max(1, footerEl.props.totalSteps)) * 100;

    const childArray = React.Children.toArray(children);
    const header = childArray.find(
      (child) => React.isValidElement(child) && child.type === WizardHeader,
    );
    const body = childArray.filter(
      (child) => !(React.isValidElement(child) && child.type === WizardHeader),
    );

    return (
      <Stack ref={ref} data-slot="wizard" grow overflow="hidden">
        {header}
        <Progress size="hairline" label="Wizard progress" value={progressValue} />
        <Stack direction="horizontal" grow overflow="hidden">
          {rail}
          <Separator orientation="vertical" span="full" />
          <Stack grow overflow="hidden">
            {body}
            <Separator span="full" />
            {footer}
          </Stack>
        </Stack>
      </Stack>
    );
  },
);
WizardRoot.displayName = 'Wizard';

// ── Compound export ───────────────────────────────────────────────────────────

export const Wizard = Object.assign(WizardRoot, {
  Header: WizardHeader,
  Rail: WizardRail,
  Step: WizardStep,
  ReviewStep: WizardReviewStep,
  ReviewCard: WizardReviewCard,
  Footer: WizardFooter,
});
