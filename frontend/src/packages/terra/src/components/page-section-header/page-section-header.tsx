import * as React from 'react';
import { cn } from '../../utils';
import { Heading, Text } from '../typography/typography';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface PageSectionHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Primary heading text for the section. Rendered with `Heading variant="title"`.
   */
  title: string;
  /**
   * Optional supporting description rendered below the title.
   * Uses a muted supporting text style.
   */
  description?: string;
  /**
   * Optional action slot aligned to the right edge — typically a single
   * `<Button>` or `<ButtonGroup>`.
   */
  action?: React.ReactNode;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * PageSectionHeader
 *
 * Canonical header row for list and management tabs: title on the left,
 * optional description below it, and an optional action (e.g. "Create"
 * button) aligned to the right.
 *
 * Domain-free — carries no Orbit-specific semantics. The caller supplies the
 * title, description copy, and action button.
 *
 * ```tsx
 * // Title + description + action
 * <PageSectionHeader
 *   title="API Keys"
 *   description="Manage your API keys and monitor their usage."
 *   action={<Button size="sm" onClick={...}>Create key</Button>}
 * />
 *
 * // Title only
 * <PageSectionHeader title="Overview" />
 * ```
 */
const PageSectionHeader = React.forwardRef<HTMLDivElement, PageSectionHeaderProps>(
  ({ title, description, action, className, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="page-section-header"
      className={cn('flex flex-wrap items-start justify-between gap-y-2', className)}
      {...props}
    >
      <div className="flex flex-col">
        <Heading variant="title">{title}</Heading>
        {description && (
          <Text size="supporting" color="muted">
            {description}
          </Text>
        )}
      </div>
      {action && <div data-slot="page-section-header-action">{action}</div>}
    </div>
  ),
);

PageSectionHeader.displayName = 'PageSectionHeader';

export { PageSectionHeader };
