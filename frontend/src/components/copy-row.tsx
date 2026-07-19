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
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), 1500);
    });
  }, [value]);

  return (
    <Button variant="ghost" size="sm" fullWidth onClick={handleCopy} aria-label={`Copy ${label}`}>
      <Stack direction="horizontal" align="center" justify="between" grow textAlign="start">
        <Stack gap="none">
          <Text size="caption" color="muted">
            {label}
          </Text>
          <Text size="supporting" weight="emphasis" mono={mono}>
            {value}
          </Text>
        </Stack>
        {copied ? (
          <Icon name="check" size="2xs" color="status-healthy" shrink />
        ) : (
          <Icon name="copy" size="2xs" color="muted" />
        )}
      </Stack>
    </Button>
  );
}
