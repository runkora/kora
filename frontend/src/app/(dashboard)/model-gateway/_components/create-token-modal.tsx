'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Modal, ModalBody, ModalFooter, ModalHeader, Stack, Text } from '@orbit/terra';
import { createToken } from '../_actions/tokens';
import { ScopePicker } from '../../policy/_components/scope-picker';
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
  const [scopeSearch, setScopeSearch] = useState('');
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
      setScopeSearch('');
      setError(null);
    }
  }, [open]);

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
      <form onSubmit={(e) => void handleSubmit(e)} noValidate>
        <ModalBody>
          {/* Name */}
          <Stack gap="tight">
            <label htmlFor="pat-name" className="text-sm font-medium text-foreground">
              Name{' '}
              <Text as="span" aria-hidden={true}>
                *
              </Text>
            </label>
            <input
              ref={nameInputRef}
              id="pat-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Production Agent Fleet"
              maxLength={128}
              required
              className="w-full rounded-md border border-border bg-surface-elevated px-3 py-2 text-sm text-foreground placeholder:text-muted focus:outline-none focus:ring-2 focus:ring-accent"
            />
            <Text size="caption" color="subtle">
              A short label to identify this key. Max 128 characters.
            </Text>
          </Stack>

          {/* Expiry */}
          <Stack gap="tight">
            <label htmlFor="pat-expiry" className="text-sm font-medium text-foreground">
              Expires in (days)
            </label>
            <input
              id="pat-expiry"
              type="number"
              value={expiresInDays}
              onChange={(e) => setExpiresInDays(e.target.value)}
              min={1}
              max={365}
              placeholder="90"
              className="w-full rounded-md border border-border bg-surface-elevated px-3 py-2 text-sm text-foreground placeholder:text-muted focus:outline-none focus:ring-2 focus:ring-accent"
            />
            <Text size="caption" color="subtle">
              Defaults to 90 days. Maximum 365 days.
            </Text>
          </Stack>

          {/* Scopes */}
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" justify="between">
              <label className="text-sm font-medium text-foreground">
                Permissions{' '}
                <Text as="span" aria-hidden={true}>
                  *
                </Text>
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
      </form>
    </Modal>
  );
}
