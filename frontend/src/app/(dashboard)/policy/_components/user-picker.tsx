'use client';

/** @tier 3 — policy-scoped. Searchable single-select list of the tenant's users, for the grant wizard. */

import { useEffect, useState } from 'react';
import { Button, Card, Input, Stack, Text } from '@runkora/hades';
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

  useEffect(() => {
    let cancelled = false;
    const delay = search ? 300 : 0;
    const timer = setTimeout(async () => {
      if (cancelled) return;
      setLoading(true);
      const result = await searchUsersAction(search);
      if (cancelled) return;
      setLoading(false);
      if (result.ok) setUsers(result.data);
    }, delay);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [search]);

  return (
    <Stack gap="none">
      <Stack marginBottom="tight">
        <Input
          type="text"
          placeholder="Search users…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </Stack>
      <Card maxHeight="scroll-sm" overflowY="auto" border="default" divide>
        {loading || users.length === 0 ? (
          <Stack paddingBlock="tight" paddingInline="snug">
            <Text size="caption" color="muted">
              {loading ? 'Loading…' : search ? 'No users match your search' : 'No users found'}
            </Text>
          </Stack>
        ) : (
          users.map((u) => {
            const isSelected = value === u.id;
            return (
              <Button
                key={u.id}
                variant="ghost"
                shape="square"
                size="sm"
                selected={isSelected}
                fullWidth
                onClick={() => onChange(u.id, u)}
              >
                <Stack direction="horizontal" grow align="center" justify="between" minWidth="none">
                  <Text
                    size="micro"
                    truncate
                    color={isSelected ? 'accent' : 'foreground'}
                    weight={isSelected ? 'emphasis' : undefined}
                  >
                    {u.display_name || u.email}
                  </Text>
                  {u.display_name && u.display_name !== u.email && (
                    <Text size="micro" color="muted">
                      {u.email}
                    </Text>
                  )}
                </Stack>
              </Button>
            );
          })
        )}
      </Card>
    </Stack>
  );
}
