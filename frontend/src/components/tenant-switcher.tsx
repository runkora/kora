/** @tier 2 — kora-domain-aware: lists the person's tenants. Used by: dashboard shell, MCP consent. */
'use client';

import { useState } from 'react';
import { Button, Icon, Popover, PopoverContent, PopoverTrigger, Stack, Text } from '@runkora/hades';

export interface TenantSwitcherOption {
  id: string;
  displayName: string;
  active: boolean;
}

export interface TenantSwitcherProps {
  /** The eligible set. The switcher renders nothing with fewer than two. */
  tenants: TenantSwitcherOption[];
  /** Called with the chosen tenant; the caller starts the switch. A rejection re-enables the list. */
  onSelect: (tenantId: string) => Promise<void> | void;
  /** Trigger text. Defaults to the active tenant's name. */
  label?: string;
}

/** Shows the active tenant and opens a list of the other tenants the person can switch to. */
export function TenantSwitcher({ tenants, onSelect, label }: TenantSwitcherProps) {
  const [open, setOpen] = useState(false);
  const [switching, setSwitching] = useState<string | null>(null);
  if (tenants.length < 2) return null;
  const active = tenants.find((t) => t.active);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline-muted" size="sm" aria-label="Switch tenant">
          <Icon name="building" size="xs" />
          <Text as="span" size="caption" truncate>
            {label ?? active?.displayName ?? 'Choose a tenant'}
          </Text>
          <Icon name="chevron-down" size="2xs" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <Stack gap="none" role="menu" aria-label="Tenants">
          {tenants.map((t) => (
            <Button
              key={t.id}
              variant="ghost"
              size="sm"
              fullWidth
              role="menuitemradio"
              aria-checked={t.active}
              disabled={switching !== null}
              onClick={() => {
                if (t.active) {
                  setOpen(false);
                  return;
                }
                setSwitching(t.id);
                Promise.resolve(onSelect(t.id)).catch(() => setSwitching(null));
              }}
              style={{ justifyContent: 'space-between' }}
            >
              <Text as="span" size="caption" truncate>
                {t.displayName}
              </Text>
              {t.active ? <Icon name="check" size="xs" color="accent" /> : null}
              {switching === t.id ? <Icon name="loader" size="xs" color="subtle" /> : null}
            </Button>
          ))}
        </Stack>
      </PopoverContent>
    </Popover>
  );
}
