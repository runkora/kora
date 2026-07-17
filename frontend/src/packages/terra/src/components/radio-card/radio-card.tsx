'use client';

import * as React from 'react';
import { Card, CardContent } from '../card/card';
import { Stack } from '../layout/stack';
import { Icon } from '../icon/icon';
import type { IconName } from '../icon/registry';
import { RadioGroupItem } from '../radio-group/radio-group';
import { Text } from '../typography/typography';

export interface RadioCardProps {
  /** The value this card contributes to the surrounding `RadioGroup`. */
  value: string;
  /** Whether this card is the selected option. Drives the card tint and icon accent. */
  selected: boolean;
  /** Called with `value` when the card (anywhere on its surface) is clicked. */
  onSelect: (value: string) => void;
  /** Optional icon rendered in a tile at the top-left of the card. */
  icon?: IconName;
  /** Option title. Also used as the radio control's accessible label. */
  title: string;
  /** Optional supporting line under the title. */
  description?: string;
}

/**
 * RadioCard
 *
 * A single-select option card: icon tile, radio control, title, and optional
 * description on a `Card interactive selected` surface. **Must be rendered
 * inside a `RadioGroup`** — the embedded `RadioGroupItem` requires the Radix
 * group context.
 *
 * The card mirrors the group value through `selected` and reports clicks
 * through `onSelect`, so the caller keeps the group's `value`/`onValueChange`
 * as the single source of truth.
 *
 * ```tsx
 * <RadioGroup value={framework} onValueChange={setFramework} aria-label="Framework">
 *   <Grid cols={3} gap="cozy">
 *     {options.map((o) => (
 *       <RadioCard
 *         key={o.id}
 *         value={o.id}
 *         selected={framework === o.id}
 *         onSelect={setFramework}
 *         icon="square-terminal"
 *         title={o.name}
 *         description={o.desc}
 *       />
 *     ))}
 *   </Grid>
 * </RadioGroup>
 * ```
 */
const RadioCard = React.forwardRef<HTMLDivElement, RadioCardProps>(
  ({ value, selected, onSelect, icon, title, description }, ref) => (
    <Card ref={ref} interactive selected={selected} onClick={() => onSelect(value)}>
      <CardContent padded compact>
        <Stack gap="snug" data-slot="radio-card">
          <Stack direction="horizontal" align="center">
            {icon ? (
              <Icon
                name={icon}
                size="sm"
                color={selected ? 'accent' : 'muted'}
                container
                containerVariant={selected ? 'accent' : 'surface-subtle'}
                label=""
              />
            ) : null}
            <Stack grow />
            <RadioGroupItem value={value} aria-label={title} />
          </Stack>
          <Stack gap="tight">
            <Text as="span" size="supporting" weight="heavy">
              {title}
            </Text>
            {description ? (
              <Text size="micro" color="muted">
                {description}
              </Text>
            ) : null}
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  ),
);

RadioCard.displayName = 'RadioCard';

export { RadioCard };
