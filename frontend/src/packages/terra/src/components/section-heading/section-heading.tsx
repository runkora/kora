import * as React from 'react';
import { Stack } from '../layout/stack';
import { Heading, Text } from '../typography/typography';
import { Icon } from '../icon/icon';
import type { IconName } from '../icon/registry';

export interface SectionHeadingProps {
  /** Icon rendered in an accent container tile to the left of the title. */
  icon: IconName;
  /** Section title — renders as a `Heading variant="subsection"` (`<h3>`). */
  title: string;
  /** Optional purpose statement under the title — supporting, muted. */
  description?: string;
  /** Section body content. Rendered below the header row, indented to align with the title. */
  children?: React.ReactNode;
}

/**
 * SectionHeading
 *
 * The "builder motif" section opener: an accent icon tile beside a subsection
 * title and an optional purpose statement. Use at the top of a wizard step or
 * a configuration section.
 *
 * ```tsx
 * <SectionHeading
 *   icon="user"
 *   title="Identity"
 *   description="Name your agent and describe what it does."
 * />
 * ```
 */
const SectionHeading = React.forwardRef<HTMLDivElement, SectionHeadingProps>(
  ({ icon, title, description, children }, ref) => (
    <Stack ref={ref} data-slot="section-heading" gap="tight">
      <Stack direction="horizontal" gap="cozy" align="center">
        <Icon name={icon} size="md" color="accent" container containerVariant="accent" label="" />
        <Heading variant="subsection">{title}</Heading>
      </Stack>
      {description ? (
        <Text size="supporting" color="muted" className="pl-12">
          {description}
        </Text>
      ) : null}
      {children ? (
        <Stack gap="relaxed" paddingTop="snug">
          {children}
        </Stack>
      ) : null}
    </Stack>
  ),
);

SectionHeading.displayName = 'SectionHeading';

export { SectionHeading };
