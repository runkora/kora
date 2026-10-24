/** @tier 2 — kora-domain-aware: marks items declared in a tenant's configuration repository. Used by: agents, environments, triggers, policy roles, model gateway catalog. */
import type { ReactNode } from 'react';
import { Alert, Badge, Callout, Text } from '@runkora/hades';
import { declarativeRefusal, PLATFORM_LOCKED_MESSAGE } from '@runkora/lib/declarative';

/** Marks an item declared in the configuration repository. Show it wherever the item is listed or opened. */
export function ManagedInGitBadge() {
  return (
    <Badge variant="secondary" size="sm">
      Managed in git
    </Badge>
  );
}

export interface ManagedInGitCalloutProps {
  /** What is read-only, as the plural subject that starts the sentence, e.g. "Agents in this tenant". */
  subject: string;
}

/** Explains why a page is read-only when its items are authored only in the configuration repository. */
export function ManagedInGitCallout({ subject }: ManagedInGitCalloutProps) {
  return (
    <Callout>
      <Text size="supporting">
        {subject} are managed in git. Make changes in the configuration repository.
      </Text>
    </Callout>
  );
}

export interface ManagedInGitNoticeProps {
  /** The item is declared in the configuration repository, rather than locked by a switch. */
  declared: boolean;
  /** The lock is the platform's virtual model switch rather than the tenant's. */
  platform?: boolean;
  /** A sentence added after the explanation. */
  children?: ReactNode;
}

/** Heads a read-only detail view: why the item cannot change here. */
export function ManagedInGitNotice({
  declared,
  platform = false,
  children,
}: ManagedInGitNoticeProps) {
  const message = declared
    ? declarativeRefusal('managed_by_resource')
    : platform
      ? PLATFORM_LOCKED_MESSAGE
      : declarativeRefusal('api_authoring_disabled');
  return (
    <Alert variant="info">
      <Text size="caption">
        {message}
        {children}
      </Text>
    </Alert>
  );
}
