'use client';

/**
 * @tier 2 — App-level shared. Input-field-styled copy trigger.
 * Displays a value as read-only text with a trailing copy icon;
 * the entire field is clickable with hover feedback.
 * Used by: token-created-modal, user-identity-card.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Icon, Stack, Text } from '@orbit/terra';

export interface CopyFieldProps {
  /** The string value to copy to the clipboard and display. */
  value: string;
  /** Accessible label for the button. Defaults to "Copy to clipboard". */
  label?: string;
  /** Render value in a monospace font. */
  mono?: boolean;
  /** Show a border around the field. Defaults to true. */
  bordered?: boolean;
  /** Expand to fill parent width. Defaults to true. */
  fullWidth?: boolean;
}

export function CopyField({
  value,
  label = 'Copy to clipboard',
  mono,
  bordered = true,
  fullWidth = true,
}: CopyFieldProps) {
  const [status, setStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(value);
      setStatus('copied');
    } catch (err) {
      console.error('Clipboard write failed:', err);
      setStatus('failed');
    }
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => setStatus('idle'), 1500);
  }, [value]);

  return (
    <Button
      variant="ghost"
      size="sm"
      fullWidth={fullWidth}
      onClick={handleCopy}
      aria-label={label}
      style={{
        ...(bordered && {
          border: '1px solid var(--color-border)',
          borderRadius: 'var(--radius-md)',
          paddingInline: '10px',
          height: 36,
        }),
        ...(!fullWidth && { alignSelf: 'flex-start' }),
      }}
    >
      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        grow
        minWidth="none"
        gap="snug"
      >
        <Stack grow minWidth="none">
          <Text size="supporting" mono={mono} truncate>
            {value}
          </Text>
        </Stack>
        {status === 'copied' ? (
          <Icon name="check" size="2xs" color="status-healthy" shrink />
        ) : status === 'failed' ? (
          <Icon name="circle-x" size="2xs" color="status-error" shrink />
        ) : (
          <Icon name="copy" size="2xs" color="subtle" shrink />
        )}
      </Stack>
    </Button>
  );
}
