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
  /** Let a long value wrap onto several lines, in emphasised weight, instead of truncating. */
  wrap?: boolean;
  /** Expand to fill the parent width. Defaults to true. */
  fullWidth?: boolean;
}

/** A read-only value that copies to the clipboard when the whole field is clicked. */
const CopyField = React.forwardRef<HTMLButtonElement, CopyFieldProps>(
  ({ value, label, ariaLabel, mono, bordered, wrap, fullWidth = true }, ref) => {
    const { status, copy } = useCopy(value);
    const hasBorder = bordered ?? !(label || wrap);
    const accessibleName = ariaLabel ?? (label ? `Copy ${label}` : 'Copy to clipboard');

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
          hasBorder && 'rounded-md border border-border px-2.5',
          hasBorder && !label && !wrap && 'h-9',
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
