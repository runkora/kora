'use client';

/**
 * @tier 3 — triggers-scoped. Per-section edit state for the trigger drawer.
 *
 * Each editable section owns a draft that only exists while it differs from
 * what is stored. That keeps two things true at once: a section the person is
 * editing keeps their text, and every other section follows the live row as the
 * page refreshes underneath the open panel.
 *
 * The reset is derived rather than run in an effect, so switching to another
 * trigger drops stale drafts during the same render instead of one frame later.
 */

import { useCallback, useState } from 'react';

export interface EditableSection<T> {
  /** The draft while editing, otherwise the stored value. */
  value: T;
  setValue: (next: T) => void;
  /** Whether the draft differs from what is stored. */
  dirty: boolean;
  pending: boolean;
  /** The last failed save. Cleared on the next edit or save. */
  error: string | null;
  /** Runs the save, keeping the draft if it fails. */
  save: () => void;
  /** Drops the draft and returns to the stored value. */
  discard: () => void;
}

interface DraftState<T> {
  /** Identifies what the draft belongs to; a change discards it. */
  key: string;
  draft: T | null;
  error: string | null;
}

export function useEditableSection<T>(
  stored: T,
  /** Resets the draft when it changes, e.g. the trigger's id. */
  resetKey: string,
  onSave: (value: T) => Promise<string | null>,
  isEqual: (a: T, b: T) => boolean = Object.is,
): EditableSection<T> {
  const [state, setState] = useState<DraftState<T>>({ key: resetKey, draft: null, error: null });
  const [pending, setPending] = useState(false);

  // A draft belonging to another trigger is not this section's draft.
  const current = state.key === resetKey ? state : { key: resetKey, draft: null, error: null };
  const value = current.draft === null ? stored : current.draft;
  const dirty = current.draft !== null && !isEqual(current.draft, stored);

  const setValue = useCallback(
    (next: T) => setState({ key: resetKey, draft: next, error: null }),
    [resetKey],
  );

  const discard = useCallback(
    () => setState({ key: resetKey, draft: null, error: null }),
    [resetKey],
  );

  const save = useCallback(() => {
    if (current.draft === null) return;
    const draft = current.draft;
    setPending(true);
    void onSave(draft).then((error) => {
      setPending(false);
      // On success the draft is dropped, so the section falls back to the
      // refreshed row; on failure it is kept, along with the reason.
      setState({ key: resetKey, draft: error === null ? null : draft, error });
    });
  }, [current.draft, onSave, resetKey]);

  return { value, setValue, dirty, pending, error: current.error, save, discard };
}
