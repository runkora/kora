'use client';

/**
 * @tier 2 — App-level shared. Consolidates clipboard copy interactions across
 * model-gateway (token-created-modal, model-detail-modal, setup-tab),
 * policy (vocabulary-tab), and chat (message-content).
 * Uses the useRef + useEffect timer cleanup pattern from vocabulary-tab.tsx.
 * @promote-to-tier-1 if: a second non-dashboard Orbit surface needs copy-to-clipboard.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Icon } from '@orbit/terra';

export interface CopyButtonProps {
  /** The string value to copy to the clipboard. */
  value: string;
  /** Accessible label for the button. Defaults to "Copy to clipboard". */
  label?: string;
}

export function CopyButton({ value, label = 'Copy to clipboard' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      return;
    }
    setCopied(true);
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => setCopied(false), 1500);
  }, [value]);

  return (
    <Button variant="ghost" size="icon-sm" onClick={handleCopy} aria-label={label}>
      {copied ? (
        <Icon name="check" size="2xs" color="status-healthy" />
      ) : (
        <Icon name="copy" size="2xs" color="subtle" />
      )}
    </Button>
  );
}
