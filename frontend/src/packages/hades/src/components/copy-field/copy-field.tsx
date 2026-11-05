'use client';

/** @tier 1 — Zero kora domain knowledge. Could be extracted to a standalone library. */

import * as React from 'react';
import { cn } from '../../utils';
import { useCopy } from '../../hooks/use-copy';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';

export interface CopyFieldProps {
  /** The string value to copy to the clipboard and display. */
  value: string;
  /** Caption shown above the value. Also names the button ("Copy {label}"). */
  label?: string;
  /** Overrides the derived accessible name. */
  ariaLabel?: string;
  /** Render the value in a monospace font. */
  mono?: boolean;
  /**
   * Draw an input-style border. Defaults to true for a bare value and false
   * when `label` or `wrap` is set, which give the borderless detail-row look.
   */
  bordered?: boolean;
  /**
   * Draw a recessed surface (`bg-background`, as `FieldRow` does) with the same
   * subtle border as the default field. Without it the field is transparent
   * and takes the colour of whatever it sits on. Bordered by default; pass
   * `bordered={false}` for the surface alone.
   */
  filled?: boolean;
  /** Let a long value wrap onto several lines, in emphasised weight, instead of truncating. */
  wrap?: boolean;
  /** Expand to fill the parent width. Defaults to true. */
  fullWidth?: boolean;
}

/**
 * A read-only value that copies to the clipboard when the whole field is clicked.
 *
 * Bare, it is a bordered, transparent field. Set `filled` to sit beside `Input`s
 * and `FieldRow`s on a recessed surface.
 */
const CopyField = React.forwardRef<HTMLButtonElement, CopyFieldProps>(
  ({ value, label, ariaLabel, mono, bordered, filled, wrap, fullWidth = true }, ref) => {
    const { status, copy } = useCopy(value);
    // One answer to "is there a border": a filled field has one unless `bordered={false}`,
    // a bare one follows `bordered` or the borderless detail-row look of `label` and `wrap`.
    const showBorder = filled ? bordered !== false : (bordered ?? !(label || wrap));
    const boxed = showBorder || filled;
    // The name carries the value, so a screen reader hears what is being copied.
    const accessibleName = ariaLabel ?? (label ? `Copy ${label}: ${value}` : `Copy ${value}`);

    return (
      <Button
        ref={ref}
        type="button"
        data-slot="copy-field"
        variant="ghost"
        size="sm"
        fullWidth={fullWidth}
        onClick={copy}
        aria-label={accessibleName}
        className={cn(
          !fullWidth && 'self-start',
          boxed && 'rounded-md',
          showBorder && 'border border-border-subtle',
          filled ? 'bg-background px-[13px]' : showBorder && 'px-2.5',
          boxed && !label && !wrap && 'h-9',
          label && !wrap && 'h-auto min-h-9 py-1.5',
          wrap && 'h-auto min-h-8 whitespace-normal pt-2.5 pb-2',
        )}
      >
        <Stack
          direction="horizontal"
          align="center"
          justify="between"
          grow
          textAlign="start"
          minWidth="none"
          gap="snug"
        >
          <Stack gap="none" grow minWidth="none">
            {label && (
              <Text size="caption" color="muted">
                {label}
              </Text>
            )}
            <Text
              size="supporting"
              weight={wrap ? 'emphasis' : undefined}
              mono={mono}
              truncate={!wrap}
              className={wrap ? 'whitespace-normal break-words' : undefined}
            >
              {value}
            </Text>
          </Stack>
          {status === 'copied' ? (
            <Icon name="check" size="2xs" color="status-healthy" shrink />
          ) : status === 'failed' ? (
            <Icon name="circle-x" size="2xs" color="status-error" shrink />
          ) : (
            <Icon name="copy" size="2xs" color={wrap ? 'muted' : 'subtle'} shrink />
          )}
        </Stack>
      </Button>
    );
  },
);
CopyField.displayName = 'CopyField';

export { CopyField };
