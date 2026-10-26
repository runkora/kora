/** @tier 2 — kora-domain-aware: marks items declared in a tenant's configuration repository. Used by: agents, environments, triggers, policy roles, model gateway catalog. */
import type { ReactNode } from 'react';
import { Alert, Anchor, Badge, Callout, Stack, Text } from '@runkora/hades';
import {
  declarativeRefusal,
  PLATFORM_LOCKED_MESSAGE,
  repositoryUrl,
} from '@runkora/lib/declarative';

export interface ManagedInGitBadgeProps {
  /** The repository that drives the item, from its runkora.io/repository annotation. */
  repository?: string;
}

/**
 * Marks an item declared in the configuration repository. Show it wherever the item is listed or
 * opened. It names the repository as text, since list rows are often links themselves. A long repository
 * truncates inside the space it is given, and the full text is the native tooltip.
 */
export function ManagedInGitBadge({ repository }: ManagedInGitBadgeProps) {
  const label = repository ? `Managed by ${repository}` : 'Managed in git';
  return (
    <Stack direction="horizontal" style={{ maxWidth: '100%', minWidth: 0 }}>
      <Badge variant="secondary" size="sm" title={label}>
        <Text as="span" size="caption" truncate>
          {label}
        </Text>
      </Badge>
    </Stack>
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
  /** The repository that drives a declared item. It is linked when it reads as a URL or host and path. */
  repository?: string;
  /** The lock is the platform's virtual model switch rather than the tenant's. */
  platform?: boolean;
  /** A sentence added after the explanation. */
  children?: ReactNode;
}

/** Heads a read-only detail view: why the item cannot change here. */
export function ManagedInGitNotice({
  declared,
  repository,
  platform = false,
  children,
}: ManagedInGitNoticeProps) {
  if (declared && repository) {
    const url = repositoryUrl(repository);
    return (
      <Alert variant="info">
        <Text size="caption">
          Managed by{' '}
          {url ? (
            <Anchor href={url} target="_blank" rel="noopener noreferrer">
              {repository}
            </Anchor>
          ) : (
            repository
          )}
          . Make changes in that repository.
          {children}
        </Text>
      </Alert>
    );
  }
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
