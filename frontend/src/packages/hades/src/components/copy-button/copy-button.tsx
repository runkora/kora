'use client';

/** @tier 1 — Zero kora domain knowledge. Could be extracted to a standalone library. */

import * as React from 'react';
import { useCopy } from '../../hooks/use-copy';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';

export interface CopyButtonProps {
  /** The string value to copy to the clipboard. */
  value: string;
  /** Accessible label for the button. Defaults to "Copy to clipboard". */
  label?: string;
}

/** An icon-only button that copies `value` and briefly swaps the icon to confirm. */
const CopyButton = React.forwardRef<HTMLButtonElement, CopyButtonProps>(
  ({ value, label = 'Copy to clipboard' }, ref) => {
    const { status, copy } = useCopy(value);

    return (
      <Button
        ref={ref}
        type="button"
        data-slot="copy-button"
        variant="ghost"
        size="icon-sm"
        onClick={copy}
        aria-label={label}
      >
        {status === 'copied' ? (
          <Icon name="check" size="2xs" color="status-healthy" />
        ) : status === 'failed' ? (
          <Icon name="circle-x" size="2xs" color="status-error" />
        ) : (
          <Icon name="copy" size="2xs" color="subtle" />
        )}
      </Button>
    );
  },
);
CopyButton.displayName = 'CopyButton';

export { CopyButton };
