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
} from '@runkora/hades';
import { updateTokenScopes } from '../_actions/tokens';
import { ScopePicker } from '@runkora/components';
import type { ScopeOption } from '@runkora/components';
import type { PAT } from '@runkora/shared/api';

interface EditScopesModalProps {
  pat: PAT | null;
  availableScopes: string[];
  scopeOptions?: ScopeOption[];
  onClose: () => void;
  onUpdated: () => void;
}

const FORM_ID = 'edit-scopes-form';

export function EditScopesModal({
  pat,
  availableScopes,
  scopeOptions,
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
      {/* The form lives inside ModalBody, not around it, so ModalBody and ModalFooter
          stay direct children of the Modal panel: that is what lets the body shrink
          and scroll while the footer stays in view. The submit button reaches the
          form through its `form` attribute. */}
      <ModalBody constrained>
        <Form id={FORM_ID} onSubmit={(e) => void handleSubmit(e)} noValidate gap="comfortable">
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="caption" weight="emphasis">
                Permissions{' '}
                <Text as="span" aria-hidden="true">
                  *
                </Text>
              </Text>
            </Stack>
            {availableScopes.length === 0 ? (
              <Text size="supporting" color="muted">
                No scopes available. Your account has no granted permissions.
              </Text>
            ) : (
              <ScopePicker
                options={scopeOptions}
                scopes={availableScopes}
                selected={selectedScopes}
                onChange={setSelectedScopes}
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
        </Form>
      </ModalBody>

      <ModalFooter>
        <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button type="submit" form={FORM_ID} variant="default" size="sm" disabled={submitting}>
          {submitting ? 'Saving…' : 'Save permissions'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
