'use client';

import * as React from 'react';
import { Button } from '../button/button';
import { Card, CardContent } from '../card/card';
import { Stack } from '../layout/stack';
import { StatusDot } from '../status-dot/status-dot';
import { Text } from '../typography/typography';

export interface SaveBarProps {
  /** Whether there are unsaved changes. The bar renders when `visible` or `success` is true. */
  visible: boolean;
  /** Whether a save is in flight — disables both actions and swaps the save label. */
  saving?: boolean;
  /** Whether the last save succeeded — shows the success state and hides the actions. */
  success?: boolean;
  /** Error message from the last save attempt, shown inline in status-error colour. */
  error?: string | null;
  /** Called when the save action is clicked. */
  onSave: () => void;
  /** Called when the discard action is clicked. */
  onDiscard: () => void;
  /** Status label while dirty. @default 'Unsaved changes' */
  dirtyLabel?: string;
  /** Status label after a successful save. @default 'Changes saved' */
  successLabel?: string;
  /** Save button label. @default 'Save changes' */
  saveLabel?: string;
  /** Discard button label. @default 'Discard' */
  discardLabel?: string;
}

/**
 * SaveBar
 *
 * A floating save bar pinned to the bottom-centre of the viewport. Appears
 * when a form goes dirty (`visible`), pulses an amber dot until saved, and
 * settles to a green "saved" state (`success`). Returns `null` when there is
 * nothing to show.
 *
 * ```tsx
 * <SaveBar
 *   visible={isDirty}
 *   saving={isSaving}
 *   success={saveSucceeded}
 *   error={saveError}
 *   onSave={save}
 *   onDiscard={reset}
 * />
 * ```
 */
const SaveBar = React.forwardRef<HTMLDivElement, SaveBarProps>(
  (
    {
      visible,
      saving = false,
      success = false,
      error = null,
      onSave,
      onDiscard,
      dirtyLabel = 'Unsaved changes',
      successLabel = 'Changes saved',
      saveLabel = 'Save changes',
      discardLabel = 'Discard',
    },
    ref,
  ) => {
    if (!visible && !success) return null;

    return (
      <div ref={ref} data-slot="save-bar" className="fixed bottom-5 left-1/2 -translate-x-1/2 z-40">
        <Card border="default" subtle>
          <CardContent padded compact>
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Stack direction="horizontal" align="center" gap="snug">
                <StatusDot size="sm" status={success ? 'healthy' : 'degraded'} pulse={!success} />
                <Text as="span" size="caption" color={success ? 'foreground' : 'muted'}>
                  {success ? successLabel : dirtyLabel}
                </Text>
              </Stack>
              {error && (
                <Text as="span" size="caption" color="status-error">
                  {error}
                </Text>
              )}
              {!success && (
                <Stack direction="horizontal" align="center" gap="snug">
                  <Button variant="secondary" size="sm" onClick={onDiscard} disabled={saving}>
                    {discardLabel}
                  </Button>
                  <Button size="sm" onClick={onSave} disabled={saving}>
                    {saving ? 'Saving…' : saveLabel}
                  </Button>
                </Stack>
              )}
            </Stack>
          </CardContent>
        </Card>
      </div>
    );
  },
);

SaveBar.displayName = 'SaveBar';

export { SaveBar };
