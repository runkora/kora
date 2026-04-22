'use client';

import { useEffect, useCallback, useRef } from 'react';

/**
 * useDialog
 *
 * Encapsulates the boilerplate required to drive a native `<dialog>` element
 * from React state. Use this hook when you cannot use the `Modal` component
 * directly (e.g., inside a data-table row, or when integrating with third-party
 * trigger elements that manage their own open state).
 *
 * For the vast majority of cases, prefer `<Modal>` — it handles all of this
 * automatically. `useDialog` exists for the rare scenario where the native
 * `<dialog>` element is unavoidable.
 *
 * @param open    - Whether the dialog should be open.
 * @param onClose - Called when the dialog requests to close (Escape key,
 *                  or backdrop click via `handleBackdropClick`).
 *
 * @returns An object with:
 *   - `ref`                  — attach to the `<dialog>` element's `ref` prop
 *   - `handleBackdropClick`  — attach to the `<dialog>` element's `onClick` prop
 *
 * ```tsx
 * function MyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
 *   const { ref, handleBackdropClick } = useDialog(open, onClose);
 *
 *   return (
 *     <dialog
 *       ref={ref}
 *       onClick={handleBackdropClick}
 *       className="m-auto w-full max-w-lg … open:flex open:flex-col"
 *     >
 *       …content…
 *     </dialog>
 *   );
 * }
 * ```
 */
export function useDialog(open: boolean, onClose: () => void) {
  const ref = useRef<HTMLDialogElement>(null);

  // Sync open state → native dialog.showModal() / dialog.close()
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [open]);

  // Sync native 'close' event (Escape key) → onClose callback
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  // Detect backdrop clicks (click on the dialog element itself, not its content)
  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDialogElement>) => {
      if (e.target === ref.current) onClose();
    },
    [onClose],
  );

  return { ref, handleBackdropClick } as const;
}
