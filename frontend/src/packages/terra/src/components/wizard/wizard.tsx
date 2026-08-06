'use client';

/** @tier 1 — Zero Orbit domain knowledge. Multi-step builder modal chrome: rail, step, review card, footer. */

import * as React from 'react';
import { Button } from '../button/button';
import { Card, CardContent, CardHeader } from '../card/card';
import { Eyebrow } from '../eyebrow/eyebrow';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Modal, ModalAccessibleTitle } from '../modal/modal';
import { Progress } from '../progress/progress';
import { Separator } from '../separator/separator';
import { StepConnector } from '../step-dot/step-connector';
import { Show } from '../show/show';
import { StepDot } from '../step-dot/step-dot';
import { Text } from '../typography/typography';
import type { IconName } from '../icon/icon';

// ── Interfaces ────────────────────────────────────────────────────────────────

export interface WizardRailStep {
  id: string;
  title: string;
  /** Abbreviated label for the mobile horizontal rail. Falls back to `title` when omitted. */
  shortTitle?: string;
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

interface WizardHeaderProps {
  icon: IconName;
  title: string;
  subtitle?: string;
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

export interface WizardStepProps {
  children: React.ReactNode;
  /**
   * When true, the step fills the full modal body with no rail or footer.
   * Use for pre-flow (kind picker) and post-flow (success) phases.
   * Progress bar displays a frozen value while any fullPage step is active.
   */
  fullPage?: boolean;
}

export interface WizardProps {
  open: boolean;
  onClose: () => void;
  icon: IconName;
  title: string;
  /** Subtitle chip — the draft id or kind name. Omit when there is nothing meaningful to show. */
  subtitle?: string;
  /** Feature-specific controls between the subtitle and the close button (e.g. save-status indicator). */
  actions?: React.ReactNode;
  /** Must be a <Wizard.Rail> element. Omit when the active step is fullPage. */
  rail?: React.ReactElement<WizardRailProps>;
  /** Pass <Wizard.Footer> with the caller's step handlers. Omit when the active step is fullPage. */
  footer?: React.ReactElement<WizardFooterProps>;
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
      shrink={false}
      className="pl-4"
    >
      <Icon name={icon} size="sm" container containerVariant="accent" label="" />
      <Text as="span" size="supporting" weight="heavy">
        {title}
      </Text>
      <Stack grow />
      {subtitle && (
        <Text as="span" size="micro" mono color="subtle">
          {subtitle}
        </Text>
      )}
      <Stack direction="horizontal" align="stretch" gap="none" className="h-full">
        {actions}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Close ${title}`}
          onClick={onClose}
          className="h-full w-auto aspect-square rounded-none lg:rounded-tr-xl"
        >
          <Icon name="close" />
        </Button>
      </Stack>
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
 * Horizontal step rail for narrow viewports. Renders step dots in a row with
 * horizontal connectors between them and labels beneath. The outer element
 * scrolls horizontally when steps overflow the screen width.
 * Rendered automatically by WizardRoot below the lg breakpoint.
 */
const WizardMobileRail = ({ steps, current, maxVisited, onGoTo }: WizardRailProps) => (
  <Stack
    data-slot="wizard-mobile-rail"
    direction="horizontal"
    overflow="auto"
    shrink={false}
    paddingBlock="comfortable"
    paddingInline="comfortable"
    gap="none"
    className="overflow-y-hidden !px-2 lg:!px-4"
  >
    <Stack direction="horizontal" align="center" gap="none" className="min-w-full justify-center">
      {steps.map((s, i) => {
        const done = i < current;
        const isCurrent = i === current;
        return (
          <React.Fragment key={s.id}>
            <Stack
              align="center"
              gap="tight"
              shrink={false}
              className="flex-1 min-w-[4rem] max-w-[6rem]"
            >
              <Button
                variant="ghost"
                size="icon"
                disabled={i > maxVisited}
                onClick={() => onGoTo(i)}
                aria-current={isCurrent ? 'step' : undefined}
              >
                <StepDot size="lg" state={done ? 'complete' : isCurrent ? 'current' : 'pending'}>
                  {done ? <Icon name="check" size="xs" /> : i + 1}
                </StepDot>
              </Button>
              <Text
                as="span"
                size="micro"
                color={isCurrent || done ? 'foreground' : 'subtle'}
                align="center"
              >
                {s.shortTitle ?? s.title}
              </Text>
            </Stack>
            {i < steps.length - 1 && <StepConnector orientation="horizontal" complete={done} />}
          </React.Fragment>
        );
      })}
    </Stack>
  </Stack>
);

/**
 * Scrollable content region for a single step. Wraps children in the
 * overflowY="auto" scroll layer and the document-width constraint.
 * Render one per active step based on caller step state.
 *
 * Pass `fullPage` to fill the entire modal body with no rail or footer —
 * use for pre-flow (kind picker) and post-flow (success) phases.
 */
const WizardStep = React.forwardRef<HTMLDivElement, WizardStepProps>(
  ({ children, fullPage }, ref) => {
    if (fullPage) {
      return (
        <Stack ref={ref} data-slot="wizard-step-full" grow overflow="hidden">
          {children}
        </Stack>
      );
    }
    return (
      <Stack ref={ref} data-slot="wizard-step" grow overflowY="auto" align="center">
        <Stack
          minWidth={{ lg: 'document' }}
          maxWidth={{ lg: 'document' }}
          padding="loose"
          gap="relaxed"
          className="w-full lg:w-auto !px-4 lg:!px-8"
        >
          {children}
        </Stack>
      </Stack>
    );
  },
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
    },
    ref,
  ) => (
    <Stack
      ref={ref}
      data-slot="wizard-footer"
      direction="horizontal"
      align="center"
      padding="comfortable"
      paddingInline="relaxed"
      shrink={false}
      className="lg:py-[--spacing-comfortable] py-[--spacing-relaxed] gap-3 lg:gap-[--spacing-comfortable] !px-4 lg:!px-6"
    >
      {/* Mobile: one-third back cell */}
      <Stack grow className="basis-0 lg:hidden">
        {onBack && (
          <Button
            variant="secondary"
            size="lg"
            responsiveSize="xl"
            onClick={onBack}
            disabled={pending}
          >
            <Icon name="chevron-left" />
            {backLabel}
          </Button>
        )}
      </Stack>

      {/* Desktop: back button */}
      <Show above="lg">
        {onBack && (
          <Button variant="secondary" onClick={onBack} disabled={pending}>
            <Icon name="chevron-left" />
            {backLabel}
          </Button>
        )}
      </Show>

      {/* Desktop: grow spacer pushes right-side group to the right */}
      <Stack grow className="hidden lg:flex" />

      {/* Mobile: two-thirds continue cell; Desktop: step count + continue grouped right */}
      <Stack
        direction="horizontal"
        align="center"
        gap="comfortable"
        className="basis-0 [flex-grow:2] lg:[flex-grow:0] lg:basis-auto"
      >
        <Text as="span" size="caption" color="subtle" align="center" className="hidden lg:block">
          Step {step + 1} of {totalSteps}
        </Text>
        <Stack className="flex-1 lg:flex-none">
          {onSubmit ? (
            <Button
              fullWidth
              size="md"
              responsiveSize="xl"
              onClick={onSubmit}
              disabled={pending || nextDisabled}
            >
              {pending ? pendingLabel : submitLabel}
              <Icon name="rocket" />
            </Button>
          ) : (
            <Button
              fullWidth
              size="md"
              responsiveSize="xl"
              onClick={onNext}
              disabled={pending || nextDisabled}
            >
              {pending ? pendingLabel : 'Continue'}
              <Icon name="arrow-right" />
            </Button>
          )}
        </Stack>
      </Stack>
    </Stack>
  ),
);
WizardFooter.displayName = 'WizardFooter';

// ── Root ──────────────────────────────────────────────────────────────────────

/**
 * Full builder modal: owns the Modal shell, renders the header and progress bar,
 * and lays out the rail + scrollable content column + footer.
 *
 * Pass `open`, `onClose`, `icon`, and `title` as props. Children should be
 * one or more Wizard.Step (or Wizard.ReviewStep) elements. When a Wizard.Step
 * with `fullPage` is active, the rail and footer are hidden and the progress
 * bar freezes at its last non-fullPage value.
 */
const WizardRoot = React.forwardRef<HTMLDivElement, WizardProps>(
  ({ open, onClose, icon, title, subtitle, actions, rail, footer, children }, ref) => {
    const [frozenProgress, setFrozenProgress] = React.useState(0);

    const childArray = React.Children.toArray(children);
    const activeIsFullPage = childArray.some(
      (child) =>
        React.isValidElement(child) &&
        child.type === WizardStep &&
        (child as React.ReactElement<WizardStepProps>).props.fullPage === true,
    );

    const footerEl = footer ? (footer as React.ReactElement<WizardFooterProps>) : null;
    const progressValue = footerEl
      ? ((footerEl.props.step + 1) / Math.max(1, footerEl.props.totalSteps)) * 100
      : 0;

    React.useEffect(() => {
      if (!activeIsFullPage) {
        setFrozenProgress(progressValue);
      }
    }, [activeIsFullPage, progressValue]);

    const displayProgress = activeIsFullPage ? frozenProgress : progressValue;

    return (
      <Modal open={open} onClose={onClose} size="builder" dismissOnOutsideClick={false}>
        <ModalAccessibleTitle>{title}</ModalAccessibleTitle>
        <Stack ref={ref} data-slot="wizard" grow overflow="hidden" gap="none">
          <WizardHeader
            icon={icon}
            title={title}
            subtitle={subtitle}
            actions={actions}
            onClose={onClose}
          />
          <Progress size="hairline" label="Wizard progress" value={displayProgress} />
          {activeIsFullPage ? (
            <Stack grow overflow="hidden" gap="none">
              {children}
            </Stack>
          ) : (
            <Stack grow overflow="hidden" gap="none">
              {rail && (
                <Show below="lg">
                  <WizardMobileRail {...(rail as React.ReactElement<WizardRailProps>).props} />
                  <Separator span="full" />
                </Show>
              )}
              <Stack
                direction={{ base: 'vertical', lg: 'horizontal' }}
                grow
                overflow="hidden"
                gap="none"
              >
                {rail && (
                  // `contents` makes the wrapper transparent to flex layout so
                  // the rail and vertical separator are direct flex children of
                  // the row — required for the separator's h-full to resolve.
                  <Stack className="hidden lg:contents" gap="none">
                    {rail}
                    <Separator orientation="vertical" span="full" />
                  </Stack>
                )}
                <Stack grow overflow="hidden" gap="none">
                  {children}
                  <Separator span="full" />
                  {footer}
                </Stack>
              </Stack>
            </Stack>
          )}
        </Stack>
      </Modal>
    );
  },
);
WizardRoot.displayName = 'Wizard';

// ── Compound export ───────────────────────────────────────────────────────────

export const Wizard = Object.assign(WizardRoot, {
  Rail: WizardRail,
  Step: WizardStep,
  ReviewStep: WizardReviewStep,
  ReviewCard: WizardReviewCard,
  Footer: WizardFooter,
});
