'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Form,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
} from '@orbit/hades';
import { updateTokenScopes } from '../_actions/tokens';
import { ScopePicker } from '@orbit/components';
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
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Initialise / reset state whenever the target PAT changes.
  useEffect(() => {
    if (!pat) {
      setSelectedScopes([]);
      setError(null);
      return;
    }
    setSelectedScopes(pat.scopes ?? []);
    setError(null);
  }, [pat]);

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
      <Form onSubmit={(e) => void handleSubmit(e)} noValidate>
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
                No scopes available. Your account has no granted permissions.
              </Text>
            ) : (
              <ScopePicker
                scopes={availableScopes}
                selected={selectedScopes}
                onChange={setSelectedScopes}
                searchable
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
      </Form>
    </Modal>
  );
}
