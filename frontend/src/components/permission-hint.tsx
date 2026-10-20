/** @tier 2 — Orbit-domain-aware: explains a missing permission. Used by: agents, agent detail, environments, triggers, model gateway catalog. */
'use client';

import type { ReactNode } from 'react';
import { Stack, Tooltip, TooltipContent, TooltipTrigger } from '@runkora/hades';

export interface PermissionHintProps {
  /** Whether the caller may perform the action. When true the control renders as it is. */
  allowed: boolean;
  /** What the control does, completing "You don't have permission to …", e.g. "start runs of this agent". */
  action: string;
  /** The control, already disabled by the caller when `allowed` is false. */
  children: ReactNode;
}

/** The sentence a disabled control shows when the caller lacks the permission behind it. */
export function permissionHintText(action: string): string {
  return `You don't have permission to ${action}.`;
}

/**
 * Explains a control the caller cannot use. A disabled button takes no pointer
 * events, so the tooltip hangs on a focusable wrapper around it.
 */
export function PermissionHint({ allowed, action, children }: PermissionHintProps) {
  if (allowed) return <>{children}</>;
  const hint = permissionHintText(action);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Stack direction="horizontal" tabIndex={0} aria-label={hint} data-permission-hint="">
          {children}
        </Stack>
      </TooltipTrigger>
      <TooltipContent>{hint}</TooltipContent>
    </Tooltip>
  );
}
