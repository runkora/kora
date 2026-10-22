'use client';

/** @tier 3 — policy-scoped. Searchable single-select list of the tenant's users, for the grant wizard. */

import { useEffect, useState } from 'react';
import {
  Card,
  CardContent,
  Icon,
  Input,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
} from '@runkora/hades';
import type { PATAttributionEntry } from '../../model-gateway/_services/tokens-server';
import { searchUsersAction } from '../_actions/grants';

interface UserPickerProps {
  /** The selected user's ID, or empty. */
  value: string;
  onChange: (id: string, user: PATAttributionEntry) => void;
}

export function UserPicker({ value, onChange }: UserPickerProps) {
  const [users, setUsers] = useState<PATAttributionEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const delay = search ? 300 : 0;
    const timer = setTimeout(async () => {
      if (cancelled) return;
      setLoading(true);
      try {
        const result = await searchUsersAction(search);
        if (cancelled) return;
        if (result.ok) {
          setUsers(result.data);
          setFailed(false);
        } else {
          setFailed(true);
        }
      } catch {
        if (!cancelled) setFailed(true);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, delay);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [search]);

  const status = loading
    ? 'Loading…'
    : failed
      ? null
      : users.length === 0
        ? search
          ? 'No users match your search'
          : 'No users found'
        : null;

  return (
    <Stack gap="snug">
      <Input
        type="text"
        placeholder="Search users…"
        aria-label="Search users"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      {status || failed ? (
        <Card subtle>
          <CardContent padded compact>
            {!loading && failed ? (
              <Text size="caption" color="status-error">
                Could not load users. Try searching again.
              </Text>
            ) : (
              <Text size="caption" color="muted">
                {status}
              </Text>
            )}
          </CardContent>
        </Card>
      ) : (
        <RadioGroup
          value={value}
          onValueChange={(id) => {
            const picked = users.find((u) => u.id === id);
            if (picked) onChange(id, picked);
          }}
          aria-label="User"
        >
          <Stack maxHeight="scroll-sm" overflowY="auto" gap="snug" paddingBlock="tight">
            {users.map((u) => {
              const name = u.display_name || u.email;
              const selected = value === u.id;
              return (
                <Card key={u.id} interactive selected={selected} onClick={() => onChange(u.id, u)}>
                  <CardContent padded compact>
                    <Stack direction="horizontal" align="center" gap="cozy">
                      <RadioGroupItem value={u.id} aria-label={name} />
                      <Icon
                        name="user"
                        size="sm"
                        color={selected ? 'accent' : 'muted'}
                        container
                        containerVariant={selected ? 'accent' : 'surface-subtle'}
                        label=""
                      />
                      <Stack gap="none" grow minWidth="none">
                        <Text as="span" size="supporting" weight="heavy" truncate>
                          {name}
                        </Text>
                        {u.display_name && u.display_name !== u.email && (
                          <Text as="span" size="caption" color="muted" truncate>
                            {u.email}
                          </Text>
                        )}
                      </Stack>
                    </Stack>
                  </CardContent>
                </Card>
              );
            })}
          </Stack>
        </RadioGroup>
      )}
    </Stack>
  );
}
