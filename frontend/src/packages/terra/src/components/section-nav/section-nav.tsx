'use client';

import * as React from 'react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import type { IconName } from '../icon/registry';
import { Stack } from '../layout/stack';
import { StepDot } from '../step-dot/step-dot';
import { Text } from '../typography/typography';

export interface SectionNavSection {
  /** Stable key reported through `onSelect` and matched against `active`. */
  key: string;
  /** Entry title. */
  title: string;
  /** Optional supporting line under the title. */
  sub?: string;
  /** Icon shown (in an accent tile) when the entry is active. */
  icon: IconName;
}

export interface SectionNavProps {
  /** The entries, in order. Inactive entries show their 1-based index in a StepDot. */
  sections: SectionNavSection[];
  /** Key of the active section. */
  active: string;
  /** Called with the section key when an entry is clicked. */
  onSelect: (key: string) => void;
}

/**
 * SectionNav
 *
 * A vertical section rail in the builder's anatomy: each entry is a tile
 * button showing a numbered step dot when inactive and an accent icon tile
 * when active. Used for in-page section navigation (e.g. a configuration
 * tab's rail).
 *
 * ```tsx
 * <SectionNav
 *   sections={[
 *     { key: 'identity', title: 'Identity', sub: 'Name & description', icon: 'user' },
 *     { key: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
 *   ]}
 *   active={active}
 *   onSelect={setActive}
 * />
 * ```
 */
const SectionNav = React.forwardRef<HTMLDivElement, SectionNavProps>(
  ({ sections, active, onSelect }, ref) => (
    <Stack ref={ref} data-slot="section-nav" gap="tight">
      {sections.map((section, i) => {
        const isActive = section.key === active;
        return (
          <Button
            key={section.key}
            variant={isActive ? 'default' : 'ghost'}
            size="tile"
            fullWidth
            onClick={() => onSelect(section.key)}
            aria-current={isActive ? 'true' : undefined}
          >
            <Stack direction="horizontal" align="center" gap="cozy" padding="snug" grow>
              {isActive ? (
                <Icon name={section.icon} size="sm" container containerVariant="surface" label="" />
              ) : (
                <StepDot size="lg" state="pending">
                  {i + 1}
                </StepDot>
              )}
              <Stack gap="tight" align="start" textAlign="start">
                <Text
                  as="span"
                  size="supporting"
                  weight="heavy"
                  color="muted"
                  className={isActive ? 'text-accent-foreground' : undefined}
                >
                  {section.title}
                </Text>
                {section.sub ? (
                  <Text
                    as="span"
                    size="micro"
                    color="subtle"
                    className={isActive ? 'text-accent-foreground' : undefined}
                  >
                    {section.sub}
                  </Text>
                ) : null}
              </Stack>
            </Stack>
          </Button>
        );
      })}
    </Stack>
  ),
);

SectionNav.displayName = 'SectionNav';

export { SectionNav };
