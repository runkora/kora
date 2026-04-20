'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { Button, Heading, Stack, Text } from '@orbit/terra';
import { updateTokenScopes } from '../_actions/tokens';
import { ScopePicker } from '../../policy/_components/scope-picker';
import type { PAT } from '@orbit/shared/api';

interface EditScopesModalProps {
  pat: PAT | null;
  availableScopes: string[];
  onClose: () => void;
  onUpdated: () => void;
}

export function EditScopesModal({
  pat,
  availableScopes,
  onClose,
  onUpdated,
}: EditScopesModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [scopeSearch, setScopeSearch] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const open = pat !== null;

  // Sync dialog open/close.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open) {
      setSelectedScopes(pat.scopes ?? []);
      setScopeSearch('');
      setError(null);
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [open, pat]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => {
      setSelectedScopes([]);
      setScopeSearch('');
      setError(null);
      onClose();
    };
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDialogElement>) => {
      if (e.target === dialogRef.current) onClose();
    },
    [onClose],
  );

  const handleToggleScope = useCallback((scope: string, checked: boolean) => {
    setSelectedScopes((prev) => (checked ? [...prev, scope] : prev.filter((s) => s !== scope)));
  }, []);

  const handleToggleScopeGroup = useCallback((scopes: string[], checked: boolean) => {
    setSelectedScopes((prev) => {
      const without = prev.filter((s) => !scopes.includes(s));
      return checked ? [...without, ...scopes] : without;
    });
  }, []);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!pat) return;
      setError(null);

      if (selectedScopes.length === 0) {
        setError('Select at least one scope.');
        return;
      }

      setSubmitting(true);
      try {
        const result = await updateTokenScopes(pat.id, selectedScopes);
        if (!result.ok) {
          setError(result.message);
          return;
        }
        onUpdated();
        onClose();
      } catch {
        setError('Something went wrong. Please try again.');
      } finally {
        setSubmitting(false);
      }
    },
    [pat, selectedScopes, onUpdated, onClose],
  );

  return (
    <dialog
      ref={dialogRef}
      onClick={handleBackdropClick}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      <form onSubmit={(e) => void handleSubmit(e)} noValidate>
        {/* Header */}
        <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
          <header className="border-b border-border p-5">
            <Stack gap="none">
              <Heading variant="section">Edit permissions</Heading>
              {pat && (
                <Text size="supporting" color="muted">
                  Update scopes for {pat.name}
                </Text>
              )}
            </Stack>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={onClose}
              aria-label="Close"
            >
              <X className="size-4" />
            </Button>
          </header>
        </Stack>

        <Stack asChild gap="comfortable">
          <section className="p-5">
            <Stack gap="tight">
              <Stack direction="horizontal" align="center" justify="between">
                <label className="text-sm font-medium text-foreground">
                  Permissions <output aria-hidden="true">*</output>
                </label>
                {selectedScopes.length > 0 && (
                  <Text size="caption" color="muted">
                    {selectedScopes.length} selected
                  </Text>
                )}
              </Stack>
              {availableScopes.length === 0 ? (
                <Text size="supporting" color="muted">
                  No scopes available — your account has no granted permissions.
                </Text>
              ) : (
                <ScopePicker
                  availableScopes={availableScopes}
                  selectedScopes={selectedScopes}
                  onToggle={handleToggleScope}
                  onToggleGroup={handleToggleScopeGroup}
                  search={scopeSearch}
                  onSearchChange={setScopeSearch}
                />
              )}
              <Text size="caption" color="subtle">
                Keys cannot carry more permissions than your current access.
              </Text>
            </Stack>

            {error && (
              <Text size="supporting" color="status-error">
                {error}
              </Text>
            )}
          </section>
        </Stack>

        <Stack asChild direction="horizontal" justify="end" gap="snug">
          <footer className="border-t border-border px-5 py-3">
            <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" variant="default" size="sm" disabled={submitting}>
              {submitting ? 'Saving…' : 'Save permissions'}
            </Button>
          </footer>
        </Stack>
      </form>
    </dialog>
  );
}
