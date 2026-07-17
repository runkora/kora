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
  ({ icon, title, description }, ref) => (
    <Stack ref={ref} data-slot="section-heading" direction="horizontal" gap="cozy" align="start">
      <Icon name={icon} size="md" color="accent" container containerVariant="accent" label="" />
      <Stack gap="tight">
        <Heading variant="subsection">{title}</Heading>
        {description ? (
          <Text size="supporting" color="muted">
            {description}
          </Text>
        ) : null}
      </Stack>
    </Stack>
  ),
);

SectionHeading.displayName = 'SectionHeading';

export { SectionHeading };
