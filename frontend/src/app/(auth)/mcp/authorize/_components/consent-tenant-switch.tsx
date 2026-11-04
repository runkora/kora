/** @tier 3 — mcp-authorize-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useState } from 'react';
import { Button, Icon, Popover, PopoverContent, PopoverTrigger, Text } from '@runkora/hades';
import { TenantList, useTenantSwitch } from '@runkora/components';
import type { TenantOption } from '@runkora/server/auth-config';

export interface ConsentTenantSwitchProps {
  tenants: TenantOption[];
  /** Starts the switch; a rejection shows the list's error line. */
  onSelect: (tenantId: string) => Promise<void> | void;
}

/** The consent row's Switch control. It opens the tenant list beside the row. */
export function ConsentTenantSwitch({ tenants, onSelect }: ConsentTenantSwitchProps) {
  const [open, setOpen] = useState(false);
  const { switching, failed, start, clearError } = useTenantSwitch(onSelect);
  const activeName = tenants.find((t) => t.active)?.displayName ?? 'this tenant';

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) clearError();
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="outline-muted" size="sm" aria-label={`Switch tenant from ${activeName}`}>
          <Text as="span" size="caption">
            Switch
          </Text>
          <Icon name="chevron-down" size="2xs" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <TenantList
          tenants={tenants}
          switching={switching}
          failed={failed}
          onPick={(t) => (t.active ? setOpen(false) : start(t.id))}
        />
      </PopoverContent>
    </Popover>
  );
}
