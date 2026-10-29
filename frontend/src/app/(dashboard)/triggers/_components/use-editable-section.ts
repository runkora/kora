'use client';

/**
 * @tier 3 — triggers-scoped. Per-section edit state for the trigger modal.
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
  /** Why this cannot be saved yet, if anything. */
  blocker: string | undefined;
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
  /**
   * A value saved successfully, held until the refreshed row arrives.
   *
   * `before` is what was stored at the moment of the save. While the row still
   * reads that way the refresh has not landed yet, so the section keeps showing
   * what was saved rather than flashing back to the old value and forward
   * again. The moment `stored` changes at all the hold ends, so the panel shows
   * what the platform actually stored, not what was sent.
   */
  awaiting: { saved: T; before: T } | null;
}

export interface EditableSectionOptions<T> {
  /** Content equality for the draft. Defaults to `Object.is`. */
  isEqual?: (a: T, b: T) => boolean;
  /** Why the current value cannot be saved, if anything. */
  validate?: (value: T) => string | undefined;
}

export function useEditableSection<T>(
  stored: T,
  /** Resets the draft when it changes, e.g. the trigger's id. */
  resetKey: string,
  onSave: (value: T) => Promise<string | null>,
  { isEqual = Object.is, validate }: EditableSectionOptions<T> = {},
): EditableSection<T> {
  const [state, setState] = useState<DraftState<T>>({
    key: resetKey,
    draft: null,
    error: null,
    awaiting: null,
  });
  const [pending, setPending] = useState(false);

  // A draft belonging to another trigger is not this section's draft.
  const current =
    state.key === resetKey ? state : { key: resetKey, draft: null, error: null, awaiting: null };
  const held =
    current.awaiting !== null && isEqual(stored, current.awaiting.before)
      ? current.awaiting.saved
      : null;
  const value = current.draft !== null ? current.draft : held !== null ? held : stored;
  const dirty = current.draft !== null && !isEqual(current.draft, stored);

  const setValue = useCallback(
    (next: T) => setState({ key: resetKey, draft: next, error: null, awaiting: null }),
    [resetKey],
  );

  const discard = useCallback(
    () => setState({ key: resetKey, draft: null, error: null, awaiting: null }),
    [resetKey],
  );

  const save = useCallback(() => {
    const draft = current.draft;
    if (draft === null) return;
    const before = stored;
    setPending(true);
    void onSave(draft).then((error) => {
      setPending(false);
      setState((prev) => {
        // Another trigger is open now; its state is not this save's to touch.
        if (prev.key !== resetKey) return prev;
        // On failure the draft is kept, along with the reason.
        if (error !== null) return { ...prev, error, awaiting: null };
        // An edit made while the save was in flight is the person's latest
        // intent: it survives the response rather than being wiped by it.
        if (prev.draft !== null && !isEqual(prev.draft, draft)) {
          return { ...prev, error: null, awaiting: null };
        }
        return { key: resetKey, draft: null, error: null, awaiting: { saved: draft, before } };
      });
    });
  }, [current.draft, stored, onSave, resetKey, isEqual]);

  return {
    value,
    setValue,
    dirty,
    pending,
    error: current.error,
    blocker: validate?.(value),
    save,
    discard,
  };
}
