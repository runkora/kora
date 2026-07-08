'use client';

/** @tier 3 — mcp-gateway-scoped. A server's identity model, with an explainer. */

import { Badge, Icon, Stack, Tooltip, TooltipContent, TooltipTrigger } from '@orbit/terra';

interface IdentityBadgeProps {
  callerIdentity: string;
  /** Short label ("Delegated" / "Service") for dense card/table rows. */
  compact?: boolean;
}

/**
 * "Delegated identity" — the gateway forwards the caller's identity, so the
 * server acts as the caller (callerIdentity: forward). Otherwise "Service
 * identity" — the server acts as itself with its own credentials.
 */
export function IdentityBadge({ callerIdentity, compact = false }: IdentityBadgeProps) {
  const delegated = callerIdentity === 'forward';
  const noun = compact ? '' : ' identity';

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={delegated ? 'secondary' : 'outline'} size="sm">
          <Stack direction="horizontal" gap="tight" align="center">
            <Icon name={delegated ? 'user-check' : 'shield'} size="2xs" />
            {(delegated ? 'Delegated' : 'Service') + noun}
          </Stack>
        </Badge>
      </TooltipTrigger>
      <TooltipContent>
        {delegated
          ? 'Runs each call as you, so it stays within your permissions.'
          : 'Acts as itself with its own credentials. Your token is never forwarded.'}
      </TooltipContent>
    </Tooltip>
  );
}
