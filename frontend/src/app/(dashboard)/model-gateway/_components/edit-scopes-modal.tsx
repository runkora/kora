'use client';

import { useCallback, useEffect, useState } from 'react';
import { Button, Modal, ModalBody, ModalFooter, ModalHeader, Stack, Text } from '@orbit/terra';
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
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [scopeSearch, setScopeSearch] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Initialise / reset state whenever the target PAT changes.
  useEffect(() => {
    if (!pat) {
      setSelectedScopes([]);
      setScopeSearch('');
      setError(null);
      return;
    }
    setSelectedScopes(pat.scopes ?? []);
    setScopeSearch('');
    setError(null);
  }, [pat]);

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
    <Modal open={!!pat} onClose={onClose} size="lg">
      <ModalHeader
        title="Edit permissions"
        description={pat ? `Update scopes for ${pat.name}` : undefined}
      />
      <form onSubmit={(e) => void handleSubmit(e)} noValidate>
        <ModalBody>
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="caption" weight="emphasis">
                Permissions{' '}
                <Text as="span" aria-hidden="true">
                  *
                </Text>
              </Text>
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
        </ModalBody>

        <ModalFooter>
          <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" variant="default" size="sm" disabled={submitting}>
            {submitting ? 'Saving…' : 'Save permissions'}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  );
}
