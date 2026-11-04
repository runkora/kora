'use client';

/** @tier 1 — Zero kora domain knowledge. Clipboard writing with a short-lived outcome status. */

import { useCallback, useEffect, useRef, useState } from 'react';

export type CopyStatus = 'idle' | 'copied' | 'failed';

const RESET_MS = 1500;

/**
 * Writes `value` to the clipboard and reports the outcome as `status`, which
 * returns to 'idle' after a short delay so the UI can flash a confirmation.
 */
export function useCopy(value: string) {
  const [status, setStatus] = useState<CopyStatus>('idle');
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(value);
      setStatus('copied');
    } catch {
      setStatus('failed');
    }
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => setStatus('idle'), RESET_MS);
  }, [value]);

  return { status, copy };
}
