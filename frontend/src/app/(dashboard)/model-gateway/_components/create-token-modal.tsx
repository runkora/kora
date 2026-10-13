'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Button,
  Form,
  FormField,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
} from '@clear-route/hades';
import { createToken } from '../_actions/tokens';
import { ScopePicker } from '@orbit/components';
import type { CreatePATResponse } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface CreateTokenModalProps {
  open: boolean;
  availableScopes: string[];
  onClose: () => void;
  onCreated: (pat: CreatePATResponse) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function CreateTokenModal({
  open,
  availableScopes,
  onClose,
  onCreated,
}: CreateTokenModalProps) {
  const nameInputRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState('');
  const [expiresInDays, setExpiresInDays] = useState<string>('90');
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Focus name input when modal opens; reset on close.
  useEffect(() => {
    if (open) {
      setTimeout(() => nameInputRef.current?.focus(), 0);
    } else {
      setName('');
      setExpiresInDays('90');
      setSelectedScopes([]);
      setError(null);
    }
  }, [open]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setError(null);

      const trimmedName = name.trim();
      if (!trimmedName) {
        setError('Name is required.');
        return;
      }
      if (trimmedName.length > 128) {
        setError('Name must be 128 characters or fewer.');
        return;
      }

      const days = expiresInDays === '' ? undefined : parseInt(expiresInDays, 10);
      if (days !== undefined && (isNaN(days) || days < 1 || days > 365)) {
        setError('Expiry must be between 1 and 365 days.');
        return;
      }

      if (selectedScopes.length === 0) {
        setError('Select at least one scope.');
        return;
      }

      setSubmitting(true);
      try {
        const result = await createToken({
          name: trimmedName,
          expires_in_days: days,
          scopes: selectedScopes,
        });
        if (!result.ok) {
          setError(result.message);
          return;
        }
        onCreated(result.pat);
      } catch {
        setError('Something went wrong. Please try again.');
      } finally {
        setSubmitting(false);
      }
    },
    [name, expiresInDays, selectedScopes, onCreated],
  );

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader
        title="Create API key"
        description="Keys are scoped to your account and usable at inference endpoints."
      />
      <Form onSubmit={(e) => void handleSubmit(e)} noValidate>
        <ModalBody>
          {/* Name */}
          <FormField
            id="pat-name"
            label="Name"
            required
            hint="A short label to identify this key. Max 128 characters."
          >
            <Input
              ref={nameInputRef}
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Production Agent Fleet"
              maxLength={128}
            />
          </FormField>

          {/* Expiry */}
          <FormField
            id="pat-expiry"
            label="Expires in (days)"
            hint="Defaults to 90 days. Maximum 365 days."
          >
            <Input
              type="number"
              value={expiresInDays}
              onChange={(e) => setExpiresInDays(e.target.value)}
              min={1}
              max={365}
              placeholder="90"
            />
          </FormField>

          {/* Scopes */}
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="supporting" weight="emphasis">
                Permissions{' '}
                <Text as="span" aria-hidden={true}>
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
              Choose which permissions this key should carry. Keys cannot exceed your own access.
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
            {submitting ? 'Creating…' : 'Create key'}
          </Button>
        </ModalFooter>
      </Form>
    </Modal>
  );
}
