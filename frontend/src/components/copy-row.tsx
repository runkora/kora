'use client';

/**
 * @tier 2 — Orbit-domain-aware: used by model-gateway modals to copy record field values.
 * Used by: inference-log-detail-modal, model-detail-modal.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Icon, Stack, Text } from '@orbit/terra';

export interface CopyRowProps {
  label: string;
  value: string;
  mono?: boolean;
}

export function CopyRow({ label, value, mono }: CopyRowProps) {
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
      fullWidth
      onClick={handleCopy}
      aria-label={`Copy ${label}`}
      style={{ paddingBlockStart: '10px', paddingBlockEnd: '8px' }}
    >
      <Stack direction="horizontal" align="center" justify="between" grow textAlign="start">
        <Stack gap="none">
          <Text size="caption" color="muted">
            {label}
          </Text>
          <Text size="supporting" weight="emphasis" mono={mono}>
            {value}
          </Text>
        </Stack>
        {status === 'copied' ? (
          <Icon name="check" size="2xs" color="status-healthy" shrink />
        ) : status === 'failed' ? (
          <Icon name="circle-x" size="2xs" color="status-error" shrink />
        ) : (
          <Icon name="copy" size="2xs" color="muted" />
        )}
      </Stack>
    </Button>
  );
}
